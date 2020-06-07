package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strconv"
    "log"

    "github.com/libp2p/go-libp2p-core/network"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/util"
    "github.com/Multi-Tier-Cloud/docker-driver/docker_driver"
)

// Alias for p2pnode.Node for type safety
type LCAAllocator struct {
    Host p2pnode.Node
}

// Allocator Handler that takes care of accepting a connection
// from an LCA Manager and allocating the requested service
func LCAAllocatorHandler(stream network.Stream) {
    defer stream.Reset()
    log.Println("Got new LCA Allocator request")
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
            cfg := docker_driver.DockerConfig{
                Image: imageName,
                Network: "host",
                Env: []string{
                    "PROXY_IP=" + ipAddress,
                    "PROXY_PORT=" + proxyPort,
                    "SERVICE_PORT=" + servicePort,
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

// Constructor for LCA Allocator
func NewLCAAllocator(ctx context.Context, cfg p2pnode.Config) (LCAAllocator, error) {
    var err error
    var node LCAAllocator

    cfg.StreamHandlers = append(cfg.StreamHandlers, LCAAllocatorHandler)
    cfg.HandlerProtocolIDs = append(cfg.HandlerProtocolIDs, LCAAllocatorProtocolID)
    cfg.Rendezvous = append(cfg.Rendezvous, LCAAllocatorRendezvous)
    node.Host, err = p2pnode.NewNode(ctx, cfg)
    if err != nil {
        return node, err
    }

    return node, nil
}
