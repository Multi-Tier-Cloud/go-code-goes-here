package main

import (
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

var lcaClient lca.LCAClient

// Stub
func getDNSMapping(serviceName string) (string, error) {
    return serviceName, nil
}

func requestHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("Got request:", r.URL.Path[1:])

    // 1. separate first section (service name) and rest (arguments)
    serviceName := r.URL.Path[1:] // TODO: make this support arguments
    //serviceHash, err := getDNSMapping(serviceName)
    serviceHash := "hello-world-server"
    if err != nil {
        fmt.Fprintf(w, "%s\n", err)
        panic(err)
    }

    // 3. if does not exist, use libp2p connection to find/create service
    fmt.Println("Finding best existing service instance")
    serviceAddress, err := lcaClient.FindService(serviceHash)
    if err != nil {
        fmt.Println("Could not find, creating new service instance")
        serviceAddress, err = lcaClient.AllocService(serviceHash)
        if err != nil {
            fmt.Println("No services able to be found or created")
            panic(err)
        }
    }

    // 5. run request
    fmt.Println("Running request")
    resp, err := http.Get(serviceAddress)
    if err != nil {
        fmt.Fprintf(w, "%s\n", err)
        panic(err)
    }
    defer resp.Body.Close()

    // 7. return result
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Fprintf(w, "%s\n", err)
        panic(err)
    }

    fmt.Fprintf(w,"%s\n", string(body))
    fmt.Println(string(body))
}

func main() {
    var err error

    // Setup LCA Client
    ctx := context.Background()
    switch len(os.Args) {
        case 1: {
            fmt.Println("Starting LCAClient in client mode")
            lcaClient, err = lca.NewLCAClient(ctx, "_do_not_find", "_do_not_find")
        }
        case 3: {
            fmt.Println("Starting LCAClient in service mode with arguments",
                        os.Args[1], os.Args[2])
            //lcaClient, err = lca.NewLCAClient(ctx, os.Args[1], os.Args[2])
            lcaClient, err = lca.NewLCAClient(ctx, "hello-world-server", "10.11.17.3")
        }
        default: {
            panic("Usage:$./proxyserver [serviceName ipAddress:port]")
        }
    }
    if err != nil {
        panic(err)
    }

    // Setup HTTP proxy service
    fmt.Println("Starting HTTP Proxy on 127.0.0.1:4201")
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe(":4201", nil))
}
