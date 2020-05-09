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

    "github.com/Multi-Tier-Cloud/common/p2putil"

    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/service-manager/pcache"
    "github.com/Multi-Tier-Cloud/service-manager/conf"
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
    // Continuously search until an instance is found is found in cache
    for {
        id, serviceAddress, err := cache.GetPeer(serviceHash)
        if err != nil {
            // If does not exist, use libp2p connection to find/create service
            fmt.Println("Finding best existing service instance")

            startTime := time.Now()

            id, serviceAddress, perf, err := manager.FindService(serviceHash)
            if err != nil {
                fmt.Println("Could not find, creating new service instance")
                id, serviceAddress, perf, err = manager.AllocService(dockerHash)
                if err != nil {
                    fmt.Println("No services able to be found or created")
                }
            } else if p2putil.PerfIndCompare(cache.ReqPerf.SoftReq, perf) {
                fmt.Println("Found service does not meet requirements, creating new service instance")
                id2, serviceAddress2, _, err := manager.AllocBetterService(dockerHash, perf)
                if err != nil {
                    fmt.Println("No services able to be created, using previously found peer")
                } else {
                    id, serviceAddress = id2, serviceAddress2
                }
            }

            elapsedTime := time.Now().Sub(startTime)
            fmt.Println("Find/alloc service took:", elapsedTime)

            // Cache peer information and loop again
            cache.AddPeer(pcache.PeerRequest{ID: id, Hash: serviceHash, Address: serviceAddress})
            continue
        }

        // Run request
        if serviceAddress != "" {
            request := fmt.Sprintf("http://%s/%s", serviceAddress, arguments)
            fmt.Println("Running request:", request)
            resp, err := http.Get(request)
            if err != nil {
                fmt.Println(err)
                fmt.Fprintf(w, "Error: Change this to a response with error code 502")
                go cache.RemovePeer(id, serviceAddress)
                return
            }
            defer resp.Body.Close()

            // Return result
            // This returns errors as well
            // Ideally this would find another instance if there is an error
            // but just keep this behaviour for now
            fmt.Println("Sending response back to requester")
            body, err := ioutil.ReadAll(resp.Body)
            fmt.Fprintf(w, string(body))
            return
        }
    }
}

func main() {
    var err error

    ctx := context.Background()

    // TODO: Parse command line arguments
    mode := "anonymous"
    configPath := "../conf/conf.json"
    if true {
        fmt.Println("Usage:")
        fmt.Println("$./proxy PORT [SERVICE ADDRESS] [--configfile PATH]")
        fmt.Println("    PORT: local port for proxy to run on")
        fmt.Println("    SERVICE: name of the service for proxy to bind to")
        fmt.Println("    ADDRESS: IP:PORT of the service for proxy to bind to")
        fmt.Println("    PATH: path to config file to use")
        fmt.Println("If service and address are omitted, proxy launches in anonymous mode")
        fmt.Println("\"anonymous mode\" allows a client to access the network without")
        fmt.Println("having to register and advertise itself in the network")
        os.Exit(1)
    }
    if err != nil {
        panic(err)
    }

    // Read in config file
    config := conf.Config{}
    configFile, err := os.Open(configPath)
    if err != nil {
        panic(err)
    }
    configByte, err := ioutil.ReadAll(configFile)
    if err != nil {
        configFile.Close()
        panic(err)
    }
    err = json.Unmarshal(configByte, &config)
    if err != nil {
        configFile.Close()
        panic(err)
    }
    configFile.Close()

    // Setup LCA Manager
    if mode == "anonymous" {
        fmt.Println("Starting LCA Manager in anonymous mode")
        manager, err = lca.NewLCAManager(ctx, "", "", config.Bootstraps)
    } else {
        fmt.Println("Starting LCA Manager in service mode with arguments",
                    os.Args[2], os.Args[3])
        manager, err = lca.NewLCAManager(ctx, os.Args[2], os.Args[3], config.Bootstraps)
    }


    // Setup cache
    fmt.Println("Launching proxy PeerCache instance")
    fmt.Println("Setting performance requirements based on perf.conf",
        "soft limit:", config.Perf.SoftReq.RTT, "hard limit:", config.Perf.HardReq.RTT)
    // Create cache instance
    cache = pcache.NewPeerCache(config.Perf, &manager.Host)
    // Boot up cache managment function
    go cache.UpdateCache()

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    fmt.Println("Starting HTTP Proxy on 127.0.0.1:" + os.Args[1])
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe("127.0.0.1:" + os.Args[1], nil))
}
