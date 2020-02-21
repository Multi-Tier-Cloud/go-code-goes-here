package lca

import (
    "bufio"
    "context"
    "fmt"
	"net"
    "os"
    "regexp"
    "strconv"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    //"github.com/Multi-Tier-Cloud/common/util"
)


type LCAAgent struct {
    Host p2pnode.Node
}

func getIPAddress() (string, error) {
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
        "../../demos/helloworld/helloworldAgent/helloworldAgent",
        []string{"helloworldAgent"},
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
	ipAddr, err := getIPAddress()
    if err != nil {
        return "", err
    }
    // port, err := util.GetFreePort()
	port := strconv.Itoa(8080)

    return ipAddr + ":" + port, nil
}

func LCAAgentHandler(stream network.Stream) {
    fmt.Println("Got new LCA Agent request")
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

func NewLCAAgent(ctx context.Context) (LCAAgent, error) {
    var err error
    var node LCAAgent

    config := p2pnode.NewConfig()
    config.StreamHandlers = []network.StreamHandler{LCAAgentHandler}
    config.HandlerProtocolIDs = []protocol.ID{LCAAgentProtocolID}
    config.Rendezvous = []string{LCAAgentRendezvous}
    node.Host, err = p2pnode.NewNode(ctx, config)
    if err != nil {
        return node, err
    }

    return node, nil
}
