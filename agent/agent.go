package main

import (
    "context"
    "fmt"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

func main () {
    ctx := context.Background()

    // Spawn LCA Server
    fmt.Println("Spawning LCA Server")
    _, err := lca.NewLCAServer(ctx)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    fmt.Println("Waiting for requests...")
    select {}
}
