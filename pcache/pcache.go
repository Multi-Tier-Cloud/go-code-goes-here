package pcache

import (
    "errors"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
)

// New type with PeerInfo and RCount
// R stands for Reliability and counts how many times
// a peer has been reliable
type RPeerInfo {
    RCount uint
    Info   p2putil.PeerInfo
}

// PeerCache holds the performance requirements
// and peer levels based on reliability
type PeerCache struct {
    ReqPerf p2putil.PerfInd
    NLevels uint
    Levels  [][]RPeerInfo
}

// Constructor for PeerCache
// Takes performance requirements (reqPerf) as argument
func NewPeerCache(reqPerf p2putil.PerfInd) PeerCache {
    peerCache PeerCache
    peerCache.ReqPerf = reqPerf
    // This is hardcoded for now as there really isn't
    // need for any more levels than three
    // Level 0: performant and reliable
    // Level 1: performant but not reliable
    // Level 2: not performant and not reliable
    peerCache.NLevels = 3
    for i := range 3 {
        peerCache.Levels = append(Levels, []RPeerInfo{})
    }
    return peerCache
}

// Gets a reliable peer from cache
func GetPeer(cache *PeerCache) (PeerInfo, error) {
    // Search levels starting from level 0 (most reliable)
    // omitting the last level (non-performant peers due for removal)
    for l := range (cache.NLevels - 1) {
        for p := range cache.Levels[l] {
            // Return the first performant peer
            // TODO: design a fairness policy
            return p.PeerInfo, nil
        }
    }
    return PeerInfo{}, errors.NewError("No suitable peer found in cache")
}

// Helper function that updates RCounts and changes peer reliability levels in cache
func updateCache(node *p2pnode.Node, cache *PeerCache) {
    // First pass: update RCounts
    for l := range cache.NLevels {
        for p := range cache.Levels[l] {
            // Ping peers to check performance
            // TODO: set timeout based on performance requirement
            responseChan := ping.Ping(node.Ctx, node.Host, p.PeerInfo.ID)
            result := <-responseChan
            // If peer isn't up or no set RCount to 0
            perf = PerfInd{RTT: result.RTT}
            if len(p.Addrs) == 0 || result.RTT == 0 {
                p.RCount = 0
            // If peer is up and doesn't meet requirements decrement RCount by 10
            } else if p2putil.PerfCompare(cache.ReqPerf, perf) {
                p.Info.Perf = perf
                if p.RCount < 10 {
                    p.RCount = 0
                } else {
                    p.Rcount -= 10
                }
            // If it does meet requirements then increment RCount up to 100
            } else {
                p.Info.Perf = perf
                if p.RCount < 100 {
                    p.RCount++
                }
            }
        }
    }
    // Second pass: move peers in levels starting from bottom up
    // Remove all peers in level 0
    // TODO: check if this implementation causes memory leaks
    cache.Levels[0] = []RPeerInfo{}
    for i, p := range cache.Levels[cache.NLevels - 1] {
        if p.RCount < 100 {
            // TODO: FIX
            cache.NLevels[l-1]
    }
    // Move peers in middle level(s) to appropriate new levels
    for l := 1; l < cache.NLevels; l++ {
        for p := range cache.Levels[l] {
            if p.RCount < 100 {
                cache.NLevels[l - 1] = append(cache.NLevels[l - 1], p)
                cache.Levels[l][] = nil
        }
    }
}

// Takes care of adding new peers and updating cache levels
// UpdateCache must be run in a separate goroutine
func UpdateCache(node *p2pnode.Node, addpeer <-chan peer.ID, addresult chan<- error, cache *PeerCache) {
    // Start a timer to track when to run update
    timer := time.NewTimer(1 * time.Second)
    for {
        select {
        // Check for new peer add requests
        case id := <-addpeer:
            // Ping peer to check if it's up and for performance
            responseChan := ping.Ping(node.Ctx, node.Host, id)
            result := <-responseChan
            // If peer isn't up send back error
            if len(p.Addrs) == 0 || result.RTT == 0 {
                addresult <- errors.NewError("Attempted to add dead peer")
                break
            }
            // Add peer to cache in lowest level
            cache.L2 = append(cache.Levels[cache.NLevels - 1],
                RPeerInfo{RCount: 0, Peer: p2putil.PeerInfo{
                    Perf: p2putil.PerfInd{RTT: result.RTT}, ID: id})
            // Send back nil to indicate no error
            addresult <- nil
        default:
            select {
            // If timer has fired, update cache
            case <-timer.C:
                updateCache(node, cache)
            // If the timer hasn't fired yet
            default:
                // Do nothing
            }
        }
    }
}
