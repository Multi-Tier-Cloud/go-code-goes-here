package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"
)


type LCAServer struct {
    Host LCAHost
}

// Stub
func dockerAlloc(serviceHash string) (string, error) {
    return "10.11.17.3:8080\n", nil
}

func respondToAlloc(stream network.Stream) {
    logger.Info("Got a new request")

    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        panic(err)
    }
    str = strings.TrimSuffix(str, "\n")
    logger.Info("Got string:", str)

    r := regexp.MustCompile("(.*?)\\s(.*?)$")
    match := r.FindStringSubmatch(str)
    switch match[1] {
        case "start-program": {
            result, err := dockerAlloc(match[2])
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            rw.WriteString(result)
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
            logger.Info("Sent microservice %s", strings.TrimSuffix(result, "\n"))
        }
        default: {
            rw.WriteString("Error\n")
            if err != nil {
                fmt.Println("Error writing to buffer")
                panic(err)
            }
            err = rw.Flush()
            if err != nil {
                fmt.Println("Error flushing buffer")
                panic(err)
            }
            logger.Info("Requested microservice invalid")
       }
    }

    logger.Info("Closing stream")
    stream.Close()
}

func LCAServerHandler(stream network.Stream) {
    respondToServiceRequest(stream)
}

func NewLCAServer(ctx context.Context) LCAServer, err {
    var err error

    var node LCAServer

    node.Host, err = New(ctx, nil, LCAServerHandler, LCAServerProtocolID, LCAServerRendezvous)
    if err != nil {
        return nil, err
    }

    return node, nil
}
