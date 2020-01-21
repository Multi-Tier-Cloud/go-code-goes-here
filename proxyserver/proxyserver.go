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
    fmt.Printf("Got request: %s", r.URL.Path[1:])

    // 1. separate first section (service name) and rest (arguments)
    serviceName := r.URL.Path[1:] // TODO: make this support arguments
    serviceHash, err := getDNSMapping(serviceName)
    if err != nil {
        fmt.Println(err)
        fmt.Fprintf(w, "%s", err)
        return
    }

    // 3. if does not exist, use libp2p connection to find/create service
    fmt.Printf("Finding best existing service instance")
    serviceAddress, err := lcaClient.FindService(serviceHash)
    if err != nil {
        fmt.Printf("Could not find, creating new service instance")
        serviceAddress, err = lcaClient.AllocService(serviceHash)
        if err != nil {
            fmt.Println(err)
            fmt.Fprintf(w, "%s", err)
            return
        }
    }

    // 5. run request
    fmt.Printf("Running request")
    resp, err := http.Get(serviceAddress)
    if err != nil {
        fmt.Println(err)
        fmt.Fprintf(w, "%s", err)
        return
    }
    defer resp.Body.Close()

    // 7. return result
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println(err)
        fmt.Fprintf(w, "%s", err)
        return
    }

    fmt.Println(string(body))
    fmt.Fprintf(w, string(body))
}

func main() {
    var err error

    // Setup LCA Client
    ctx := context.Background()
    switch len(os.Args) {
        case 1: {
            fmt.Println("Starting LCAClient in client mode")
            lcaClient, err = lca.NewLCAClient(ctx, "_do_not_find", "")
        }
        case 3: {
            fmt.Printf("Starting LCAClient in service mode with arguments %s, %s",
                        os.Args[1], os.Args[2])
            lcaClient, err = lca.NewLCAClient(ctx, os.Args[1], os.Args[2])
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
