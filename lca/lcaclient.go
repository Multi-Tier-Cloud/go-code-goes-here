package lca

import (
    "bufio"
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/libp2p/go-libp2p-core/network"
)

type LCAClient struct {
    Host           LCAHost
    ServiceName    string
    ServiceAddress string
}

func (lca *LCAClient) FindService(serviceHash string) (string, error) {
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, serviceHash)
    if err != nil {
        panic(err)
    }

    peers := SortPeers(peerChan, lca.Host)

    found := 0
    for _, p := range peers {
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAClientProtocolID)
        if err != nil {
            continue
        } else {
            rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
            str, err := rw.ReadString('\n')
            if err != nil {
                fmt.Println("Error reading from buffer")
                panic(err)
            }
            str = strings.TrimSuffix(str, "\n")

            stream.Close()

            match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
            if err != nil {
                return "", err
            }

            if match {
                return str, nil
            }

            found = 1
            break
        }
    }

    if found != 1 {
        panic("No reachable LCAs exist.")
    }

    return "", ErrUhOh
}

func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    _, err := rw.WriteString(fmt.Sprintf("start program %s\n", serviceHash))
    if err != nil {
        fmt.Println("Error writing to buffer")
        panic(err)
    }
    err = rw.Flush()
    if err != nil {
        fmt.Println("Error flushing buffer")
        panic(err)
    }

    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        panic(err)
    }
    str = strings.TrimSuffix(str, "\n")

    stream.Close()

    match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
    if err != nil {
        return "", err
    }

    if match {
        return str, nil
    }

    return "", ErrUhOh
}

func (lca *LCAClient) AllocService(serviceHash string) (string, error) {
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, LCAServerRendezvous)
    if err != nil {
        panic(err)
    }

    peers := SortPeers(peerChan, lca.Host)

    found := 0
    for _, p := range peers {
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAServerProtocolID)
        if err != nil {
            continue
        } else {
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return result, nil
            found = 1
            break
        }
    }

    if found != 1 {
        panic("No reachable LCAs exist.")
    }

    return "", ErrUhOh
}

// Stub
func pingService() error {
    return nil
}

func LCAClientHandler(stream network.Stream) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    err := pingService()
    if err != nil {
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
   } else {
       rw.WriteString("10.11.17.3:8080\n") // replace with some mechanism
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

func NewLCAClient(ctx context.Context, serviceName string, serviceAddress string) (LCAClient, error) {
    var err error

    var node LCAClient
    node.ServiceName = serviceName
    node.ServiceAddress = serviceAddress

    node.Host, err = New(ctx, nil, LCAClientHandler, LCAClientProtocolID, LCAClientRendezvous)
    if err != nil {
        return node, err
    }

    return node, nil
}
