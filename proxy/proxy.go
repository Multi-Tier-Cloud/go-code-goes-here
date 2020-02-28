package main

import (
    "context"
    "errors"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

)

var lcaClient lca.LCAClient

func requestHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Println("Got request:", r.URL.Path[1:])

    // 1. separate first section (service name) and rest (arguments)
    tokens := strings.SplitN(r.URL.Path, "/", 3)
    fmt.Println(tokens)
    // tokens[0] should be an empty string from parsing the initial "/"
    serviceName := tokens[1]
    serviceHash, dockerHash, err := hashlookup.GetHashWithHostRouting(
        lcaClient.Host.Ctx, lcaClient.Host.Host,
        lcaClient.Host.RoutingDiscovery, serviceName,
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

    // 3. if does not exist, use libp2p connection to find/create service
    fmt.Println("Finding best existing service instance")
    serviceAddress, err := lcaClient.FindService(serviceHash)
    if err != nil {
        fmt.Println("Could not find, creating new service instance")
        serviceAddress, err = lcaClient.AllocService(dockerHash)
        if err != nil {
            fmt.Println("No services able to be found or created")
            panic(err)
        }
    }

    // 5. run request
    request := fmt.Sprintf("http://%s/%s", serviceAddress, arguments)
    fmt.Println("Running request:", request)
    resp, err := http.Get(request)
    if err != nil {
        fmt.Fprintf(w, "%s\n", err)
        panic(err)
    }
    defer resp.Body.Close()

    // 7. return result
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

    // Setup LCA Client
    ctx := context.Background()
    // TODO: change this to use flags
    if len(os.Args) == 3 {
        if os.Args[1] == "@" {
            fmt.Println("Starting LCAClient in anonymous mode")
            lcaClient, err = lca.NewLCAClient(ctx, "", "")
        } else {
            fmt.Println("Starting LCAClient in service mode with arguments",
                        os.Args[1], os.Args[2])
            lcaClient, err = lca.NewLCAClient(ctx, os.Args[1], os.Args[2])
        }
    } else {
        fmt.Println("Usage:")
        fmt.Println("$./proxyserver [serviceName ipAddress:port]")
        err = errors.New("Improper usage")
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
