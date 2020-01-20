package main

import (
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

var lcaClient lca.LCAClient

// Stub
func getDNSMapping(serviceID string) (string, error) {
    return serviceID, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "Got request: %s", r.URL.Path[1:])

    // 1. separate first section (service name) and rest (arguments)
    serviceID := r.URL.Path[1:] // TODO: make this support arguments
    serviceHash, err := getDNSMapping(serviceID)
    if err != nil {
        panic(err)
    }

    // 3. if does not exist, use libp2p connection to find/create service
    serviceAddress, err := lcaClient.FindService(serviceHash)
    if err != nil {
        serviceAddress, err = lcaClient.AllocService(serviceHash)
        if err != nil {
            panic(err)
        }
    }

    // 5. run request
    resp, err := http.Get(serviceAddress)
    if err != nil {
        resp.Body.Close()
        fmt.Fprintf(w, "%s", err)
    }
    defer resp.Body.Close()

    // 7. return result
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Fprintf(w, "%s", err)
    }

    fmt.Fprintf(w, string(body))
}

func main() {

    // Setup LCA Client
    var err error
    ctx := context.Background()
    lcaClient, err = lca.NewLCAClient(ctx, "hello-world-server", "10.11.17.3:8080")
    if err != nil {
        panic(err)
    }

    // Setup HTTP proxy service
    http.HandleFunc("/", handler)
    // make 4201 the dedicated port for the proxy
    log.Fatal(http.ListenAndServe(":4201", nil))
}
