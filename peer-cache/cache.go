package cache

import (
    "github.com/Multi-Tier-Cloud/common/p2putil"
)

type P2PCache struct {
    L0 []p2putil.PeerInfo
    L1 []p2putil.PeerInfo
    L2 []p2putil.PeerInfo
}
