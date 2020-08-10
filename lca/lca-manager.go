package lca

import (
    "bufio"
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "io/ioutil"
    "net/http"
    "regexp"
    "runtime/debug"
    "strings"
    "log"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/PhysarumSM/common/p2pnode"
    "github.com/PhysarumSM/common/p2putil"
    "github.com/PhysarumSM/service-registry/registry"
)

//  for p2pnode.Node and also related
type LCAManager struct {
    // Libp2p node instance for this node
    Host       p2pnode.Node
    // Identifier hash for the service this node is responsible for
    P2PHash    string
}

// Stub
// Used to check the health of the service the LCA Manager is responsible for
func pingService() error {
    return nil
}

// Helper function to AllocService that handles the communication with LCA Allocator
// Returns the new service's in-container IP:port pair, and any errors
//   - NOTE: The service's IP:port pair is not really needed, but we'll keep it
//           for potential debugging purposes.
func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    // Send command Start Program"
    err := write(rw, fmt.Sprintf("%s %s", LCAAPCmdStartProgram, serviceHash))
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

// Requests allocation on LCA Allocators with performance better than "perf"
func (lca *LCAManager) AllocBetterService(
    serviceHash string, perf p2putil.PerfInd,
) (
    peer.ID, p2putil.PerfInd, error,
) {
    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Look for Allocators
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, LCAAllocatorRendezvous)
    if err != nil {
        return peer.ID(""), p2putil.PerfInd{}, err
    }

    // Sort Allocators based on performance
    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Request allocation until one succeeds then return allocated service address
    for _, p := range peers {
        if perf.LessThan(p.Perf) {
            return peer.ID(""), p2putil.PerfInd{}, errors.New("Could not find better service")
        }
        log.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(ctx, p.ID, LCAAllocatorProtocolID)
        if err != nil {
            log.Printf("ERROR: Unable to contact allocator %s\n%v\n", p.ID, err)
            continue
        }
        defer stream.Reset()

        _, err = requestAlloc(stream, serviceHash)
        if err != nil {
            log.Printf("ERROR: Unable to allocate service %s using allocator %s\n%v\n",
                        serviceHash, p.ID, err)
            continue
        }

        return p.ID, p.Perf, nil
    }

    return peer.ID(""), p2putil.PerfInd{}, errors.New("Could not find peer to allocate service")
}

// Requests allocation on LCA Allocators with good network performance
// Returns:
//  - The peer ID of the allocator that spawned the new instance
//  - The performance of the allocator that spawned the instance
//  - Any errors.
func (lca *LCAManager) AllocService(serviceHash string) (peer.ID, p2putil.PerfInd, error) {
    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Look for Allocators
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, LCAAllocatorRendezvous)
    if err != nil {
        return peer.ID(""), p2putil.PerfInd{}, err
    }

    // Sort Allocators based on performance
    peers := p2putil.SortPeers(peerChan, lca.Host)

    // Request allocation until one succeeds then return allocated service address
    for _, p := range peers {
        log.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(ctx, p.ID, LCAAllocatorProtocolID)
        if err != nil {
            log.Printf("ERROR: Unable to contact allocator %s\n%v\n", p.ID, err)
            continue
        }
        defer stream.Reset()

        _, err = requestAlloc(stream, serviceHash)
        if err != nil {
            log.Printf("ERROR: Unable to allocate service %s using allocator %s\n%v\n",
                        serviceHash, p.ID, err)
            continue
        }

        return p.ID, p.Perf, nil
    }

    return peer.ID(""), p2putil.PerfInd{}, errors.New("Could not find peer to allocate service")
}

// TODO: Move this outside of LCAManager to a more general structure or library?
//       It's not relevant to controlling the lifecycle of virtual resources.
// Finds the best service instance by pinging other LCA Manager instances
// Returns:
//  - ID of candidate peer
//  - Latency to candidate peer
//  - Any errors
func (lca *LCAManager) FindService(serviceHash string) (peer.ID, p2putil.PerfInd, error) {
    log.Println("Finding providers for:", serviceHash)

    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    // Find peers
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(ctx, serviceHash)
    if err != nil {
        return peer.ID(""), p2putil.PerfInd{}, err
    }

    peers := p2putil.SortPeers(peerChan, lca.Host)
    if len(peers) > 0 {
        return peers[0].ID, peers[0].Perf, nil
    }

    return peer.ID(""), p2putil.PerfInd{}, errors.New("Could not find peer offering service")
}

