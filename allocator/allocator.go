package main

import (
    "context"
    "fmt"

    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

func main () {
    ctx := context.Background()

    // Spawn LCA Allocator
    fmt.Println("Spawning LCA Allocator")
    _, err := lca.NewLCAAllocator(ctx)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    fmt.Println("Waiting for requests...")
    select {}
}
