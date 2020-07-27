package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "log"
    "net"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/pnet"
    "github.com/libp2p/go-libp2p-core/protocol"

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

// TODO: Clean this shit up; decrease number of package-scoped variables.
//       Terrible style and makes things harder to debug / maintain.
//       Perhaps wrap all proxy-related variables into a ProxyMetadata struct?
//       Then that can be passed around easily.

// HTTP listening endpoint for control messages
// TODO: Make this configurable? i.e. proxy available to non-localhost?
var ctrlHost = "127.0.0.1" // Port will be passed via CLI

// Human-readable name for the service this proxy represents
var service string

// Endpoint (IP:PORT) of service this proxy represents, when in service mode
var servEndpoint string

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var cache pcache.PeerCache

// Global Registry Cache instance to cache service registry info
var registryCache *rcache.RegistryCache

// Listens to the listening socket and redirects to remote addr
type Forwarder struct {
    // Use TCPAddr instead for endpoint addresses?
    ListenAddr  string
    tcpWorker   func(net.Listener, []string)
    udpWorker   func(*net.UDPConn, []string)
}

// Maps a remote addr to existing Forwarder for that addr
var serv2Fwd = make(map[string]Forwarder)

func findOrAllocate(serviceHash, dockerHash string) (peer.ID, error) {
    var err error
    var id peer.ID
    var serviceAddress string
    var perf p2putil.PerfInd

    // 2. Search for cached instances
    id, serviceAddress, err = cache.GetPeer(serviceHash)
    log.Printf("Get peer returned id %s, serviceAddr %s, and err %v\n", id, serviceAddress, err)
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
            //       Need to obtain perf req from hash-lookup service
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
            } else if p2putil.PerfIndCompare(cache.ReqPerf.SoftReq, perf) {
                log.Printf("Found service's performance (%s) does not meet requirements (%s)\n", perf, cache.ReqPerf.SoftReq)
                log.Println("Creating new service instance")
                id, serviceAddress, _, err = manager.AllocBetterService(dockerHash, perf)
                if err != nil {
                    log.Println("No services able to be created, using previously found peer")
                }
            }
        }

        if err == nil && serviceAddress != "" {
            // Cache peer information and loop again
            cache.AddPeer(pcache.PeerRequest{ID: id, Hash: serviceHash, Address: serviceAddress})

            elapsedTime := time.Now().Sub(startTime)
            log.Println("Find/alloc service took:", elapsedTime)
        }
    }

    if serviceAddress == "" || id == peer.ID("") {
        return peer.ID(""), fmt.Errorf("Unable to find or allocate service\n")
    }

    return id, nil
}

// Returns the components of the URI, excluding the first and last '/'
func splitURIPath(uriPath string) []string {
    uriPath = strings.TrimPrefix(uriPath, "/")
    uriPath = strings.TrimSuffix(uriPath, "/")
    res := strings.Split(uriPath, "/")
    if res[0] == "" {
        // Case where URI did not contain any components
        // Split() normally returns min slice of length 1 if the
        // separator wasn't found. Instead, return an empty slice.
        return []string{}
    }

    return res
}

func createStream(targetPeer peer.ID, proto protocol.ID) (network.Stream, error) {
    p2pNode := manager.Host
    stream, err := p2pNode.Host.NewStream(p2pNode.Ctx, targetPeer, proto)
    if err != nil {
        return nil, err
    }

    return stream, nil
}

// resolveService() performs the service name to hash lookup, and then finds
// an appropriate peer that provides that service, allocating a new instance
// if necessary.
// Returns the peer's ID, the service's info, and any errors
func resolveService(servName string) (peer.ID, registry.ServiceInfo, error) {
    info, err := registryCache.GetOrRequestService(servName)
    if err != nil {
        return "", info, fmt.Errorf("ERROR: Hash lookup for service %s failed\n%s\n",
                                    servName, err)
    }

    peerProxyID, err := findOrAllocate(info.ContentHash, info.DockerHash)
    if err != nil {
        return "", info, fmt.Errorf("ERROR: Unable to find or allocate service %s (%s)\n%v\n",
                                    servName, info.ContentHash, err)
    }

    return peerProxyID, info, nil
}

