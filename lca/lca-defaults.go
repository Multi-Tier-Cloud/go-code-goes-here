package lca

import (
    "log"

    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/util"
)


// Useful defaults
var DefaultListenAddrs []multiaddr.Multiaddr

var LCAManagerProtocolID protocol.ID

var LCAAllocatorProtocolID protocol.ID
var LCAAllocatorRendezvous string


// Initialize defaults
func init() {
    var err error
    DefaultListenAddrs, err = util.StringsToMultiaddrs([]string{
        "/ip4/0.0.0.0/tcp/4001",
    })
    if err != nil {
        panic(err)
    }

    LCAManagerProtocolID = protocol.ID("/LCAManager/1.0")

    LCAAllocatorProtocolID = protocol.ID("/LCAAllocator/1.0")
    LCAAllocatorRendezvous = "QmQJRHSU69L6W2SwNiKekpUHbxHPXi57tWGRWJaD5NsRxS"

    // Set up logging defaults
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

