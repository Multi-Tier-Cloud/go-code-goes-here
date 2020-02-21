package lca

import (
    "errors"

    "github.com/libp2p/go-libp2p-core/protocol"

    "github.com/multiformats/go-multiaddr"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
)


var ErrUhOh = errors.New("Communication error with LCA")


var DefaultListenAddrs []multiaddr.Multiaddr

var LCAClientProtocolID protocol.ID

var LCAAgentProtocolID protocol.ID
var LCAAgentRendezvous string


func init() {
    var err error
    DefaultListenAddrs, err = p2pnode.StringsToMultiaddrs([]string{
        "/ip4/0.0.0.0/tcp/4001",
    })
    if err != nil {
        panic(err)
    }

    LCAClientProtocolID = protocol.ID("/LCAClient/1.0")

    LCAAgentProtocolID = protocol.ID("/LCAAgent/1.0")
    LCAAgentRendezvous = "QmQJRHSU69L6W2SwNiKekpUHbxHPXi57tWGRWJaD5NsRxS"
}

