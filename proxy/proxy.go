package main

import (
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

)

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Handles the "proxying" part of proxy
func requestHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("Got request:", r.URL.Path[1:])

    // 1. Find service information and arguments from URL
    tokens := strings.SplitN(r.URL.Path, "/", 3)
    fmt.Println(tokens)
    // tokens[0] should be an empty string from parsing the initial "/"
    serviceName := tokens[1]
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

    // TODO: 2. Search for cached instances

    // If does not exist, use libp2p connection to find/create service
    fmt.Println("Finding best existing service instance")
    serviceAddress, err := manager.FindService(serviceHash)
    if err != nil {
        fmt.Println("Could not find, creating new service instance")
        serviceAddress, err = manager.AllocService(dockerHash)
        if err != nil {
            fmt.Println("No services able to be found or created")
            panic(err)
        }
    }

    // TODO: Cache peer information

    // Run request
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
}

func main() {
    var err error

    // Setup LCA Manager
    ctx := context.Background()

    // Parse command line arguments
    if len(os.Args) == 1 {
        fmt.Println("Starting LCA Manager in anonymous mode")
        manager, err = lca.NewLCAManager(ctx, "", "")
    } else if len(os.Args) == 3 {
        fmt.Println("Starting LCA Manager in service mode with arguments",
                    os.Args[1], os.Args[2])
        manager, err = lca.NewLCAManager(ctx, os.Args[1], os.Args[2])
    } else {
        fmt.Println("Usage:")
        fmt.Println("$./proxyserver [service address]")
        fmt.Println("    service: name of the service for proxy to bind to")
        fmt.Println("    address: IP:PORT of the service for proxy to bind to")
        fmt.Println("If service and address are omitted, proxy launches in anonymous mode")
        fmt.Println("\"anonymous mode\" allows a client to access the network without")
        fmt.Println("having to register and advertise itself in the network")
        os.Exit(1)
    }
    if err != nil {
        panic(err)
    }

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    fmt.Println("Starting HTTP Proxy on 127.0.0.1:4201")
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe(":4201", nil))
}
