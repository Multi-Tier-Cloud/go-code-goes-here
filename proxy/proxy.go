package main

import (
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/pnet"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
    "github.com/Multi-Tier-Cloud/common/util"

    "github.com/Multi-Tier-Cloud/service-registry/registry"

    "github.com/Multi-Tier-Cloud/service-manager/conf"
    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/service-manager/pcache"
    "github.com/Multi-Tier-Cloud/service-manager/rcache"
)

const defaultKeyFile = "~/.privKeyProxy"

func init() {
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var peerCache *pcache.PeerCache

// Global Registry Cache instance to cache service registry info
var registryCache *rcache.RegistryCache

func runRequest(servName string, servInfo registry.ServiceInfo, req *http.Request) (*http.Response, error) {
    var err error
    var id peer.ID
    var serviceAddress string
    var perf p2putil.PerfInd
    serviceHash := servInfo.ContentHash
    dockerHash := servInfo.DockerHash

    // 2. Search for cached instances, allocate new instance if none found
    id, err = peerCache.GetPeer(serviceHash)
    log.Printf("Get peer returned ID %s, serviceAddr %s, and err %v\n", id, serviceAddress, err)
    if err != nil {
        // Search for an instance in the network, allocating a new one if need be.
        // Maximum of 3 allocation attempts.
        // TODO: It's totally possible for an allocation attempt to succeed,
        // but the service takes a long time to come up, leading to subsequent
        // allocation attempts. This is a future problem to solve.
        serviceAddress = ""
        startTime := time.Now()
        for attempts := 0; attempts < 3 && serviceAddress == ""; attempts++ {
            if attempts > 0 {
                log.Printf("Unable to successfully find or allocate, retrying...")
            }

            // TODO: Always pass perf req into AllocService()
            //       Need to combine AllocService and AllocBetterService
            //       Need to obtain perf req from registry-service
            log.Println("Finding best existing service instance")
            id, serviceAddress, perf, err = manager.FindService(serviceHash)
            if err != nil {
                log.Println("Could not find, creating new service instance")
                _, _, _, err = manager.AllocService(dockerHash)
                if err != nil {
                    log.Println("Service allocation failed\n", err)
                }

                // Re-do FindService() to ensure the new instance is connected
                // to the network. Sleep 200ms or so to allow the service to
                // come up. If not found, perform exponential backoff and attempt
                // to re-find it (max 3 times). If it's still found, there may be
                // something wrong with it (or it's taking too long to boot).
                wait := 200 * time.Millisecond
                time.Sleep(wait)
                backoff, err := util.NewExpoBackoffAttempts(wait, time.Second, 5)
                if err != nil {
                    log.Printf("ERROR: Unable to create ExpoBackoffAttempts\n")
                }
                for backoff.Attempt() {
                    id, serviceAddress, perf, err = manager.FindService(serviceHash)
                    if err == nil && serviceAddress != "" {
                        break
                    }
                }
            } else if p2putil.PerfIndCompare(servInfo.NetworkSoftReq, perf) {
                log.Printf("Found service's performance (%s) does not meet requirements (%s)\n", perf, servInfo.NetworkSoftReq)
                id, serviceAddress, _, err = manager.AllocBetterService(dockerHash, perf)
                if err != nil {
                    log.Println("No services able to be created, using previously found peer")
                }
            }

            if err == nil && serviceAddress != "" {
                // Cache peer information
                peerCache.AddPeer(p2putil.PeerInfo{
                    ID: id,
                    ServName: servName,
                    ServHash: serviceHash,
                })
            }
        }

        elapsedTime := time.Now().Sub(startTime)
        log.Println("Find/alloc service took:", elapsedTime)
    }

    if serviceAddress == "" || id == peer.ID("") {
        return nil, errors.New("Not found")
    }
    log.Printf("Running request to peer ID %s, serviceAddr %s\n",
               id, serviceAddress)
    resp, err := manager.Request(id, req)
    if err != nil {
        log.Printf("ERROR: HTTP request over P2P failed\n%v\n", err)
        go peerCache.RemovePeer(id)
    }

    return resp, err
}

// Handles the "proxying" part of proxy
// TODO: Refactor this function
func httpRequestHandler(w http.ResponseWriter, r *http.Request) {
    log.Println("Got request:", r.URL.RequestURI()[1:])

    // URL.RequestURI() includes path?query (URL.Path only has the path)
    tokens := strings.SplitN(r.URL.RequestURI(), "/", 3)
    log.Println(tokens)
    // tokens[0] should be an empty string from parsing the initial "/"
    serviceName := tokens[1]
    info, err := registryCache.GetOrRequestService(serviceName)
    if err != nil {
        http.Error(w, "404 Not Found", http.StatusNotFound)
        fmt.Fprintf(w, "%s\n", err)
        log.Printf("ERROR: Registry lookup failed\n%s\n", err)
        return
    }
    // Run request
    resp, err := runRequest(serviceName, info, r)
    if resp != nil {
        defer resp.Body.Close()
    }
    if err != nil {
        log.Println("Request to service returned an error:\n", err)

        // Propagate error code and message to client
        if resp != nil {
            http.Error(w, "Service error: " + resp.Status, resp.StatusCode)
        } else {
            http.Error(w, "Service error", http.StatusBadGateway)
        }
        return
    }

    // Return result
    // This returns errors as well
    // Ideally this would find another instance if there is an error
    // but just keep this behaviour for now
    log.Println("Sending response back to requester")
	// Copy any headers
	for k, v := range resp.Header {
		for _, s := range v {
			w.Header().Add(k, s)
		}
	}
    w.WriteHeader(resp.StatusCode)
    // Copy body
    io.Copy(w, resp.Body)
    return
}

// Custom usage func to support positional arguments (not trivial in golang)
func customUsage() {
    fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
    fmt.Fprintf(flag.CommandLine.Output(),
        "$ %s [OPTIONS ...] PORT [SERVICE] [ADDRESS]\n", os.Args[0])

    fmt.Fprintf(flag.CommandLine.Output(), "\nOPTIONS:\n")
    flag.PrintDefaults()

    fmt.Fprintf(flag.CommandLine.Output(), "\nPOSITIONAL PARAMETERS:\n")
    posArgs := map[string]string {
        "PORT": "Local port for proxy to listen on",
        "SERVICE": "Human-readable name of the service for proxy to represent",
        "ADDRESS": "A string in \"IP:SERVICE_PORT\" format of the address for the proxy to advertise",
    }

    for name, usage := range posArgs {
        // Altered from golang's flag.go's PrintDefaults() implementation
        s := "  " + name
        if len(s) <= 4 {
            s += "\t"
        } else {
            s += "\n    \t"
        }
        s += strings.ReplaceAll(usage, "\n", "\n    \t")
        fmt.Fprint(flag.CommandLine.Output(), s + "\n")
    }

    s := "NOTE: PORT, SERVICE, and ADDRESS *must* come after any OPTIONS flag arguments."
    fmt.Fprint(flag.CommandLine.Output(), "\n" + s + "\n")

    s = "If service and address are omitted, proxy launches in anonymous mode.\n" +
            "Using \"anonymous mode\" allows a client to access the network without\n" +
            "having to register and advertise itself in the network."
    fmt.Fprint(flag.CommandLine.Output(), "\n" + s + "\n")
}

func main() {
    var err error

    // Argument options
    var configPath string
    flag.StringVar(&configPath, "configfile", "../conf/conf.json", "Path to configuration file to use")
    var rcacheTTL int
    flag.IntVar(&rcacheTTL, "rcache-ttl", 3600, "Time-to-live in seconds for registry cache entries")

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
    flag.Usage = customUsage // Do this only afer adding all flags
    flag.Parse() // Parse flag arguments

    // Parse positional arguments
    var mode string
    var port string
    var service string
    var address string
    switch flag.NArg() {
    case 1:
        mode = "anonymous"
        port = flag.Arg(0)
    case 3:
        mode = "service"
        port = flag.Arg(0)
        service = flag.Arg(1)
        address = flag.Arg(2)
    default:
        flag.Usage()
        os.Exit(1)
    }

    priv, err := util.CreateOrLoadKey(keyFlags)
    if err != nil {
        log.Fatalln(err)
    }

    // Read in config file
    config := conf.Config{}
    configFile, err := os.Open(configPath)
    if err != nil {
        log.Fatalf("ERROR: Unable to open configuration file\n%s\n", err)
    }
    configByte, err := ioutil.ReadAll(configFile)
    if err != nil {
        configFile.Close()
        log.Fatalf("ERROR: Unable to read configuration file\n%s\n", err)
    }
    err = json.Unmarshal(configByte, &config)
    if err != nil {
        configFile.Close()
        log.Fatalf("ERROR: Unable to parse configuration file\n%s\n", err)
    }
    configFile.Close()

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

    // If CLI didn't specify a PSK, check the environment variables
    if *psk == nil {
        envPsk, err := util.GetEnvPSK()
        if err != nil {
            log.Fatalln(err)
        }

        *psk = envPsk
    }

    // Set node configuration
    nodeConfig := p2pnode.NewConfig()
    nodeConfig.PrivKey = priv
    nodeConfig.BootstrapPeers = *bootstraps
    nodeConfig.PSK = *psk

    // Setup LCA Manager
    ctx := context.Background()
    if mode == "anonymous" {
        log.Println("Starting LCA Manager in anonymous mode")
        manager, err = lca.NewLCAManager(ctx, nodeConfig, "", "")
    } else {
        log.Println("Starting LCA Manager in service mode with arguments",
                    service, address)
        manager, err = lca.NewLCAManager(ctx, nodeConfig, service, address)
    }

    if err != nil {
        log.Fatalf("ERROR: Unable to create LCA Manager\n%s", err)
    }


    // Setup registry cache
    registryCache = rcache.NewRegistryCache(manager.Host.Ctx, manager.Host.Host,
        manager.Host.RoutingDiscovery, rcacheTTL)

    // Create peer cache instance and start cache update loop
    log.Println("Launching proxy PeerCache instance")
    peerCache = pcache.NewPeerCache(&manager.Host, registryCache)
    go peerCache.UpdateCache()

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    log.Println("Starting HTTP Proxy on 127.0.0.1:" + port)
    http.HandleFunc("/", httpRequestHandler)
    log.Fatal(http.ListenAndServe("127.0.0.1:" + port, nil))
}
