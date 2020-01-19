package main

import (
    "context"
    "fmt"

    "github.com/go-code-goes-here/lca"
)

func main () {
    ctx := context.Background()

    // Spawn LCA Server
    fmt.Println("Spawning LCA Server...")
    lcaServer, err := lca.NewLCAServer(ctx)
    if err != nil {
        panic(err)
    }
    fmt.Println("Done")

    // Wait for connection
    fmt.Println("Waiting for connections...")
    select {}
}
