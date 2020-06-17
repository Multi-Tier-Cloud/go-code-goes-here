package lca

import (
    "bufio"
    _ "errors"
    "log"
    "strings"

    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/util"
)


// Useful defaults
var (
    DefaultListenAddrs []multiaddr.Multiaddr

    LCAManagerFindProtID protocol.ID // Deprecated, re-use for something else?
    LCAManagerRequestProtID protocol.ID

    LCAAllocatorProtocolID protocol.ID
    LCAAllocatorRendezvous string
)

// Commands
const (
    LCAAPCmdStartProgram = "start-program"
)

// Errors
const (
    LCASErrWriteFail = "Error: writing to buffer"
    LCASErrReadFail = "Error: reading from buffer"
    LCAPErrUnrecognized = "Error: unrecognized command"
    LCAPErrAllocFail = "Error: allocation failed"
    LCAPErrDeadProgram = "Error: program non-responsive"
)

// Initialize defaults
func init() {
    var err error
    DefaultListenAddrs, err = util.StringsToMultiaddrs([]string{
        "/ip4/0.0.0.0/tcp/4001",
    })
    if err != nil {
        panic(err)
    }

    LCAManagerFindProtID = protocol.ID("/LCAManagerFind/1.0")
    LCAManagerRequestProtID = protocol.ID("/LCAManagerRequest/1.0")

    LCAAllocatorProtocolID = protocol.ID("/LCAAllocator/1.0")
    LCAAllocatorRendezvous = "QmQJRHSU69L6W2SwNiKekpUHbxHPXi57tWGRWJaD5NsRxS"

    // Set up logging defaults
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func write(rw *bufio.ReadWriter, msg string) error {
    _, err := rw.WriteString(msg + "\n")
    if err != nil {
        return err
    }
    rw.Flush()
    return nil
}

func read(rw *bufio.ReadWriter) (string, error) {
    str, err := rw.ReadString('\n')
    if err != nil {
        return "", err
    }
    str = strings.TrimSuffix(str, "\n")
    return str, nil
}