// TODO: Move this outside of LCAManager to a more general structure or library?
//       It's not relevant to controlling the lifecycle of virtual resources.
func (lca *LCAManager) Request(pid peer.ID, req *http.Request) (*http.Response, error) {
    // Setup context
    ctx, cancel := context.WithCancel(lca.Host.Ctx)
    defer cancel()

    log.Println("Attempting to contact peer with pid:", pid)
    stream, err := lca.Host.Host.NewStream(ctx, pid, LCAManagerRequestProtID)
    if err != nil {
        return nil, errors.New("Error: could not connect to microservice peer")
    }
    defer stream.Reset()

    err = req.Write(stream)
    if err != nil {
        return nil, errors.New(LCASErrWriteFail)
    }

    // NOTE: Wrapping the 'stream' in a bufio.Reader and using that to construct
    //       a Response leads to a stream leak, as Response.Body.Close() does not
    //       close the stream. If we close the stream here in this function, it
    //       prevents the Response.Body from being read later on. ¯\_(ツ)_/¯
    //
    //       Thus, we copy the response body to a temporary buffer and use that
    //       to construct the response. This strategy currently does not support
    //       streaming HTTP applications (e.g. video); all response content must
    //       fit into a single Response.
    bodyBuf, err := ioutil.ReadAll(stream)
    if err != nil {
        return nil, fmt.Errorf("Unable to read from stream\n%w\n", err)
    }
    r := bufio.NewReader(bytes.NewBuffer(bodyBuf))
    resp, err := http.ReadResponse(r, req)
    if err != nil {
        if resp != nil && resp.Body != nil {
            resp.Body.Close()
        }
        return nil, errors.New("Error: could not receive response")
    }

    return resp, nil
}

// TODO: Finish this function
// LCAManagerHandler generator function
// Used to allow the Handler to remember the service address
// Generated LCAManagerHandler pings the service it is responsible for to check
// that it is still up then sends the service address back to the requester.
func RequestHandler(address string) func(network.Stream) {
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

        log.Println("Got a new LCA Manager Request request")
        r := bufio.NewReader(stream)
        w := bufio.NewWriter(stream)
        rw := bufio.NewReadWriter(r, w)
        err := pingService()
        if err != nil {
            err = write(rw, LCAPErrDeadProgram)
            if err != nil {
                log.Println("Error writing to buffer")
                panic(err)
            }
        }

        req, err := http.ReadRequest(r)
        if req != nil {
            defer req.Body.Close()
        }
        if err != nil {
            if err == io.EOF {
                log.Println("Error: Incoming stream unexpectedly closed")
            }
            log.Printf("Error reading from buffer\n")
            panic(err)
        }

        // URL.RequestURI() includes path?query (URL.Path only has the path)
        tokens := strings.SplitN(req.URL.RequestURI(), "/", 3)
        log.Println(tokens)
        // tokens[0] should be an empty string from parsing the initial "/"
        arguments := ""
        // check if arguments exist
        if len(tokens) == 3 {
            arguments = tokens[2]
        }

        req.URL, err = req.URL.Parse(fmt.Sprintf("http://%s/%s", address, arguments))
        if err != nil {
            log.Println("Error: invalid constructed URL from arguments")
            err = write(rw, LCAPErrDeadProgram)
            if err != nil {
                log.Println("Error writing to buffer")
                panic(err)
            }
        }

        log.Println("Proxying request to service, request", req.URL)
        outreq := new(http.Request)
        *outreq = *req
        resp, err := http.DefaultTransport.RoundTrip(outreq)
        if resp != nil {
            defer resp.Body.Close()
        }
        if err != nil {
            log.Println("Error: invalid response from server")
            err = write(rw, LCAPErrDeadProgram)
            if err != nil {
                log.Println("Error writing to buffer")
                panic(err)
            }
        }

        err = resp.Write(stream)
        if err != nil {
            panic(err)
        }
    }
}

// Constructor for LCA Manager instance
// If serviceName is empty string start instance in "anonymous mode"
func NewLCAManager(ctx context.Context, cfg p2pnode.Config,
                    serviceName string, serviceAddress string) (LCAManager, error) {
    var err error

    var node LCAManager

    // Set stream handler, protocol ID and create the node
    cfg.StreamHandlers = append(cfg.StreamHandlers, RequestHandler(serviceAddress))
    cfg.HandlerProtocolIDs = append(cfg.HandlerProtocolIDs, LCAManagerRequestProtID)
    node.Host, err = p2pnode.NewNode(ctx, cfg)
    if err != nil {
        return node, err
    }

    // Now that the node is created, it can be used to get the rendezvous
    // without the need to create a separate node for GetService
    if serviceName != "" {
        // Set rendezvous to service hash value
        info, err := registry.GetServiceWithHostRouting(node.Host.Ctx,
                         node.Host.Host, node.Host.RoutingDiscovery, serviceName)
        if err != nil {
            return node, err
        }
        node.P2PHash = info.ContentHash
        node.Host.Advertise(node.P2PHash)
    }

    return node, nil
}
