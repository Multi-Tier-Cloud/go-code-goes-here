package main

import (
    "fmt"
    "log"
    "net"
    "syscall"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/t-lin/go-libp2p-gostream"
)

var udpTunnelProtoID = protocol.ID("/LCATunnelUDP/1.0")

// UDP data forwarder
// This function can be used to forward type a stream connection to a UDP
// connection, and vice-versa. In some cases, the same socket is used to
// receive from multiple clients. In such cases, writing to this UDP "Conn"
// object requires an extra parameter, dstAddr. If dstAddr is not nil, then
// consider dst to be type *net.UDPConn and use WriteTo() instead of Write().
//
// TODO: This function currently doesn't seem to end... the I/O functions do not
//       appear to be returning errors. May need some way to timeout the
//       connections so they don't keep consuming memory.
func udpFwdData(src, dst net.Conn, dstAddr net.Addr) {
    var err error
    var nBytes, nBytesW, n int
    buf := make([]byte, 0xffff) // 64k buffer
    for {
        nBytesW = 0

        nBytes, err = src.Read(buf)
        if err != nil {
            if err == syscall.EINVAL {
                log.Printf("Connection %s <=> %s closed", src.LocalAddr(), src.RemoteAddr())
                break
            } else {
                log.Printf("ERROR: Unable to read from UDP connection %s <=> %s\n%v\n",
                    src.LocalAddr(), src.RemoteAddr(), err)
            }
            return
        }
        data := buf[:nBytes]

        for nBytesW < nBytes {
            if dstAddr != nil {
                n, err = dst.(*net.UDPConn).WriteTo(data, dstAddr)
            } else {
                n, err = dst.Write(data)
            }
            if err != nil {
                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed", dst.LocalAddr(), dst.RemoteAddr())
                    break
                } else {
                    log.Printf("ERROR: Unable to write to UDP connection %s <=> %s\n%v\n",
                        dst.LocalAddr(), dst.RemoteAddr(), err)
                }
                return
            }

            nBytesW += n
        }
    }
}

// Demultiplex incoming packets and forward them to destination
func udpServiceProxy(lConn *net.UDPConn, targetPeer peer.ID) {
    defer lConn.Close()

    var err error

    // Since there's no per-client UDP connection object, we'll need to do some
    // manual demultiplexing. Create a separate outgoing P2P stream to the
    // same target service per unique client.
    //
    // TODO: Use a NotifyBundle to watch for stream and connection close events,
    //       and clean this data structure when they happen
    client2RConn := make(map[string]net.Conn)

    var rConn net.Conn
    var exists bool
    var from net.Addr
    var nBytes, nBytesW, n int
    buf := make([]byte, 0xffff) // 64k buffer
    for {
        nBytesW = 0

        nBytes, from, err = lConn.ReadFrom(buf)
        if err != nil {
            if err == syscall.EINVAL {
                log.Printf("Connection %s <=> %s closed",
                    lConn.LocalAddr(), lConn.RemoteAddr())
            } else {
                log.Printf("ERROR: Unable to read from UDP connection %s <=> %s\n%v\n",
                    lConn.LocalAddr(), lConn.RemoteAddr(), err)
            }
            return
        }

        // Create per-client connection with remote service
        if rConn, exists = client2RConn[from.String()]; !exists {
            log.Printf("New UDP conn: %s <=> %s\n", lConn.LocalAddr(), from)

            p2pNode := manager.Host
            rConn, err = gostream.Dial(p2pNode.Ctx, p2pNode.Host, targetPeer, udpTunnelProtoID)
            if err != nil {
                log.Printf("ERROR: Unable to dial target peer %s\n%v\n", targetPeer, err)
                return
            }
            defer rConn.Close()

            client2RConn[from.String()] = rConn

            // Create separate goroutine to handle reverse path
            go udpFwdData(rConn, lConn, from)
        }

        data := buf[:nBytes]

        for nBytesW < nBytes {
            n, err = rConn.Write(data)
            if err != nil {
                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed",
                        rConn.LocalAddr(), rConn.RemoteAddr())
                } else {
                    log.Printf("ERROR: Unable to write to UDP connection %s <=> %s\n%v\n",
                        rConn.LocalAddr(), rConn.RemoteAddr(), err)
                }
                continue
            }

            nBytesW += n
        }
    }
}


// Open UDP tunnel to service and open local UDP listening port
func openUDPProxy(servicePeer peer.ID) (string, error) {
    var listenAddr string
    serviceKey := "udp://" + string(servicePeer)
    if _, exists := serv2Fwd[serviceKey]; !exists {
        udpAddr, err := net.ResolveUDPAddr("udp", ctrlHost + ":") // choose port
        if err != nil {
            return "", fmt.Errorf("Unable to resolve UDP address\n%w\n", err)
        }

        conn, err := net.ListenUDP("udp", udpAddr)
        if err != nil {
            return "", fmt.Errorf("Unable to open UDP listening port\n")
        }

        listenAddr = conn.LocalAddr().String()
        serv2Fwd[serviceKey] = Forwarder {
            ListenAddr: listenAddr,
            udpWorker: udpServiceProxy,
        }
        go serv2Fwd[serviceKey].udpWorker(conn, servicePeer)
    } else {
        listenAddr = serv2Fwd[serviceKey].ListenAddr
    }

    return listenAddr, nil
}

// Handler for udpTunnelProtoID (i.e. invoked at destination proxy)
// Make a connection to the local service and forward data to/from it
func udpTunnelHandler(stream network.Stream) {
    lConn := gostream.NewConn(stream)
    defer lConn.Close()

    // Resolve and open connection to destination service
    rAddr, err := net.ResolveUDPAddr("udp", servEndpoint)
    if err != nil {
        log.Printf("ERROR: Unable to resolve UDP target address %s\n%v\n", servEndpoint, err)
        return
    }

    rConn, err := net.DialUDP("udp", nil, rAddr)
    if err != nil {
        log.Printf("ERROR: Unable to dial UDP target address %s\n%v\n", rAddr, err)
        return
    }
    defer rConn.Close()

    // Forward data in each direction
    go udpFwdData(lConn, rConn, nil)
    udpFwdData(rConn, lConn, nil)
}

