package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"
)

//  for p2pnode.Node and also related
type LCAManager struct {
    // Libp2p node instance for this node
    Host       p2pnode.Node
    // Identifier hash for the service this node is responsible for
    P2PHash    string
}

// Finds the best service instance by pinging other LCA Manager instances
func (lca *LCAManager) FindService(serviceHash string) (peer.ID, string, error) {
    fmt.Println("Finding providers for:", serviceHash)
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, serviceHash)
    if err != nil {
        panic(err)
    }

    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Print out RTT for testing
    // for _, p := range peers {
    //     fmt.Println(p.ID, ":", p.Perf)
    // }

    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAManagerProtocolID)
        if err != nil {
            continue
        } else {
            rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
            str, err := rw.ReadString('\n')
            if err != nil {
                fmt.Println("Error reading from buffer")
                return peer.ID(""), "", err
            }
            str = strings.TrimSuffix(str, "\n")

            stream.Close()

            fmt.Println("Got response from peer:", str)
            return p.ID, str, nil
        }
    }

    return peer.ID(""), "", ErrUhOh
}

// Helper function to AllocService that handles the communication with LCA Allocator
func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    // Send command "start-program"
    _, err := rw.WriteString(fmt.Sprintf("start-program %s\n", serviceHash))
    if err != nil {
        fmt.Println("Error writing to buffer")
        return "", err
    }
    err = rw.Flush()
    if err != nil {
        fmt.Println("Error flushing buffer")
        return "", err
    }

    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        return "", err
    }
    str = strings.TrimSuffix(str, "\n")

    stream.Close()

    // Parse IP address and Port
    match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
    if err != nil {
        return "", err
    }

    if match {
        return str, nil
    }

    return "", ErrUhOh
}

// Requests allocation on LCA Allocators with good network performance
func (lca *LCAManager) AllocService(serviceHash string) (peer.ID, string, error) {
    // Look for Allocators
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, LCAAllocatorRendezvous)
    if err != nil {
        return peer.ID(""), "", err
    }

    // Sort Allocators based on performance
    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Request allocation until one succeeds then return allocated service address
    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAAllocatorProtocolID)
        if err != nil {
            continue
        } else {
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return p.ID, result, nil
        }
    }

    return peer.ID(""), "", ErrUhOh
}

// Stub
// Used to check the health of the service the LCA Manager is responsible for
func pingService() error {
    return nil
}

// LCAManagerHandler generator function
// Used to allow the Handler to remember the service address
// Generated LCAManagerHandler pings the service it is responsible for to check
// that it is still up then sends the service address back to the requester.
func NewLCAManagerHandler(address string) func(network.Stream) {
    return func(stream network.Stream) {
        fmt.Println("Got a new LCA Manager request")
        rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
        err := pingService()
        if err != nil {
            rw.WriteString("Error\n")
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
        } else {
            rw.WriteString(fmt.Sprintf("%s\n", address))
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
        }
    }
}


// Constructor for LCA Manager instance
// If serviceName is empty string start instance in "anonymous mode"
func NewLCAManager(ctx context.Context, serviceName string, serviceAddress string) (LCAManager, error) {
    var err error

    var node LCAManager

    config := p2pnode.NewConfig()
    config.StreamHandlers = []network.StreamHandler{NewLCAManagerHandler(serviceAddress)}
    config.HandlerProtocolIDs = []protocol.ID{LCAManagerProtocolID}
    if serviceName != "" {
        // Set rendezvous to service hash value
        node.P2PHash, _, err = hashlookup.GetHash(serviceName)
        if err != nil {
            return node, err
        }
        config.Rendezvous = []string{node.P2PHash}
    } else {
        // Set no rendezvous (anonymous mode)
        config.Rendezvous = []string{}
    }
    node.Host, err = p2pnode.NewNode(ctx, config)
    if err != nil {
        return node, err
    }

    return node, nil
}
