package lca

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "regexp"
    "runtime/debug"
    "log"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"

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
func (lca *LCAManager) FindService(serviceHash string) (peer.ID, string, p2putil.PerfInd, error) {
    log.Println("Finding providers for:", serviceHash)

    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Find peers
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, serviceHash)
    if err != nil {
        return peer.ID(""), "", p2putil.PerfInd{}, err
    }

    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Print out RTT for testing
    //for i, p := range peers {
    //    log.Println(i, ":", p.ID, ":", p.Perf)
    //}

    for _, p := range peers {
        // Get microservice address from peer's proxy
        log.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(ctx, p.ID, LCAManagerProtocolID)
        if err != nil {
            continue
        } else {
            defer stream.Reset()
            rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
            str, err := read(rw)
            if err != nil {
                if err == io.EOF {
                    log.Println("Error: Incoming stream unexpectedly closed")
                }
                log.Printf("Error reading from buffer: %v\n" +
                    "Buffer contents received: %s\n", err, str)
                return peer.ID(""), "", p2putil.PerfInd{}, err
            }

            stream.Close()

            log.Println("Got response from peer:", str)
            return p.ID, str, p.Perf, nil
        }
    }

    return peer.ID(""), "", p2putil.PerfInd{}, errors.New("Could not find peer offering service")
}

// Helper function to AllocService that handles the communication with LCA Allocator
func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    // Send command Start Program"
    err := write(rw, fmt.Sprintf("%s %s\n", LCAAPCmdStartProgram, serviceHash))
    if err != nil {
        log.Println("Error writing to buffer")
        return "", err
    }

    str, err := read(rw)
    if err != nil {
        if err == io.EOF {
            log.Println("Error: Incoming stream unexpectedly closed")
        }
        log.Printf("Error reading from buffer: %v\n" +
            "Buffer contents received: %s\n", err, str)
        return "", err
    }

    // Parse IP address and Port
    log.Println("New instance:", str)
    match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,5}$", []byte(str))
    if err != nil {
        log.Println("Error performing regex match")
        return "", err
    }

    if match {
        return str, nil
    }

    return "", errors.New("Returned address does not match format")
}

// Requests allocation on LCA Allocators with good network performance
func (lca *LCAManager) AllocService(serviceHash string) (peer.ID, string, p2putil.PerfInd, error) {
    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Look for Allocators
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, LCAAllocatorRendezvous)
    if err != nil {
        return peer.ID(""), "", p2putil.PerfInd{}, err
    }

    // Sort Allocators based on performance
    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Print out RTT for testing
    //for i, p := range peers {
    //    log.Println(i, ":", p.ID, ":", p.Perf)
    //}

    // Request allocation until one succeeds then return allocated service address
    for _, p := range peers {
        log.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(ctx, p.ID, LCAAllocatorProtocolID)
        if err != nil {
            continue
        } else {
            defer stream.Reset()
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return p.ID, result, p.Perf, nil
        }
    }

    return peer.ID(""), "", p2putil.PerfInd{}, errors.New("Could not find peer to allocate service")
}

// Requests allocation on LCA Allocators with performance better than "perf"
func (lca *LCAManager) AllocBetterService(
    serviceHash string, perf p2putil.PerfInd,
) (
    peer.ID, string, p2putil.PerfInd, error,
) {
    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Look for Allocators
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, LCAAllocatorRendezvous)
    if err != nil {
        return peer.ID(""), "", p2putil.PerfInd{}, err
    }

    // Sort Allocators based on performance
    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Print out RTT for testing
    //for i, p := range peers {
    //    log.Println(i, ":", p.ID, ":", p.Perf)
    //}

    // Request allocation until one succeeds then return allocated service address
    for _, p := range peers {
        if p2putil.PerfIndCompare(perf, p.Perf) {
            return peer.ID(""), "", p2putil.PerfInd{}, errors.New("Could not find better service")
        }
        log.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(ctx, p.ID, LCAAllocatorProtocolID)
        if err != nil {
            continue
        } else {
            defer stream.Reset()
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return p.ID, result, p.Perf, nil
        }
    }

    return peer.ID(""), "", p2putil.PerfInd{}, errors.New("Could not find peer to allocate service")
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
        defer stream.Close()
        defer func() {
            // Don't crash the whole program if panic() called
            // Print stack trace for debug info
            if r := recover(); r != nil {
                fmt.Printf("Stream handler for %s panic'd:\n%s\n",
                    address, string(debug.Stack()))
            }
        }()

        log.Println("Got a new LCA Manager request")
        rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
        err := pingService()
        if err != nil {
            err = write(rw, LCAPErrDeadProgram)
            if err != nil {
                log.Println("Error writing to buffer")
                panic(err)
            }
        } else {
            err = write(rw, fmt.Sprintf("%s\n", address))
            if err != nil {
                log.Println("Error writing to buffer")
                panic(err)
            }
        }
    }
}


// Constructor for LCA Manager instance
// If serviceName is empty string start instance in "anonymous mode"
func NewLCAManager(ctx context.Context, cfg p2pnode.Config,
                    serviceName string, serviceAddress string) (LCAManager, error) {
    var err error

    var node LCAManager

    // Set stream handler, protocol ID, and hash to advertise
    cfg.StreamHandlers = append(cfg.StreamHandlers, NewLCAManagerHandler(serviceAddress))
    cfg.HandlerProtocolIDs = append(cfg.HandlerProtocolIDs, LCAManagerProtocolID)
    if serviceName != "" {
        // Set rendezvous to service hash value
        node.P2PHash, _, err = hashlookup.GetHash(cfg.BootstrapPeers, cfg.PSK, serviceName)
        if err != nil {
            return node, err
        }
        cfg.Rendezvous = append(cfg.Rendezvous, node.P2PHash)
    }
    node.Host, err = p2pnode.NewNode(ctx, cfg)
    if err != nil {
        return node, err
    }

    return node, nil
}
