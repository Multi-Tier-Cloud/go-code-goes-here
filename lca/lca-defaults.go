package lca

import (
    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
)


// Useful defaults
var DefaultListenAddrs []multiaddr.Multiaddr

var LCAManagerProtocolID protocol.ID

var LCAAllocatorProtocolID protocol.ID
var LCAAllocatorRendezvous string


// Initialize defaults
func init() {
    var err error
    DefaultListenAddrs, err = p2pnode.StringsToMultiaddrs([]string{
        "/ip4/0.0.0.0/tcp/4001",
    })
    if err != nil {
        panic(err)
    }

    LCAManagerProtocolID = protocol.ID("/LCAManager/1.0")

    LCAAllocatorProtocolID = protocol.ID("/LCAAllocator/1.0")
    LCAAllocatorRendezvous = "QmQJRHSU69L6W2SwNiKekpUHbxHPXi57tWGRWJaD5NsRxS"
}

