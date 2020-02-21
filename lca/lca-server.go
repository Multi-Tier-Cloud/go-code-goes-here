package lca

import (
    "bufio"
    "context"
    "fmt"
	"net"
    "os"
    "regexp"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
)


type LCAServer struct {
    Host p2pnode.Node
}

// Get preferred outbound ip of this machine
func GetIPAddress() (string, error) {
    conn, err := net.Dial("udp", "8.8.8.8:80")
    if err != nil {
        return "", err
    }
    defer conn.Close()

    localAddr := conn.LocalAddr().(*net.UDPAddr)

    return localAddr.IP.String(), nil
}

// Stub
func dockerAlloc(serviceHash string) (string, error) {
    attr := os.ProcAttr{}
    process, err := os.StartProcess(
        "../../demos/helloworld/helloworldserver/helloworldserver",
        []string{"helloworldserver"},
        &attr,
    )
    if err != nil {
        return "", err
    }
    err = process.Release()
    if err != nil {
        return "", err
    }

	ipAddr, err := GetIPAddress()
    if err != nil {
        return "", err
    }
	port := "8080"

    return ipAddr + ":" + port, nil
}

func LCAServerHandler(stream network.Stream) {
    fmt.Println("Got new LCA Server request")
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        panic(err)
    }
    str = strings.TrimSuffix(str, "\n")

    r := regexp.MustCompile("(.*?)\\s(.*?)$")
    match := r.FindStringSubmatch(str)
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

    fmt.Println("Closing stream")
    stream.Close()
}

func NewLCAServer(ctx context.Context) (LCAServer, error) {
    var err error
    var node LCAServer

    config := p2pnode.NewConfig()
    config.StreamHandler = LCAServerHandler
    config.HandlerProtocolID = LCAServerProtocolID
    config.Rendezvous = LCAServerRendezvous
    node.Host, err = p2pnode.NewNode(ctx, config)
    if err != nil {
        return node, err
    }

    return node, nil
}
