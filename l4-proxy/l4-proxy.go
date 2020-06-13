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

    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/pnet"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
    "github.com/Multi-Tier-Cloud/common/util"

    "github.com/Multi-Tier-Cloud/hash-lookup/hashlookup"

    "github.com/Multi-Tier-Cloud/service-manager/conf"
    "github.com/Multi-Tier-Cloud/service-manager/lca"
    "github.com/Multi-Tier-Cloud/service-manager/pcache"
)

const defaultKeyFile = "~/.privKeyProxy"

func init() {
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

// HTTP listening endpoint for control messages
// TODO: Make this configurable? i.e. proxy available to non-localhost?
var ctrlHost = "127.0.0.1" // Port will be passed via CLI

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var cache pcache.PeerCache

// Listens to the listening socket and redirects to remote addr
type Forwarder struct {
    // Use TCPAddr instead for endpoint addresses?
    ListenAddr  string
    tcpWorker   func(net.Listener, string)
    udpWorker   func(*net.UDPConn, string)
}

// Maps a remote addr to existing ServiceProxy for that addr
var serv2Fwd = make(map[string]Forwarder)

// Handles the "proxying" part of proxy
// TODO: Refactor this function
func requestHandler(w http.ResponseWriter, r *http.Request) {
    log.Println("Got request:", r.URL.RequestURI()[1:])

    // 1. Find service information and arguments from URL
    // URL.RequestURI() includes path?query (URL.Path only has the path)
    // tokens[0] should be an empty string from parsing the initial "/"
    tokens := strings.Split(r.URL.RequestURI(), "/")

    if len(tokens) < 3 {
        http.Error(w, "Bad Request", http.StatusBadRequest)
        fmt.Fprintf(w, "Error: Incorrect API usage, expecting a protocol " +
            "('tcp' or 'udp') and a service name.\n" +
            "e.g. http://%s/tcp/name-of-service\n", ctrlHost)
        log.Printf("ERROR: Incorrect number of arguments in HTTP URI\n")
        return
    }

    tpProto := tokens[1]
    switch tpProto {
    case "udp", "tcp": // do nothing
    default:
        http.Error(w, "Bad Request", http.StatusBadRequest)
        fmt.Fprintf(w, "Error: Expecting protocol to be either 'tcp' or 'udp'\n")
        return
    }

    serviceName := tokens[2]
    log.Printf("Requested protocol is: %s\n", tpProto)
    log.Printf("Requested service is: %s\n", serviceName)

    // check if arguments exist
    var arguments []string
    if len(tokens) > 3 {
        arguments = tokens[3:]
        log.Printf("Parsed arguments are: %s\n", arguments)
    }

    log.Println("Looking for service with name", serviceName, "in hash-lookup")
    serviceHash, dockerHash, err := hashlookup.GetHashWithHostRouting(
        manager.Host.Ctx, manager.Host.Host,
        manager.Host.RoutingDiscovery, serviceName,
    )
    if err != nil {
        http.Error(w, "404 Not Found", http.StatusNotFound)
        fmt.Fprintf(w, "%s\n", err)
        log.Printf("ERROR: Hash lookup failed\n%s\n", err)
        return
    }
    var id peer.ID
    var serviceAddress string

    // 2. Search for cached instances
    // Search for an instance in the cache, up to 3 attempts
    for attempts := 0; attempts < 3 && serviceAddress == ""; attempts++ {
        // Backoff before searching again (the 5s is arbitrary at this point)
        // TODO: Remove hard-coding
        if attempts > 0 {
            time.Sleep(5 * time.Second)
        }

        id, serviceAddress, err = cache.GetPeer(serviceHash)
        log.Printf("Get peer returned id %s, serviceAddr %s, and err %v\n", id, serviceAddress, err)
        if err != nil {
            // If does not exist, use libp2p connection to find/create service
            log.Println("Finding best existing service instance")

            startTime := time.Now()

            id, serviceAddress, perf, err := manager.FindService(serviceHash)
            if err != nil {
                log.Println("Could not find, creating new service instance")
                id, serviceAddress, perf, err = manager.AllocService(dockerHash)
                if err != nil {
                    log.Println("No services able to be found or created\n", err)
                    continue
                }
            } else if p2putil.PerfIndCompare(cache.ReqPerf.SoftReq, perf) {
                log.Println("Found service does not meet requirements, creating new service instance")
                id2, serviceAddress2, _, err := manager.AllocBetterService(dockerHash, perf)
                if err != nil {
                    log.Println("No services able to be created, using previously found peer")
                } else {
                    id, serviceAddress = id2, serviceAddress2
                }
            }

            elapsedTime := time.Now().Sub(startTime)
            log.Println("Find/alloc service took:", elapsedTime)

            // Cache peer information and loop again
            cache.AddPeer(pcache.PeerRequest{ID: id, Hash: serviceHash, Address: serviceAddress})
            continue
        }
    }

    if serviceAddress == "" || id == peer.ID("") {
        http.NotFound(w, r)
        return
    }

    // Open TCP/UDP tunnel to service and open local TCP listening port
    // Start separate goroutine for handling connections and proxying to service
    var listenAddr string
    if tpProto == "tcp" {
        listenAddr, err = openTCPProxy(serviceAddress)
    } else if tpProto == "udp" {
        listenAddr, err = openUDPProxy(serviceAddress)
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

    // Setup cache
    config.Perf.SoftReq.RTT = config.Perf.SoftReq.RTT * time.Millisecond
    config.Perf.HardReq.RTT = config.Perf.HardReq.RTT * time.Millisecond
    log.Println("Launching proxy PeerCache instance")
    log.Println("Setting performance requirements based on perf.conf",
        "soft limit:", config.Perf.SoftReq.RTT, "hard limit:", config.Perf.HardReq.RTT)
    // Create cache instance
    cache = pcache.NewPeerCache(config.Perf, &manager.Host)
    // Boot up cache managment function
    go cache.UpdateCache()

    // Setup HTTP proxy service
    // This port number must be fixed in order for the proxy to be portable
    // Docker must route this port to an available one externally
    log.Println("Starting HTTP control endpoint on:", ctrlHost + ":" + port)
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe(ctrlHost + ":" + port, nil))
}
