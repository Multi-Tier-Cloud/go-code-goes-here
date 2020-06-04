package main

import (
    "context"
    "encoding/json"
    "flag"
    "io/ioutil"
    "log"
    "os"
    "net/http"

    "github.com/libp2p/go-libp2p-core/pnet"

    "github.com/multiformats/go-multiaddr"

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
    var bootstraps *[]multiaddr.Multiaddr
    var psk *pnet.PSK
    if keyFlags, err = util.AddKeyFlags(defaultKeyFile); err != nil {
        log.Fatalln(err)
    }
    if bootstraps, err = util.AddBootstrapFlags(); err != nil {
        log.Fatalln(err)
    }
    if psk, err = util.AddPSKFlag(); err != nil {
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
        log.Fatalln(err)
    }
    defer configFile.Close()

    configByte, err := ioutil.ReadAll(configFile)
    if err != nil {
        log.Fatalln(err)
    }
    err = json.Unmarshal(configByte, &config)
    if err != nil {
        log.Fatalln(err)
    }

    // If CLI didn't specify any bootstraps, fallback to configuration file.
    // If configuration file doesn't contain bootstraps, fallback to
    // checking environment variables.
    if len(*bootstraps) == 0 {
        if len(config.Bootstraps) == 0 {
            envBootstraps, err := util.GetEnvBootstraps()
            if err != nil {
                log.Fatalln(err)
            }

            if len(envBootstraps) == 0 {
                log.Fatalf("ERROR: Must specify at least one bootstrap node " +
                    "through a command line flag, the configuration file, or " +
                    "setting the %s environment variable.", util.ENV_KEY_BOOTSTRAPS)
            }

            *bootstraps = envBootstraps
        } else {
            *bootstraps, err = util.StringsToMultiaddrs(config.Bootstraps)
            if err != nil {
                log.Fatalln(err)
            }
        }
    }

    // Set node configuration
    nodeConfig := p2pnode.NewConfig()
    nodeConfig.PrivKey = priv
    nodeConfig.BootstrapPeers = *bootstraps
    nodeConfig.PSK = *psk

    // Spawn LCA Allocator
    log.Println("Spawning LCA Allocator")
    _, err = lca.NewLCAAllocator(ctx, nodeConfig)
    if err != nil {
        log.Fatalln(err)
    }

    // Wait for connection
    log.Println("Waiting for requests...")
    select {}
}
