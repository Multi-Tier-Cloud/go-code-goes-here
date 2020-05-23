package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"
    "net/http"

    "github.com/Multi-Tier-Cloud/common/util"
    "github.com/Multi-Tier-Cloud/service-manager/conf"
    "github.com/Multi-Tier-Cloud/service-manager/lca"

    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/libp2p/go-libp2p-core/crypto"
)

var (
    // TODO: Move key-related flags into common?
    algo = flag.String("algo", "RSA",
        "Cryptographic algorithm to use for generating the key.\n"+
            "Will be ignored if 'genkey' is false.\n"+
            "Must be one of {RSA, Ed25519, Secp256k1, ECDSA}")
    bits = flag.Int("bits", 2048,
        "Key length, in bits. Will be ignored if 'algo' is not RSA.")
    keyFileAlloc = flag.String("keyfile", "~/.privKeyAlloc",
        "Location of private key to read from (or write to, if generating).")
    ephemeral = flag.Bool("ephemeral", false,
        "Generate a new key just for this run, and don't store it to file.\n"+
            "If 'keyfile' is specified, it will be ignored.")
    configPath = flag.String("configfile", "../conf/conf.json", "path to config file to use")
)

func main () {
    // Parse options
    flag.Parse()

    var priv crypto.PrivKey
    var err error

    if *ephemeral {
        fmt.Println("Generating a new key...")
        if priv, err = util.GeneratePrivKey(*algo, *bits); err != nil {
            fmt.Printf("ERROR: Unable to generate key\n%v", err)
            os.Exit(1)
        }
    } else {
        if util.FileExists(*keyFileAlloc) {
            if priv, err = util.LoadPrivKeyFromFile(*keyFileAlloc); err != nil {
                fmt.Printf("ERROR: Unable to load key from file\n%v", err)
                os.Exit(1)
            }
        } else {
            fmt.Printf("Key does not exist at location: %s.\n", *keyFileAlloc)
            fmt.Println("Generating a new key...")
            if priv, err = util.GeneratePrivKey(*algo, *bits); err != nil {
                fmt.Printf("ERROR: Unable to generate key\n%v", err)
                os.Exit(1)
            }

            if err = util.StorePrivKeyToFile(priv, *keyFileAlloc); err != nil {
                fmt.Printf("ERROR: Unable to save key to file %s\n", *keyFileAlloc)
                os.Exit(1)
            }
            fmt.Println("New key is stored at:", *keyFileAlloc)
        }
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
    fmt.Println("Spawning LCA Allocator")
    _, err = lca.NewLCAAllocator(ctx, config.Bootstraps, priv)
    if err != nil {
        panic(err)
    }

    // Wait for connection
    fmt.Println("Waiting for requests...")
    select {}
}
