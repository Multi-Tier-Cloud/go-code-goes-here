package main

import (
    "fmt"
    "io"
    "log"
    "net"
    "syscall"
)

// TCP data forwarder
// Simplified version of pipe() from https://github.com/jpillora/go-tcp-proxy/blob/master/proxy.go
func tcpFwdData(src, dst net.Conn) {
    buf := make([]byte, 0xffff) // 64k buffer
    for {
        // NOTE: Using io.Copy or io.CopyBuffer slows down *a lot* after 2-3 runs.
        //       Not clear why right now. Thus, do the copy manually.
        nBytes, err := src.Read(buf)
        if err != nil {
            if err != io.EOF {
                log.Printf("ERROR: Unable to read from connection %s\n%v\n", src, err)
            }
            return
        }
        data := buf[:nBytes]

        nBytes, err = dst.Write(data) // TODO: Ensure nBytes is written
        if err != nil {
            if err != io.EOF {
                fmt.Printf("ERROR: Unable to write to connection %s\n%v\n", dst, err)
            }
            return
        }
    }
}

// Connection forwarder
func tcpFwdConnToServ(lConn net.Conn, targetAddr string) {
    defer lConn.Close()
    log.Printf("Accepted TCP conn: %s <=> %s\n", lConn.LocalAddr(), lConn.RemoteAddr())

    // Resolve and open connection to destination service
    rAddr, err := net.ResolveTCPAddr("tcp", targetAddr)
    if err != nil {
        log.Printf("ERROR: Unable to resolve target address %s\n%v\n", targetAddr, err)
        return
    }

    rConn, err := net.DialTCP("tcp", nil, rAddr)
    if err != nil {
        log.Printf("ERROR: Unable to dial target address %s\n%v\n", targetAddr, err)
        return
    }
    defer rConn.Close()

    // Forward data in each direction
    go tcpFwdData(lConn, rConn)
    tcpFwdData(rConn, lConn)
}

// Implementation of ServiceProxy
func tcpServiceProxy(listen net.Listener, targetAddr string) {
    defer listen.Close()

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
        go tcpFwdConnToServ(conn, targetAddr)
    }
}

// Open TCP tunnel to service and open local TCP listening port
// NOTE: Currently doesn't support SSL/TLS
//
// TODO: In the future, perhaps the connection type info (TCP/UDP) can be stored in hash-lookup?
// TODO: In the future, move towards proxy-to-proxy implementation
//       This may be useful? https://github.com/libp2p/go-libp2p-gostream
// Start separate goroutine for handling connections and proxying to service
//
// Returns a string of the new TCP listening endpoint address
func openTCPProxy(serviceAddr string) (string, error) {
    var listenAddr string
    if _, exists := serv2Fwd[serviceAddr]; !exists {
        listen, err := net.Listen("tcp", ctrlHost + ":") // automatically choose port
        if err != nil {
            return "", fmt.Errorf("Unable to open TCP listening port\n")
        }

        listenAddr = listen.Addr().String()
        serv2Fwd[serviceAddr] = Forwarder {
            ListenAddr: listenAddr,
            tcpWorker: tcpServiceProxy,
        }
        go serv2Fwd[serviceAddr].tcpWorker(listen, serviceAddr)
    } else {
        listenAddr = serv2Fwd[serviceAddr].ListenAddr
    }

    return listenAddr, nil
}

