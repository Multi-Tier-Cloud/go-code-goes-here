package lca

import (
    "bufio"
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "regexp"
    "strconv"
    "strings"
    "sync"

    "github.com/libp2p/go-libp2p-core/network"

    "github.com/multiformats/go-multiaddr"

    "github.com/PhysarumSM/common/p2pnode"
    "github.com/PhysarumSM/common/util"
    "github.com/PhysarumSM/docker-driver/docker_driver"
)

// Alias for p2pnode.Node for type safety
type LCAAllocator struct {
    Host p2pnode.Node
    // Use a map for fast insert/delete
    // and ease of passing into functions
    services map[string]string
    // lock for services map
    servicesMutex sync.Mutex
}

func cmdStartProgram(bootstraps []multiaddr.Multiaddr, sPsk string, imageName string,
         services map[string]string, servicesMutex *sync.Mutex,
         rw *bufio.ReadWriter) string {
    _, err := docker_driver.PullImage(imageName)
    if err != nil {
        log.Println("Error calling Docker PullImage()\n", err)
        return LCAPErrAllocFail
    }
    ipAddress, err := util.GetIPAddress()
    if err != nil {
        log.Println("Error getting IP address\n", err)
        return LCAPErrAllocFail
    }
    pp, err := util.GetFreePort()
    if err != nil {
        log.Println("Error getting free port for proxy\n", err)
        return LCAPErrAllocFail
    }
    sp, err := util.GetFreePort()
    if err != nil {
        log.Println("Error getting free port for service\n", err)
        return LCAPErrAllocFail
    }
    mp, err := util.GetFreePort()
    if err != nil {
        log.Println("Error getting free port for service\n", err)
        return LCAPErrAllocFail
    }
    proxyPort := strconv.Itoa(pp)
    servicePort := strconv.Itoa(sp)
    metricsPort := strconv.Itoa(mp)
    strBootstraps := []string{}
    for _, addr := range bootstraps {
        strBootstraps = append(strBootstraps, addr.String())
    }

    cfg := docker_driver.DockerConfig{
        Image: imageName,
        Network: "host",
        Env: []string{
            "PROXY_IP=" + ipAddress,
            "PROXY_PORT=" + proxyPort,
            "SERVICE_PORT=" + servicePort,
            "METRICS_PORT=" + metricsPort,
            "P2P_BOOTSTRAPS=" + strings.Join(strBootstraps, " "),
            "P2P_PSK=" + sPsk,
        },
    }
    cid, err := docker_driver.RunContainer(cfg)
    if err != nil {
        log.Println("Error calling Docker RunContainer()\n", err)
        return LCAPErrAllocFail
    }
    err = write(rw, fmt.Sprintf("%s\n", ipAddress + ":" + servicePort))
    if err != nil {
        log.Println("Error writing to buffer\n", err)
        return LCAPErrAllocFail
    }

    servicesMutex.Lock()
    services[metricsPort] = cid
    servicesMutex.Unlock()

    log.Println("Started new service", imageName, "with metric at", metricsPort)

    return ""
}

// Generator function for LCA handler function
// Used to allow the handler to remember the bootstraps and PSK
func NewLCAHandler(bootstraps []multiaddr.Multiaddr, sPsk string,
         services map[string]string,
         servicesMutex *sync.Mutex) func(network.Stream) {

    // The handler function takes care of accepting requests
    // from an LCA Manager and allocating the requested service
    return func (stream network.Stream) {
        defer stream.Close()
        log.Println("Got new LCA Manager request")
        // Open communication channels
        rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

        // Read request
        str, err := read(rw)
        if err != nil {
            log.Println("Error reading from buffer\n", err)
            return
        }

        r := regexp.MustCompile("(.*?)\\s(.*?)$")
        match := r.FindStringSubmatch(str)
        // Respond to command
        switch match[1] {
            case LCAAPCmdStartProgram: {
                imageName := match[2]
                log.Println("Received command", match[1], "starting image:", match[2])
                result := cmdStartProgram(bootstraps, sPsk,
                          imageName, services, servicesMutex, rw)
                if result != "" {
                    err2 := write(rw, result)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                }
            }
            default: {
                err = write(rw, LCAPErrUnrecognized)
                if err != nil {
                    log.Println("Error writing to buffer\n", err)
                }
            }
        }

        // Clean up
        log.Println("Closing stream")
    }
}

