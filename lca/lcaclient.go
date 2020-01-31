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
}

func (lca *LCAClient) FindService(serviceHash string) (string, error) {
    fmt.Println("Finding providers for:", serviceHash)
    peerChan, err := lca.Host.RoutingDiscovery.FindPeers(lca.Host.Ctx, serviceHash)
    if err != nil {
        panic(err)
    }

    peers := SortPeers(peerChan, lca.Host)

    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAClientProtocolID)
        if err != nil {
            continue
        } else {
            rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
            str, err := rw.ReadString('\n')
            if err != nil {
                fmt.Println("Error reading from buffer")
                return "", err
            }
            str = strings.TrimSuffix(str, "\n")

            stream.Close()

            fmt.Println("Got response from peer:", str)
            //match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
            //if err != nil {
            //    fmt.Println("unsuccessful")
            //    return "", err
            //}

            //if match {
            //    fmt.Println("successful")
            //    return str, nil
            //}
            return str, nil
        }
    }

    return "", ErrUhOh
}

func requestAlloc(stream network.Stream, serviceHash string) (string, error) {
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    _, err := rw.WriteString(fmt.Sprintf("start-program %s\n", serviceHash))
    if err != nil {
        fmt.Println("Error writing to buffer")
        return "", err
    }
    err = rw.Flush()
    if err != nil {
        fmt.Println("Error flushing buffer")
        return "", err
    }

    str, err := rw.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading from buffer")
        return "", err
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
        return "", err
    }

    peers := SortPeers(peerChan, lca.Host)

    for _, p := range peers {
        fmt.Println("Attempting to contact peer with pid:", p.ID)
        stream, err := lca.Host.Host.NewStream(lca.Host.Ctx, p.ID, LCAServerProtocolID)
        if err != nil {
            continue
        } else {
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                continue
            }

            return result, nil
        }
    }

    return "", ErrUhOh
}

// Stub
func pingService() error {
    return nil
}

func NewLCAClientHandler(address string) func(network.Stream) {
    return func(stream network.Stream) {
        fmt.Println("Got a new LCA Client request")
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
            rw.WriteString(fmt.Sprintf("%s\n", address)) // replace with some mechanism
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
}

// Stub
func getDNSMapping(serviceName string) (string, error) {
    return serviceName, nil
}

func NewLCAClient(ctx context.Context, serviceName string, serviceAddress string) (LCAClient, error) {
    var err error

    var node LCAClient

    serviceHash, err := getDNSMapping(serviceName)
    if err != nil {
        return node, err
    }

    node.Host, err = New(ctx, nil, NewLCAClientHandler(serviceAddress), LCAClientProtocolID, serviceHash)
    if err != nil {
        return node, err
    }

    return node, nil
}
