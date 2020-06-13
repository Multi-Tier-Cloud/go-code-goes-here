package main

import (
    "fmt"
    "log"
    "net"
    "syscall"
)

// UDP data forwarder
func udpFwdData(src, dst *net.UDPConn, dstAddr net.Addr) {
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
            continue
        }
        data := buf[:nBytes]

        for nBytesW < nBytes {
            n, err = dst.WriteTo(data, dstAddr)
            if err != nil {
                if err == syscall.EINVAL {
                    log.Printf("Connection %s <=> %s closed", src.LocalAddr(), src.RemoteAddr())
                    break
                } else {
                    log.Printf("ERROR: Unable to write to UDP connection %s <=> %s\n%v\n",
                        src.LocalAddr(), src.RemoteAddr(), err)
                }
                continue
            }

            nBytesW += n
        }
    }
}

// Demultiplex incoming packets and forward them to destination
func udpServiceProxy(lConn *net.UDPConn, targetAddr string) {
    defer lConn.Close()

    var err error

    // Resolve and open connection to destination service
    rAddr, err := net.ResolveUDPAddr("udp", targetAddr)
    if err != nil {
        log.Printf("ERROR: Unable to resolve UDP target address %s\n%v\n", targetAddr, err)
        return
    }

    // Since there's no per-client UDP connection object, we'll need to do some
    // manual demultiplexing. Create a separate outgoing UDP "connection" to the
    // same target service per unique client.
    client2RConn := make(map[string]*net.UDPConn)

    var rConn *net.UDPConn
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

            rConn, err = net.DialUDP("udp", nil, rAddr)
            if err != nil {
                log.Printf("ERROR: Unable to dial UDP target address %s\n%v\n",
                    targetAddr, err)
                continue // Or return? Other connections may be okay...
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
func openUDPProxy(serviceAddr string) (string, error) {
    var listenAddr string
    if _, exists := serv2Fwd[serviceAddr]; !exists {
        udpAddr, err := net.ResolveUDPAddr("udp", ctrlHost + ":") // choose port
        if err != nil {
            return "", fmt.Errorf("Unable to resolve UDP address\n%w\n", err)
        }

        conn, err := net.ListenUDP("udp", udpAddr)
        if err != nil {
            return "", fmt.Errorf("Unable to open UDP listening port\n")
        }

        listenAddr = conn.LocalAddr().String()
        serv2Fwd[serviceAddr] = Forwarder {
            ListenAddr: listenAddr,
            udpWorker: udpServiceProxy,
        }
        go serv2Fwd[serviceAddr].udpWorker(conn, serviceAddr)
    } else {
        listenAddr = serv2Fwd[serviceAddr].ListenAddr
    }

    return listenAddr, nil
}
