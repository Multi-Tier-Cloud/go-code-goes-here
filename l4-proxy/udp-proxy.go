package main

import (
    "errors"
    "fmt"
    "io"
    "log"
    "net"
    "strings"
    "sync"
    "syscall"

    //"github.com/libp2p/go-msgio"
    "github.com/libp2p/go-libp2p-core/network"
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
    var data []byte

    srcComm := NewChainMsgCommunicator(src)

    for {
        if data, err = receiveData(srcComm); err != nil {
            log.Printf("ERROR: %v\n", err)
            if errors.Is(err, syscall.EINVAL) || errors.Is(err, io.EOF) {
                log.Printf("Connection %s <=> %s closed by remote",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer())
            } else {
                log.Printf("Unable to read from connection %s <=> %s\n%v\n",
                    src.Conn().LocalPeer(), src.Conn().RemotePeer(), err)
            }
            return
        }

        nBytes = len(data)
        nBytesW = 0
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
    var nBytes int
    buf := make([]byte, MAX_UDP_TUNNEL_PAYLOAD)

    dstComm := NewChainMsgCommunicator(dst)

    for {
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

        if err = sendData(dstComm, data); err != nil {
            if errors.Is(err, syscall.EINVAL) {
                log.Printf("Connection %s <=> %s closed",
                    dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
            } else {
                log.Printf("ERROR: Unable to write to connection %s <=> %s\n%v\n",
                    dst.Conn().LocalPeer(), dst.Conn().RemotePeer(), err)
            }
            return
        }
    }
}

// Demultiplex incoming packets and forward them to destination
func udpServiceProxy(lConn *net.UDPConn, chainSpec []string) {
    defer func() {
        log.Printf("Shutting down proxy for chain %s\n", chainSpec)
        lConn.Close()
    }()

    var err error

    // Since there's no per-client UDP connection object, we'll need to do some
    // manual demultiplexing. Create a separate outgoing P2P stream, wrapped in
    // a chainMsgCommunicator object, to the same target service per unique client.
    client2ChainComm := make(map[string]*chainMsgCommunicator)
    var mapMtx sync.Mutex

    // Use callbacks to remove streams from client2ChainComm
    netCBs := network.NotifyBundle{}
    netCBs.ClosedStreamF = func(net network.Network, stream network.Stream) {
        if stream.Protocol() != udpTunnelProtoID {
            return
        }

        mapMtx.Lock()
        defer mapMtx.Unlock()

        // TODO: Could speed this up with a reverse map (stream ID to client)
        for k, v := range client2ChainComm {
            if v.GetStream() == stream {
                delete(client2ChainComm, k)
                break
            }
        }
    }
    manager.Host.Host.Network().Notify(&netCBs)

    // Begin proxy operations
    var rConn network.Stream
    var exists bool
    var from net.Addr
    var nBytes int
    var writeRetry = false
    buf := make([]byte, MAX_UDP_TUNNEL_PAYLOAD)

    var dstComm *chainMsgCommunicator

    for {
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
        if dstComm, exists = client2ChainComm[from.String()]; !exists {
            log.Printf("New UDP conn: %s <=> %s\n", lConn.LocalAddr(), from)

            // TODO: Each invocation of setupChain() may result in a new set of
            //       instances if services along the old path goes down, or if
            //       conditions and metrics change. Thus, it may have to set up
            //       a new path. In such cases, this code currently suffers from
            //       HOL blocking (i.e. a chain taking too long to set up will block
            //       packets from other clients). It should be re-architected in
            //       the future to avoid such a scenario, where each client is
            //       handled independently.
            if rConn, err = setupChain(chainSpec); err != nil {
                log.Printf("ERROR: Unable to set up chain %s\n%v\n", chainSpec, err)
                return
            }
            dstComm = NewChainMsgCommunicator(rConn)
            client2ChainComm[from.String()] = dstComm

            // Create separate goroutine to handle reverse path
            go udpFwdStream2Conn(rConn, lConn, from)
        }
        mapMtx.Unlock()

        data := buf[:nBytes]

        // Write data to P2P stream (attempt at most twice, see comments below)
        writeRetry = false
        for {
            if err = sendData(dstComm, data); err != nil {
                rConn = dstComm.GetStream()
                rConn.Reset()
                if writeRetry == true {
                    // Already tried twice... give up, but first remove rConn
                    delete(client2ChainComm, from.String())
                    break
                }

                if errors.Is(err, syscall.EINVAL) {
                    log.Printf("Connection %s <=> %s closed",
                        rConn.Conn().LocalPeer(), rConn.Conn().RemotePeer())
                } else {
                    log.Printf("ERROR: Unable to write to connection %s <=> %s\n%v\n",
                        rConn.Conn().LocalPeer(), rConn.Conn().RemotePeer(), err)
                }

                // Two possibilities for failure to write:
                //  1) Old stream may have been closed, but not yet timed out & removed by callback
                //  2) Peer may have been restarted (or a momentary network disconnection)
                // Attempt Write() again with newly created stream
                if rConn, err = setupChain(chainSpec); err != nil {
                    log.Printf("ERROR: Unable to set up chain %s\n%v\n", chainSpec, err)
                    return
                }

                mapMtx.Lock()
                dstComm = NewChainMsgCommunicator(rConn)
                client2ChainComm[from.String()] = dstComm
                go udpFwdStream2Conn(rConn, lConn, from)
                writeRetry = true
                mapMtx.Unlock()
                continue
            }

            // This is really shit code that can be easily fixed with a goto...
            break
        }
    }
}


// Open UDP tunnel to service and open local UDP listening port
func openUDPProxy(chainSpec []string) (string, error) {
    var listenAddr string
    servChainKey := strings.Join(chainSpec, "/")
    if _, exists := serv2Fwd[servChainKey]; !exists {
        udpAddr, err := net.ResolveUDPAddr("udp", ctrlHost + ":") // choose port
        if err != nil {
            return "", fmt.Errorf("Unable to resolve UDP address\n%w\n", err)
        }

        lConn, err := net.ListenUDP("udp", udpAddr)
        if err != nil {
            return "", fmt.Errorf("Unable to open UDP listening port\n")
        }

        listenAddr = lConn.LocalAddr().String()
        serv2Fwd[servChainKey] = Forwarder {
            ListenAddr: listenAddr,
            udpWorker: udpServiceProxy,
        }

        // Pass a signal to the UDP worker to signal when the chain is set up
        go serv2Fwd[servChainKey].udpWorker(lConn, chainSpec)
    } else {
        listenAddr = serv2Fwd[servChainKey].ListenAddr
    }

    return listenAddr, nil
}

func udpResolveAndDial(endpoint string) (net.Conn, error) {
    // Resolve and open connection to destination service
    rAddr, err := net.ResolveUDPAddr("udp", endpoint)
    if err != nil {
        return nil, fmt.Errorf("Unable to resolve UDP target address %s\n%v\n", servEndpoint, err)
    }

    rConn, err := net.DialUDP("udp", nil, rAddr)
    if err != nil {
        return nil, fmt.Errorf("Unable to dial UDP target address %s\n%v\n", rAddr, err)
    }

    return rConn, nil
}

// Handler for udpTunnelProtoID (i.e. invoked at destination proxy)
// Make a connection to the local service and forward data to/from it
func udpEndChainHandler(stream network.Stream) {
    log.Printf("Invoked new UDP EndChainHandler\n")

    rConn, err := udpResolveAndDial(servEndpoint)
    if err != nil {
        log.Printf("ERROR: %v\n", err)
        return
    }

    // Forward data in each direction
    // Closing will be done within udpFwd* functions
    go udpFwdStream2Conn(stream, rConn, nil)
    go udpFwdConn2Stream(rConn, stream)
}

func udpMidChainHandler(inStream, outStream network.Stream) {
    log.Printf("Invoked new UDP MidChainHandler\n")

    rConn, err := udpResolveAndDial(servEndpoint)
    if err != nil {
        log.Printf("ERROR: %v\n", err)
        return
    }

    // Forward data from the inStream to the service, from the service to the
    // outStream, and from the outStream back to inStream.
    // Closing will be done within udpFwd* functions
    go udpFwdStream2Conn(inStream, rConn, nil)
    go udpFwdConn2Stream(rConn, outStream)
    go fwdStream2Stream(outStream, inStream)
}

