package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"
)

type LCAClient struct {
    // Libp2p node instance for this node
    Host       p2pnode.Node
    // Identifier hash for the service this node is responsible for
    P2PHash    string
}

// Finds the best service instance by pinging other LCA Client instances
func (lca *LCAClient) FindService(serviceHash string) (string, error) {
    fmt.Println("Finding providers for:", serviceHash)
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, serviceHash)
    if err != nil {
        panic(err)
    }

    peers := p2putil.SortPeers(peerChan, lca.Host)

    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAClientProtocolID)
        if err != nil {
            continue
        } else {
            rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
            str, err := rw.ReadString('\n')
            if err != nil {
                fmt.Println("Error reading from buffer")
                return "", err
            }
            str = strings.TrimSuffix(str, "\n")

            stream.Close()

            fmt.Println("Got response from peer:", str)
            //match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
            //if err != nil {
            //    fmt.Println("unsuccessful")
            //    return "", err
            //}

            //if match {
            //    fmt.Println("successful")
            //    return str, nil
            //}
            return str, nil
        }
    }

    return "", ErrUhOh
}

// Helper function to AllocService that handles the communication with LCA Agent
func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
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

    match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
    if err != nil {
        return "", err
    }

    if match {
        return str, nil
    }

    return "", ErrUhOh
}

// Requests allocation on nearby LCA Agents
func (lca *LCAClient) AllocService(serviceHash string) (string, error) {
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, LCAAgentRendezvous)
    if err != nil {
        return "", err
    }

    peers := p2putil.SortPeers(peerChan, lca.Host)

    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAAgentProtocolID)
        if err != nil {
            continue
        } else {
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return result, nil
        }
    }

    return "", ErrUhOh
}

// Stub
// Used to check the health of the service the LCA Client is responsible for
func pingService() error {
    return nil
}

// LCAClientHandler generator function
// Used to allow the Handler to remember the service address
// Generated LCAClientHandler pings the service it is responsible for to check
// that it is still up then sends the service address back to the requester.
func NewLCAClientHandler(address string) func(network.Stream) {
    return func(stream network.Stream) {
        fmt.Println("Got a new LCA Client request")
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


// Constructor for LCA Client instance
// If serviceName is empty string start instance in "anonymous mode"
func NewLCAClient(ctx context.Context, serviceName string, serviceAddress string) (LCAClient, error) {
    var err error

    var node LCAClient

    config := p2pnode.NewConfig()
    config.StreamHandlers = []network.StreamHandler{NewLCAClientHandler(serviceAddress)}
    config.HandlerProtocolIDs = []protocol.ID{LCAClientProtocolID}
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