// Handles the setting up proxies to services
func requestHandler(w http.ResponseWriter, r *http.Request) {
    var err error

    log.Println("Got request:", r.URL.RequestURI())

    // 1. Find service information and arguments from URL
    // URL.RequestURI() includes path?query (URL.Path only has the path)
    if r.URL.RawQuery != "" {
        http.Error(w, "Bad Request", http.StatusBadRequest)
        return
    }
    chainSpec := splitURIPath(r.URL.Path)

    if len(chainSpec) < 2 {
        http.Error(w, "Bad Request", http.StatusBadRequest)
        // TODO: Update this error message with chain specs
        fmt.Fprintf(w, "Error: Incorrect API usage, expecting a protocol " +
            "('tcp' or 'udp') and a service name.\n" +
            "e.g. http://%s/tcp/name-of-service\n", ctrlHost)
        log.Printf("ERROR: Incorrect number of arguments in HTTP URI\n")
        return
    }

    // Since we're in the HTTP handler, we know the chain has not been set up
    tpProto := chainSpec[0]
    switch tpProto {
    case "udp", "tcp": // do nothing
    default:
        http.Error(w, "Bad Request", http.StatusBadRequest)
        fmt.Fprintf(w, "Error: Expecting protocol to be either 'tcp' or 'udp'\n")
        return
    }

    // Open TCP/UDP tunnel to service and open local TCP listening port
    // Start separate goroutine for handling connections and proxying to service
    var listenAddr string
    if tpProto == "tcp" {
        listenAddr, err = openTCPProxy(chainSpec)
    } else if tpProto == "udp" {
        listenAddr, err = openUDPProxy(chainSpec)
    } else {
        // Should not get here
        http.Error(w, "Unknown transport protocol", http.StatusInternalServerError)
        log.Printf("ERROR: Should not get here; Unknown transport protocol: %s\n", tpProto)
        return
    }
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        log.Printf("ERROR: %s\n", err.Error())
        return
    }

    // Return result
    // This returns errors as well
    // Ideally this would find another instance if there is an error
    // but just keep this behaviour for now
    log.Println("Sending response back to requester")
    fmt.Fprintf(w, "%s\n", listenAddr)
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
    switch flag.NArg() {
    case 1:
        mode = "anonymous"
        port = flag.Arg(0)
    case 3:
        mode = "service"
        port = flag.Arg(0)
        service = flag.Arg(1)
        servEndpoint = flag.Arg(2)
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

    // Set up chain setup handler
    nodeConfig.HandlerProtocolIDs = append(nodeConfig.HandlerProtocolIDs, chainSetupProtoID)
    nodeConfig.StreamHandlers = append(nodeConfig.StreamHandlers, chainSetupHandler)

    // Setup LCA Manager
    ctx := context.Background()
    if mode == "anonymous" {
        log.Println("Starting LCA Manager in anonymous mode")
        manager, err = lca.NewLCAManager(ctx, nodeConfig, "", "")
    } else {
        log.Println("Starting LCA Manager in service mode with arguments",
                    service, servEndpoint)
        manager, err = lca.NewLCAManager(ctx, nodeConfig, service, servEndpoint)
    }

    if err != nil {
        log.Fatalf("ERROR: Unable to create LCA Manager\n%s", err)
    }

    // Setup peer cache
    config.Perf.SoftReq.RTT = config.Perf.SoftReq.RTT * time.Millisecond
    config.Perf.HardReq.RTT = config.Perf.HardReq.RTT * time.Millisecond
    log.Println("Launching proxy PeerCache instance")
    log.Println("Setting performance requirements based on perf.conf",
        "soft limit:", config.Perf.SoftReq.RTT, "hard limit:", config.Perf.HardReq.RTT)
    // Create cache instance
    cache = pcache.NewPeerCache(config.Perf, &manager.Host)
    // Boot up cache managment function
    go cache.UpdateCache()

    // Setup registry cache
    registryCache = rcache.NewRegistryCache(manager.Host.Ctx, manager.Host.Host,
        manager.Host.RoutingDiscovery, rcacheTTL)

    // Setup HTTP control service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    log.Println("Starting HTTP control endpoint on:", ctrlHost + ":" + port)
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe(ctrlHost + ":" + port, nil))
}
