package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strconv"
    "strings"
    "log"

    "github.com/libp2p/go-libp2p-core/network"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/util"
    "github.com/Multi-Tier-Cloud/docker-driver/docker_driver"
)

// Alias for p2pnode.Node for type safety
type LCAAllocator struct {
    Host p2pnode.Node
}

// Generator function for LCA handler function
// Used to allow the handler to remember the bootstraps and PSK
func NewLCAHandler(bootstraps []multiaddr.Multiaddr,
                    sPsk string) func(network.Stream) {

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
                _, err = docker_driver.PullImage(imageName)
                if err != nil {
                    log.Println("Error calling Docker PullImage()\n", err)
                    err2 := write(rw, LCAPErrAllocFail)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                    return
                }
                ipAddress, err := util.GetIPAddress()
                if err != nil {
                    log.Println("Error getting IP address\n", err)
                    err2 := write(rw, LCAPErrAllocFail)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                    return
                }
                pp, err := util.GetFreePort()
                if err != nil {
                    log.Println("Error getting free port for proxy\n", err)
                    err2 := write(rw, LCAPErrAllocFail)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                    return
                }
                sp, err := util.GetFreePort()
                if err != nil {
                    log.Println("Error getting free port for service\n", err)
                    err2 := write(rw, LCAPErrAllocFail)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                    return
                }
                proxyPort := strconv.Itoa(pp)
                servicePort := strconv.Itoa(sp)
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
                        "P2P_BOOTSTRAPS=" + strings.Join(strBootstraps, " "),
                        "P2P_PSK=" + sPsk,
                    },
                }
                _, err = docker_driver.RunContainer(cfg)
                if err != nil {
                    log.Println("Error calling Docker RunContainer()\n", err)
                    err2 := write(rw, LCAPErrAllocFail)
                    if err2 != nil {
                        log.Println("Error writing to buffer\n", err2)
                    }
                    return
                }
                err = write(rw, fmt.Sprintf("%s\n", ipAddress + ":" + servicePort))
                if err != nil {
                    log.Println("Error writing to buffer\n", err)
                    return
                }
            }
            default: {
                err = write(rw, LCAPErrUnrecognized)
                if err != nil {
                    log.Println("Error writing to buffer\n", err)
                    return
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

    var allocHandler func(network.Stream)
    for _, addr := range multiaddrs {
        if strings.Contains(addr.String(), pubAddr) {
            cfg.BootstrapPeers = append(cfg.BootstrapPeers, addr)
            allocHandler = NewLCAHandler(cfg.BootstrapPeers, sPsk)
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
