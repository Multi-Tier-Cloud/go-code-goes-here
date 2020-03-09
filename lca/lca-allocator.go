package lca

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "regexp"
    "strconv"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/util"
)


// Alias for p2pnode.Node for type safety
type LCAAllocator struct {
    Host p2pnode.Node
}

// Temporary function until the real docker alloc gets implemented
func dockerAlloc(serviceHash string) (string, error) {
    attr := os.ProcAttr{}
    process, err := os.StartProcess(
        "../../demos/helloworld/helloworldserver/helloworldserver",
        []string{"helloworldAllocator"},
        &attr,
    )
    if err != nil {
        return "", err
    }
    err = process.Release()
    if err != nil {
        return "", err
    }

	//ipAddr, err := util.GetIPAddress()
	ipAddr, err := util.GetIPAddress()
    if err != nil {
        return "", err
    }
    // port, err := util.GetFreePort()
	port := strconv.Itoa(8080)

    return ipAddr + ":" + port, nil
}

// Allocator Handler that takes care of accepting a connection
// from an LCA Manager and allocating the requested service
func LCAAllocatorHandler(stream network.Stream) {
    fmt.Println("Got new LCA Allocator request")
    // Open communication channels
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

    // Read request
    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        panic(err)
    }
    str = strings.TrimSuffix(str, "\n")

    r := regexp.MustCompile("(.*?)\\s(.*?)$")
    match := r.FindStringSubmatch(str)
    // Respond to command
    switch match[1] {
        case "start-program": {
            result, err := dockerAlloc(match[2])
            if err != nil {
                _, err2 := rw.WriteString("Error: could not start process\n")
                if err2 != nil {
                    fmt.Println("Error writing to buffer")
                    panic(err2)
                }
                panic(err)
            }
            _, err = rw.WriteString(fmt.Sprintf("%s\n", result))
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
        }
        default: {
            _, err = rw.WriteString("Error: unrecognized command\n")
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
       }
    }

    // Clean up
    fmt.Println("Closing stream")
    stream.Close()
}

// Constructor for LCA Allocator
func NewLCAAllocator(ctx context.Context) (LCAAllocator, error) {
    var err error
    var node LCAAllocator

    config := p2pnode.NewConfig()
    config.StreamHandlers = []network.StreamHandler{LCAAllocatorHandler}
    config.HandlerProtocolIDs = []protocol.ID{LCAAllocatorProtocolID}
    config.Rendezvous = []string{LCAAllocatorRendezvous}
    node.Host, err = p2pnode.NewNode(ctx, config)
    if err != nil {
        return node, err
    }

    return node, nil
}
