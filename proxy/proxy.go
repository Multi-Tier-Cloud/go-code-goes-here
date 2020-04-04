package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/service-manager/pcache"
)

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var cache pcache.PeerCache

// Handles the "proxying" part of proxy
func requestHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("Got request:", r.URL.RequestURI()[1:])

    // 1. Find service information and arguments from URL
    // URL.RequestURI() includes path?query (URL.Path only has the path)
    tokens := strings.SplitN(r.URL.RequestURI(), "/", 3)
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

    // 2. Search for cached instances
    // Continuously search until an instance is found
    for {
        info, serviceAddress, err := cache.GetPeer(serviceHash)
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
            cache.AddPeer(pcache.PeerRequest{ID: id, Hash: serviceHash, Address: serviceAddress})
        }

        // Run request
        if serviceAddress != "" {
            request := fmt.Sprintf("http://%s/%s", serviceAddress, arguments)
            fmt.Println("Running request:", request)
            resp, err := http.Get(request)
            if err != nil {
                cache.RemovePeer(info.ID, serviceAddress)
                continue
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
            return
        } else {
            fmt.Fprintf(w,"%s\n", "Error: Could not find service")
        }
    }
}

func main() {
    var err error

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
    // Read in cache config
    fmt.Println("Launching proxy PeerCache instance")
    var reqPerf pcache.PerfConf
    perfFile, err := os.Open("perf.conf")
    if err != nil {
        panic(err)
    }
    defer perfFile.Close()
    perfByte, err := ioutil.ReadAll(perfFile)
    if err != nil {
        panic(err)
    }
    json.Unmarshal(perfByte, &reqPerf)
    reqPerf.SoftReq.RTT = reqPerf.SoftReq.RTT * time.Millisecond
    reqPerf.HardReq.RTT = reqPerf.HardReq.RTT * time.Millisecond
    fmt.Println("Setting performance requirements based on perf.conf to:",
        "soft limit:", reqPerf.SoftReq.RTT, "hard limit:", reqPerf.HardReq.RTT)
    // Create cache instance
    cache = pcache.NewPeerCache(reqPerf, &manager.Host)
    // Boot up cache managment function
    go cache.UpdateCache()

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    fmt.Println("Starting HTTP Proxy on 127.0.0.1:" + os.Args[1])
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe("127.0.0.1:" + os.Args[1], nil))
}
