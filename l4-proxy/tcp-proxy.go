package main

import (
    "errors"
    "fmt"
    "io"
    "log"
    "net"
    "strings"
    "syscall"

    "github.com/libp2p/go-libp2p-core/network"
    "github.com/libp2p/go-libp2p-core/protocol"
)

var tcpTunnelProtoID = protocol.ID("/LCATunnelTCP/1.0")

const MAX_TCP_TUNNEL_PAYLOAD = 0xffff - 20 - 20 // Max uint16 - Min TCP header size - IP header size

// TODO: Only difference between this and udpFwdStream2Conn is
//       the dstAddr param and WriteTo() in the body... can they
//       be somehow merged?
func tcpFwdStream2Conn(src network.Stream, dst net.Conn) {
    // Not shared amongst multiple clients, safe to close upstream connection
    defer func() {
        log.Printf("Closing connection %s <=> %s\n", dst.LocalAddr(), dst.RemoteAddr())
        src.Reset()
        dst.Close()
    }()

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
            n, err = dst.Write(data)
            if err != nil {
                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed", dst.LocalAddr(), dst.RemoteAddr())
                } else {
                    log.Printf("ERROR: Unable to write to TCP connection %s <=> %s\n%v\n",
                        dst.LocalAddr(), dst.RemoteAddr(), err)
                }
                return
            }

            nBytesW += n
        }
    }
}

// TODO: This now seems identical to udpFwdConn2Stream... except payload size. Merge the two?
func tcpFwdConn2Stream(src net.Conn, dst network.Stream) {
    defer func() {
        log.Printf("Closing connection %s <=> %s\n",
            dst.Conn().LocalPeer(), dst.Conn().RemotePeer())
        dst.Reset()
        src.Close()
    }()

    var err error
    var nBytes int
    buf := make([]byte, MAX_TCP_TUNNEL_PAYLOAD)

    dstComm := NewChainMsgCommunicator(dst)

    for {
        // NOTE: Using io.Copy or io.CopyBuffer slows down *a lot* after 2-3 runs.
        //       Not clear why right now. Thus, do the copy manually.
        nBytes, err = src.Read(buf)
        if err != nil {
            if errors.Is(err, syscall.EINVAL) || errors.Is(err, io.EOF) {
                log.Printf("Connection %s <=> %s closed by remote", src.LocalAddr(), src.RemoteAddr())
            } else {
                log.Printf("Unable to read from TCP connection %s <=> %s\n%v\n",
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

// Connection forwarder
// This function is separate from tcpServiceProxy() so as to avoid HOL
// blocking in the event the chain setup takes too long and another
// client wants to connect.
func tcpFwdConnToServ(lConn net.Conn, chainSpec []string) {
    log.Printf("Accepted TCP conn: %s <=> %s\n", lConn.LocalAddr(), lConn.RemoteAddr())

    rConn, err := setupChain(chainSpec)
    if err != nil {
        log.Printf("ERROR: Unable to set up chain %s\n%v\n", chainSpec, err)
        return
    }

    // Forward data in each direction
    // Closing will be done within the tcpFwd* functions
    go tcpFwdConn2Stream(lConn, rConn)
    go tcpFwdStream2Conn(rConn, lConn)
}

// Implementation of TCP service proxy. Invoked by the source proxy in the
// chain. Responsible for setting up the chain and passing the ingress stream
// to the appropriate data forwarders.
func tcpServiceProxy(listen net.Listener, chainSpec []string) {
    defer func() {
        log.Printf("Shutting down proxy for chain %s\n", chainSpec)
        listen.Close()
    }()

    for {
        conn, err := listen.Accept()
        if err != nil {
            if err == syscall.EINVAL {
                log.Printf("Listener (%s) closed", listen.Addr())
                break
            }
            log.Printf("Listener (%s) unable to accept connection\n%v\n", listen.Addr(), err)
            continue
        }

        // Forward connection
        go tcpFwdConnToServ(conn, chainSpec)
    }
}

// Open TCP tunnel to service and open local TCP listening port
// Start separate goroutine for handling connections and proxying to service
// NOTE: Currently doesn't support SSL/TLS
//
// TODO: In the future, perhaps the connection type info (TCP/UDP) can be stored in hash-lookup?
//
// Returns a string of the new TCP listening endpoint address
func openTCPProxy(chainSpec []string) (string, error) {
    var listenAddr string
    serviceKey := strings.Join(chainSpec, "/")
    if _, exists := serv2Fwd[serviceKey]; !exists {
        listen, err := net.Listen("tcp", ctrlHost + ":") // automatically choose port
        if err != nil {
            return "", fmt.Errorf("Unable to open TCP listening port\n")
        }

        listenAddr = listen.Addr().String()
        serv2Fwd[serviceKey] = Forwarder {
            ListenAddr: listenAddr,
            tcpWorker: tcpServiceProxy,
        }
        go serv2Fwd[serviceKey].tcpWorker(listen, chainSpec)
    } else {
        listenAddr = serv2Fwd[serviceKey].ListenAddr
    }

    return listenAddr, nil
}

// Handler for tcpTunnelProtoID (i.e. invoked at destination proxy)
// Make a connection to the local service and forward data to/from it
func tcpEndChainHandler(stream network.Stream) {
    // Resolve and open connection to destination service
    rAddr, err := net.ResolveTCPAddr("tcp", servEndpoint)
    if err != nil {
        log.Printf("ERROR: Unable to resolve target address %s\n%v\n", servEndpoint, err)
        return
    }

    rConn, err := net.DialTCP("tcp", nil, rAddr)
    if err != nil {
        log.Printf("ERROR: Unable to dial target address %s\n%v\n", servEndpoint, err)
        return
    }

    // Forward data in each direction
    go tcpFwdStream2Conn(stream, rConn)
    go tcpFwdConn2Stream(rConn, stream)
}

func tcpMidChainHandler(inStream, outStream network.Stream) {
    // Resolve and open connection to destination service
    rAddr, err := net.ResolveTCPAddr("tcp", servEndpoint)
    if err != nil {
        log.Printf("ERROR: Unable to resolve target address %s\n%v\n", servEndpoint, err)
        return
    }

    rConn, err := net.DialTCP("tcp", nil, rAddr)
    if err != nil {
        log.Printf("ERROR: Unable to dial target address %s\n%v\n", servEndpoint, err)
        return
    }

    // Forward data in each direction
    go tcpFwdStream2Conn(inStream, rConn)
    go tcpFwdConn2Stream(rConn, outStream)
    go fwdStream2Stream(outStream, inStream)
}

