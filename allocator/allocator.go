package main

import (
    "context"
    "encoding/json"
    "flag"
    "io/ioutil"
    "log"
    "os"
    "net/http"

    "github.com/Multi-Tier-Cloud/common/util"
    "github.com/Multi-Tier-Cloud/service-manager/conf"
    "github.com/Multi-Tier-Cloud/service-manager/lca"

    "github.com/prometheus/client_golang/prometheus/promhttp"
)

const defaultKeyFile = "~/.privKeyAlloc"

func init() {
    // Set up logging defaults
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func main () {
    var err error

    // Parse options
    configPath := flag.String("configfile", "../conf/conf.json", "path to config file to use")
    var keyFlags util.KeyFlags
    if keyFlags, err = util.AddKeyFlags(defaultKeyFile); err != nil {
        log.Fatalln(err)
    }
    flag.Parse()

    priv, err := util.CreateOrLoadKey(keyFlags)
    if err != nil {
        log.Fatalln(err)
    }

    // Start Prometheus endpoint for stats collection
    http.Handle("/metrics", promhttp.Handler())
    go http.ListenAndServe(":9101", nil)

    ctx := context.Background()

    // Read in config file
    config := conf.Config{}
    configFile, err := os.Open(*configPath)
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
    log.Println("Spawning LCA Allocator")
    _, err = lca.NewLCAAllocator(ctx, config.Bootstraps, priv)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    log.Println("Waiting for requests...")
    select {}
}
