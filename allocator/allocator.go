package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"

    "github.com/Multi-Tier-Cloud/service-manager/conf"
    "github.com/Multi-Tier-Cloud/service-manager/lca"
)

func main () {
    ctx := context.Background()

    // Parse options
    var configPath string
    flag.StringVar(&configPath, "configfile", "../conf/conf.json", "path to config file to use")
    flag.Parse()

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

    // Spawn LCA Allocator
    fmt.Println("Spawning LCA Allocator")
    _, err = lca.NewLCAAllocator(ctx, config.Bootstraps)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    fmt.Println("Waiting for requests...")
    select {}
}
