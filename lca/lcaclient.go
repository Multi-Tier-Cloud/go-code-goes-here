package lca

import (
    "bufio"
    "errors"
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
    peerChan, err := lca.Host.routingDiscovery.FindPeers(lca.Ctx, serviceHash)
    if err != nil {
        panic(err)
    }

    peers := SortPeers(peerChan)

    found := 0
    for _, p := range peers {
        stream, err := lca.Host.Host.NewStream(lca.Ctx, p.ID, LCAClientProtocolID))
        if err != nil {
            continue
        } else {
            logger.Info("Requesting microservice from:", p.ID)
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
                logger.Info("Response valid")
                return str, nil
            }

            logger.Info("Microservice found at", result)
            found = 1
            break
        }
    }
    if found != 1 {
        panic("No reachable LCAs exist.")
    }
}

func requestAlloc(stream network.Stream, string serviceHash)
    rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))
    logger.Info("Sending request string")
    _, err := rw.WriteString(fmt.Sprintf("start program %s\n", serviceName))
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
    logger.Info("Got response:", str)

    logger.Info("Closing stream")
    stream.Close()

    logger.Info("Validating response")
    match, err := regexp.Match("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}:[0-9]{1,4}$", []byte(str))
    if err != nil {
        return "", err
    }

    if match {
        logger.Info("Response valid")
        return str, nil
    }

    return "", ErrUhOh
}

func (lca *LCAClient) AllocService(serviceHash string) (string, error) {
    peerChan, err := lca.Host.routingDiscovery.FindPeers(lca.Ctx, lca.Rendezvous)
    if err != nil {
        panic(err)
    }

    peers := SortPeers(peerChan)

    found := 0
    for _, p := range peers {
        stream, err := lca.Host.Host.NewStream(lca.Ctx, p.ID, LCAServerProtocol))
        if err != nil {
            continue
        } else {
            logger.Info("Requesting microservice from:", p.ID)
            result, err := requestAlloc(stream, serviceHash)
            if err != nil {
                logger.Info("Error:", err)
                continue
            }

            logger.Info("Microservice created at", result)
            found = 1
            break
        }
    }
    if found != 1 {
        panic("No reachable LCAs exist.")
    }
}

// Stub
func pingService(serviceName) err {
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

func NewLCAClient(ctx context.Context, serviceName string, serviceAddress string) LCAClient, err {
    var err error

    var node LCAClient
    node.ServiceName = serviceName
    node.ServiceAddress = serviceAddress

    node.Host, err = New(ctx, nil, LCAClientHandler, LCAClientProtocolID, LCAClientRendezvous)
    if err != nil {
        return nil, err
    }

    return node, nil
}
