package main

import (
    "context"
    "fmt"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

func main () {
    ctx := context.Background()

    // Spawn LCA Agent
    fmt.Println("Spawning LCA Agent")
    _, err := lca.NewLCAAgent(ctx)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    fmt.Println("Waiting for requests...")
    select {}
}
