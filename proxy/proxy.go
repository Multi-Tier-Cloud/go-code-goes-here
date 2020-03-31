package main

import (
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/Multi-Tier-Cloud/common/p2putil"
    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/service-manager/pcache"
)

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var cache pcache.PeerCache
var rchan chan pcache.PeerRequest

// Handles the "proxying" part of proxy
func requestHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("Got request:", r.URL.Path[1:])

    // 1. Find service information and arguments from URL
    tokens := strings.SplitN(r.URL.Path, "/", 3)
    fmt.Println(tokens)
    // tokens[0] should be an empty string from parsing the initial "/"
    serviceName := tokens[1]
    fmt.Println("Looking for service with name", serviceName, "in hash-lookup")
    serviceHash, dockerHash, err := hashlookup.GetHashWithHostRouting(
        manager.Host.Ctx, manager.Host.Host,
        manager.Host.RoutingDiscovery, serviceName,
    )
    if err != nil {
        fmt.Fprintf(w, "%s\n", err)
        panic(err)
    }
    arguments := ""
    // check if arguments exist
    if len(tokens) == 3 {
        arguments = tokens[2]
    }

    serviceAddress := ""
    // 2. Search for cached instances
    // TODO: allow search by service name
    _, serviceAddress, err = cache.GetPeer(serviceHash)
    if err != nil {
        // If does not exist, use libp2p connection to find/create service
        fmt.Println("Finding best existing service instance")
        startTime := time.Now()
        id, serviceAddress, err := manager.FindService(serviceHash)
        if err != nil {
            fmt.Println("Could not find, creating new service instance")
            id, serviceAddress, err = manager.AllocService(dockerHash)
            if err != nil {
                fmt.Println("No services able to be found or created")
                fmt.Fprintf(w, "%s\n", err)
                panic(err)
            }
        }

        elapsedTime := time.Now().Sub(startTime)
        fmt.Println("Took", elapsedTime)

        // Cache peer information and loop again
        rchan <- pcache.PeerRequest{ID: id, Hash: serviceHash, Address: serviceAddress}
    }

    // Run request
    if serviceAddress != "" {
        request := fmt.Sprintf("http://%s/%s", serviceAddress, arguments)
        fmt.Println("Running request:", request)
        resp, err := http.Get(request)
        if err != nil {
            fmt.Fprintf(w, "%s\n", err)
            panic(err)
        }
        defer resp.Body.Close()

        // Return result
        fmt.Println("Sending response back to requester")
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            fmt.Fprintf(w, "%s\n", err)
            panic(err)
        }

        fmt.Fprintf(w,"%s\n", string(body))
        fmt.Println("Response from service:", string(body))
    } else {
        fmt.Fprintf(w,"%s\n", "Error: Could not find service")
    }
}

func main() {
    var err error

    // Setup LCA Manager
    ctx := context.Background()

    // Parse command line arguments
    if len(os.Args) == 2 {
        fmt.Println("Starting LCA Manager in anonymous mode")
        manager, err = lca.NewLCAManager(ctx, "", "")
    } else if len(os.Args) == 4 {
        fmt.Println("Starting LCA Manager in service mode with arguments",
                    os.Args[2], os.Args[3])
        manager, err = lca.NewLCAManager(ctx, os.Args[2], os.Args[3])
    } else {
        fmt.Println("Usage:")
        fmt.Println("$./proxyserver PORT [SERVICE ADDRESS]")
        fmt.Println("    PORT: local port for proxy to run on")
        fmt.Println("    SERVICE: name of the service for proxy to bind to")
        fmt.Println("    ADDRESS: IP:PORT of the service for proxy to bind to")
        fmt.Println("If service and address are omitted, proxy launches in anonymous mode")
        fmt.Println("\"anonymous mode\" allows a client to access the network without")
        fmt.Println("having to register and advertise itself in the network")
        os.Exit(1)
    }
    if err != nil {
        panic(err)
    }

    // Setup cache
    reqPerf := p2putil.PerfInd{RTT: 10} // BS RTT for now
    cache = pcache.NewPeerCache(reqPerf)
    // Boot up managing function
    // Make rchan blocking to allow as much time as possible for the cache
    // to add a new instance before the microservice requests it again
    rchan = make(chan pcache.PeerRequest, 0)
    go pcache.UpdateCache(&manager.Host, rchan, &cache)

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    fmt.Println("Starting HTTP Proxy on 127.0.0.1:" + os.Args[1])
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe("127.0.0.1:" + os.Args[1], nil))
}