// Constructor for LCA Allocator
// Input Params:
//   ctx: Context to pass to the new P2P node
//   cfg: Configuration settings for the new P2P node
//   sPsk: Un-hashed PSK passphrase to pass to spawned proxies
func NewLCAAllocator(ctx context.Context,
        cfg p2pnode.Config, sPsk string) (LCAAllocator, error) {

    var err error
    var node LCAAllocator

    cfg.Rendezvous = append(cfg.Rendezvous, LCAAllocatorRendezvous)
    node.Host, err = p2pnode.NewNode(ctx, cfg)
    if err != nil {
        return node, err
    }

    // Find public-facing listening multiaddr for this node and
    // pass it to the allocation handler generator
    multiaddrs, err := util.Whoami(node.Host.Host)
    if err != nil {
        log.Printf("ERROR: Unable to get addresses for node\n")
        return node, err
    }
    pubAddr, err := util.GetIPAddress()
    if err != nil {
        // TODO: Should we allow cases where nodes are running in completely
        //       private networks w/ no access to the Internet??
        log.Printf("ERROR: Unable to get public address\n")
        return node, err
    }

    node.services = make(map[string]string)
    var allocHandler func(network.Stream)
    for _, addr := range multiaddrs {
        if strings.Contains(addr.String(), pubAddr) {
            cfg.BootstrapPeers = append(cfg.BootstrapPeers, addr)
            allocHandler = NewLCAHandler(cfg.BootstrapPeers, sPsk,
                               node.services, &node.servicesMutex)
            break
        }
    }

    if allocHandler == nil {
        log.Printf("ERROR: Unable to find a listening multiaddr for this node\n")
        return node, err
    }

    node.Host.Host.SetStreamHandler(LCAAllocatorProtocolID, allocHandler)

    return node, nil
}

type Service struct {
    MetricsPort string
    Cid string
}

func (lca *LCAAllocator) CullUnusedServices() {
    var servicesToCull []Service
    lca.servicesMutex.Lock()
    defer lca.servicesMutex.Unlock()
    for metricsPort, cid := range lca.services {
        resp, err := http.Get("http://127.0.0.1:" + metricsPort)
        if resp != nil {
            defer resp.Body.Close()
        }
        if err != nil {
            log.Printf("Adding %s to cull list because resp error\n", cid)
            servicesToCull = append(servicesToCull, Service{metricsPort, cid})
            continue
        }
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Printf("Adding %s to cull list because read error\n", cid)
            servicesToCull = append(servicesToCull, Service{metricsPort, cid})
            continue
        }
        tslsrString := strings.TrimSpace(string(body))
        tslsr, err := strconv.ParseInt(tslsrString, 10, 64)
        if err != nil {
            log.Printf("Adding %s to cull list because conv error\n", cid)
            servicesToCull = append(servicesToCull, Service{metricsPort, cid})
            continue
        }
        if (tslsr > 60) {
            log.Printf("Adding %s to cull list because over time\n", cid)
            log.Printf("Got %d while limit is %d\n", tslsr, 60)
            servicesToCull = append(servicesToCull, Service{metricsPort, cid})
            continue
        }
    }
    for _, service := range servicesToCull {
        log.Printf("Culling service with metrics port %s and cid %s\n",
            service.MetricsPort, service.Cid)
        delete(lca.services, service.MetricsPort)
        docker_driver.StopContainer(service.Cid)
        docker_driver.DeleteContainer(service.Cid)
    }
}
