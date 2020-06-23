package main

import (
    "fmt"
    "io"
    "log"
    "net"
    "sync"
    "syscall"

    "github.com/libp2p/go-msgio"
    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/peer"
    "github.com/libp2p/go-libp2p-core/protocol"
)

var udpTunnelProtoID = protocol.ID("/LCATunnelUDP/1.0")

const MAX_UDP_TUNNEL_PAYLOAD = 0xffff - 8 - 20 // Max uint16 - UDP header size - IP header size

// UDP data forwarder functions (udpFwdStream2Conn and udpFwdConn2Stream)
// Performs bi-directional close at the end (ending one will end the other).
//
// These function can be used to forward type a stream connection to a UDP
// connection, and vice-versa. In some cases, the same socket is used to
// receive from multiple clients. In such cases, writing to this UDP "Conn"
// object requires an extra parameter, dstAddr. If dstAddr is not nil, then
// consider dst to be type *net.UDPConn and use WriteTo() instead of Write().

func udpFwdStream2Conn(src network.Stream, dst net.Conn, dstAddr net.Addr) {
    if dstAddr == nil {
        // Not shared amongst multiple clients, safe to close upstream connection
        defer func() {
            log.Printf("Closing connection %s <=> %s\n", dst.LocalAddr(), dst.RemoteAddr())
            dst.Close()
        }()
    }
    defer src.Reset()

    var err error
    var nBytes, nBytesW, n int
    buf := make([]byte, MAX_UDP_TUNNEL_PAYLOAD)
    msgR := msgio.NewVarintReaderSize(src, len(buf))

    for {
        nBytesW = 0

        nBytes, err = msgR.Read(buf)
        if err != nil {
            if err == syscall.EINVAL || err == io.EOF {
                log.Printf("Connection %s <=> %s closed by remote",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer())
            } else {
                log.Printf("Unable to read from UDP connection %s <=> %s\n%v\n",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer(), err)
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

func udpFwdConn2Stream(src net.Conn, dst network.Stream) {
    defer func() {
        log.Printf("Closing connection %s <=> %s\n",
            dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
        dst.Reset()
        src.Close()
    }()

    var err error
    var nBytes, nBytesW, n int
    buf := make([]byte, MAX_UDP_TUNNEL_PAYLOAD)
    msgW := msgio.NewVarintWriter(dst)

    for {
        nBytesW = 0

        nBytes, err = src.Read(buf)
        if err != nil {
            if err == syscall.EINVAL || err == io.EOF {
                log.Printf("Connection %s <=> %s closed by remote", src.LocalAddr(), src.RemoteAddr())
            } else {
                log.Printf("Unable to read from UDP connection %s <=> %s\n%v\n",
                    src.LocalAddr(), src.RemoteAddr(), err)
            }
            return
        }
        data := buf[:nBytes]

        for nBytesW < nBytes {
            n, err = msgW.Write(data)
            if err != nil {
                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed",
                        dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
                } else {
                    log.Printf("ERROR: Unable to write to UDP connection %s <=> %s\n%v\n",
                        dst.Conn().LocalPeer(), dst.Conn().RemotePeer(), err)
                }
                return
            }

            nBytesW += n
        }
    }
}

func createStream(targetPeer peer.ID) (network.Stream, error) {
    p2pNode := manager.Host
    stream, err := p2pNode.Host.NewStream(p2pNode.Ctx, targetPeer, udpTunnelProtoID)
    if err != nil {
        return nil, err
    }

    return stream, nil
}

// Demultiplex incoming packets and forward them to destination
func udpServiceProxy(p2pNet network.Network, lConn *net.UDPConn, targetPeer peer.ID) {
    defer func() {
        log.Printf("Shutting down proxy to peer %s\n", targetPeer)
        lConn.Close()
    }()

    var err error

    // Since there's no per-client UDP connection object, we'll need to do some
    // manual demultiplexing. Create a separate outgoing P2P stream to the
    // same target service per unique client.
    client2Stream := make(map[string]network.Stream)
    var mapMtx sync.Mutex

    // Use callbacks to remove streams from client2Stream
    netCBs := network.NotifyBundle{}
    netCBs.ClosedStreamF = func(net network.Network, stream network.Stream) {
        if stream.Protocol() != udpTunnelProtoID {
            return
        }

        mapMtx.Lock()
        defer mapMtx.Unlock()

        // TODO: Could speed this up with a reverse map (stream ID to client)
        for k, v := range client2Stream {
            if v == stream {
                delete(client2Stream, k)
                break
            }
        }
    }
    p2pNet.Notify(&netCBs)

    // Begin proxy operations
    var rConn network.Stream
    var exists bool
    var from net.Addr
    var nBytes, nBytesW, n int
    var writeRetry = false
    var msgW msgio.WriteCloser
    buf := make([]byte, MAX_UDP_TUNNEL_PAYLOAD)

    for {
        nBytesW = 0

        // Read data from listening socket
        nBytes, from, err = lConn.ReadFrom(buf)
        if err != nil {
            if err == syscall.EINVAL || err == io.EOF {
                log.Printf("Connection %s <=> %s closed by remote",
                    lConn.LocalAddr(), lConn.RemoteAddr())
            } else {
                log.Printf("ERROR: Unable to read from UDP connection %s <=> %s\n%v\n",
                    lConn.LocalAddr(), lConn.RemoteAddr(), err)
            }

            return // or continue?
        }

        // Create per-client connection (stream) with remote service
        mapMtx.Lock()
        if rConn, exists = client2Stream[from.String()]; !exists {
            log.Printf("New UDP conn: %s <=> %s\n", lConn.LocalAddr(), from)

            if rConn, err = createStream(targetPeer); err != nil {
                log.Printf("ERROR: Unable to dial target peer %s\n%v\n", targetPeer, err)
                return
            }
            client2Stream[from.String()] = rConn

            // Create separate goroutine to handle reverse path
            go udpFwdStream2Conn(rConn, lConn, from)
        }
        mapMtx.Unlock()

        data := buf[:nBytes]

        // Write data to P2P stream (attempt at most twice, see comments below)
        writeRetry = false
        msgW = msgio.NewVarintWriter(rConn)
        for nBytesW < nBytes {
            n, err = msgW.Write(data)
            if err != nil {
                rConn.Reset()
                if writeRetry == true {
                    // Already tried twice... give up, but first remove rConn
                    delete(client2Stream, from.String())
                    break
                }

                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed",
                        rConn.Conn().LocalPeer(), rConn.Conn().RemotePeer())
                } else {
                    log.Printf("ERROR: Unable to write to UDP connection %s <=> %s\n%v\n",
                        rConn.Conn().LocalPeer(), rConn.Conn().RemotePeer(), err)
                }

                // Two possibilities for failure to write:
                //  1) Old stream may have been closed, but not yet timed out & removed by callback
                //  2) Peer may have been restarted (or a momentary network disconnection)
                // Attempt Write() again with newly created stream
                mapMtx.Lock()
                if rConn, err = createStream(targetPeer); err != nil {
                    log.Printf("ERROR: Unable to dial target peer %s\n%v\n", targetPeer, err)
                    return
                }
                client2Stream[from.String()] = rConn
                go udpFwdStream2Conn(rConn, lConn, from)
                msgW = msgio.NewVarintWriter(rConn)
                writeRetry = true
                mapMtx.Unlock()

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

        lConn, err := net.ListenUDP("udp", udpAddr)
        if err != nil {
            return "", fmt.Errorf("Unable to open UDP listening port\n")
        }

        listenAddr = lConn.LocalAddr().String()
        serv2Fwd[serviceKey] = Forwarder {
            ListenAddr: listenAddr,
            udpWorker: udpServiceProxy,
        }
        go serv2Fwd[serviceKey].udpWorker(manager.Host.Host.Network(), lConn, servicePeer)
    } else {
        listenAddr = serv2Fwd[serviceKey].ListenAddr
    }

    return listenAddr, nil
}

// Handler for udpTunnelProtoID (i.e. invoked at destination proxy)
// Make a connection to the local service and forward data to/from it
func udpTunnelHandler(stream network.Stream) {
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

    // Forward data in each direction
    // Closing will be done within udpFwd* functions
    go udpFwdStream2Conn(stream, rConn, nil)
    go udpFwdConn2Stream(rConn, stream)
}

