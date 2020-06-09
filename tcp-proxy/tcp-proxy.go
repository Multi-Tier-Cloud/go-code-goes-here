package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "io"
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

// Global LCA Manager instance to handle peer search and allocation
var manager lca.LCAManager

// Global Peer Cache instance to cache connected peers
var cache pcache.PeerCache

// Listens to the listening socket and redirects to remote addr
type Forwarder struct {
    // Use TCPAddr instead for endpoint addresses?
    ListenAddr  string
    fWorker     func(net.Listener, string)
}

// Maps a remote addr to existing ServiceProxy for that addr
var serv2Fwd = make(map[string]Forwarder)

// Data forwarder
// Simplified version of pipe() from https://github.com/jpillora/go-tcp-proxy/blob/master/proxy.go
func fwdData(src, dst net.Conn) {
    buf := make([]byte, 0xffff) // 64k buffer
    for {
        nBytes, err := src.Read(buf)
        if err != nil {
            if err != io.EOF {
                log.Printf("ERROR: Unable to read from connection %s\n", src)
            }
            return
        }
        data := buf[:nBytes]

        nBytes, err = dst.Write(data)
        if err != nil {
            if err != io.EOF {
                fmt.Printf("ERROR: Unable to write to connection %s\n", dst)
            }
            return
        }
    }
}

// Connection forwarder
func fwdLocalConn(lConn net.Conn, targetAddr string) {
    defer lConn.Close()
    log.Printf("Accepted conn: %s\n", lConn)

    // Resolve and open connection to destination service
    rAddr, err := net.ResolveTCPAddr("tcp", targetAddr)
    if err != nil {
        log.Printf("ERROR: Unable to resolve target address %s\n", targetAddr)
        return
    }

    rConn, err := net.DialTCP("tcp", nil, rAddr)
    if err != nil {
        log.Printf("ERROR: Unable to dial target address %s\n", targetAddr)
        return
    }
    defer rConn.Close()

    // Forward data in each direction
    go fwdData(lConn, rConn)
    fwdData(rConn, lConn)
}

// Implementation of ServiceProxy
func serviceProxy(listen net.Listener, targetAddr string) {
    defer listen.Close()

    for {
        conn, err := listen.Accept()
        if err != nil {
            log.Printf("Listener (%s) unable to accept connection\n%v\n", listen.Addr(), err)
            continue
        }

        // Forward connection
        go fwdLocalConn(conn, targetAddr)
    }
}


// Handles the "proxying" part of proxy
// TODO: Refactor this function
func requestHandler(w http.ResponseWriter, r *http.Request) {
    log.Println("Got request:", r.URL.RequestURI()[1:])

    // 1. Find service information and arguments from URL
    // URL.RequestURI() includes path?query (URL.Path only has the path)
    tokens := strings.SplitN(r.URL.RequestURI(), "/", 3)
    log.Println(tokens)
    // tokens[0] should be an empty string from parsing the initial "/"
    serviceName := tokens[1]
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

    var arguments []string
    // check if arguments exist
    if len(tokens) == 3 {
        arguments = tokens[2:]
        fmt.Printf("parsed arguments are: %s\n", arguments)
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

    // Open TCP tunnel to service and open local TCP listening port
    // NOTE: Currently doesn't support SSL/TLS
    //
    // TODO: In the future, perhaps the connection type info (TCP/UDP) can be stored in hash-lookup?
    // TODO: In the future, move towards proxy-to-proxy implementation
    //       This may be useful? https://github.com/libp2p/go-libp2p-gostream
    // Start separate goroutine for handling connections and proxying to service
    var listenAddr string
    if _, exists := serv2Fwd[serviceAddress]; !exists {
        listen, err := net.Listen("tcp", "127.0.0.1:") // automatically choose port
        if err != nil {
            http.Error(w, "Unable to open listening port", http.StatusInternalServerError)
            return
        }

        listenAddr = listen.Addr().String()
        serv2Fwd[serviceAddress] = Forwarder {
            ListenAddr: listenAddr,
            fWorker: serviceProxy,
        }
        go serv2Fwd[serviceAddress].fWorker(listen, serviceAddress)
    } else {
        listenAddr = serv2Fwd[serviceAddress].ListenAddr
    }


    //request := fmt.Sprintf("http://%s/%s", serviceAddress, arguments)
    //log.Println("Running request:", request)
    //resp, err := http.Get(request)
    //if err != nil {
    //    log.Println("Request to service returned an error:\n", err)

    //    // Propagate error code and message to client
    //    if resp != nil {
    //        http.Error(w, "Service error: " + resp.Status, resp.StatusCode)
    //    } else {
    //        http.Error(w, "Service error", http.StatusBadGateway)
    //    }
    //    go cache.RemovePeer(id, serviceAddress)
    //    return
    //}
    //defer resp.Body.Close()

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
    log.Println("Starting HTTP Proxy on 127.0.0.1:" + port)
    http.HandleFunc("/", requestHandler)
    log.Fatal(http.ListenAndServe("127.0.0.1:" + port, nil))
}
