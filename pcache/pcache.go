package pcache

import (
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
)

// New type with PeerInfo and RCount
// R stands for Reliability and counts how many times
// a peer has been reliable
type RPeerInfo struct {
    RCount  uint
    Info    p2putil.PeerInfo
    Hash    string
    Address string
}

// PeerCache holds the performance requirements
// and peer levels based on reliability
type PeerCache struct {
    ReqPerf p2putil.PerfInd
    NLevels uint
    Levels  [][]RPeerInfo
    // Synchronization
    node    *p2pnode.Node
    mux     sync.Mutex
    rmax    uint
}

// Request struct for request addition of peer in UpdateCache
type PeerRequest struct {
    ID      peer.ID
    Hash    string
    Address string
}

// Constructor for PeerCache
// Takes performance requirements (reqPerf) as argument
func NewPeerCache(reqPerf p2putil.PerfInd, node *p2pnode.Node) PeerCache {
    var peerCache PeerCache
    peerCache.node = node
    peerCache.ReqPerf = reqPerf
    // This is hardcoded for now as there really isn't
    // need for any more levels than three
    // Level 0: performant and reliable
    // Level 1: performant but not reliable
    // Level 2: not performant and not reliable
    peerCache.NLevels = 3
    peerCache.Levels = [][]RPeerInfo{}
    for i := uint(0); i < peerCache.NLevels; i++ {
        peerCache.Levels = append(peerCache.Levels, []RPeerInfo{})
    }
    // Look for top 3 cache results when deleting
    peerCache.rmax = 3
    return peerCache
}

// Helper function "add peer to slice"
func apts(s []RPeerInfo, p RPeerInfo) []RPeerInfo {
    s = append(s, p)
    return s
}

// Helper function "remove peer from slice"
func rpfs(s []RPeerInfo, i uint) []RPeerInfo {
    s[len(s)-1], s[i] = s[i], s[len(s)-1]
    return s[:len(s)-1]
}

func (cache *PeerCache) AddPeer(p PeerRequest) error {
    fmt.Println("Adding new peer with ID", p.ID)
    // Ping peer to check if it's up and for performance
    responseChan := ping.Ping(cache.node.Ctx, cache.node.Host, p.ID)
    result := <-responseChan
    // If peer isn't up send back error
    // TODO: check if this is the correct error condition
    if result.RTT == 0 {
        return errors.New("Peer is not up")
    }
    // Add peer to cache in second lowest level
    cache.mux.Lock()
    defer cache.mux.Unlock()
    cache.Levels[cache.NLevels-2] = apts(cache.Levels[cache.NLevels-2],
        RPeerInfo{
            RCount: 50, Info: p2putil.PeerInfo{
                Perf: p2putil.PerfInd{RTT: result.RTT}, ID: p.ID,
            }, Hash: p.Hash, Address: p.Address,
        },
    )
    return nil
}

func (cache *PeerCache) RemovePeer(id peer.ID, address string) {
    cache.mux.Lock()
    defer cache.mux.Unlock()
    count := uint(0)
    for l := uint(0); l < (cache.NLevels-1); l++ {
        for i, p := range cache.Levels[l] {
            if count < cache.rmax {
                if id == p.Info.ID && address == p.Address {
                    cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
                    return
                }
            } else {
                return
            }
        }
    }
    return
}

// Gets a reliable peer from cache
func (cache *PeerCache) GetPeer(hash string) (p2putil.PeerInfo, string, error) {
    // Search levels starting from level 0 (most reliable)
    // omitting the last level (non-performant peers due for removal)
    cache.mux.Lock()
    defer cache.mux.Unlock()
    for l := uint(0); l < (cache.NLevels-1); l++ {
        for _, p := range cache.Levels[l] {
            // Return the first performant peer
            // TODO: design a fairness policy
            if p.Hash == hash {
                fmt.Println("Getting peer with ID", p.Info.ID, "from pcache")
                return p.Info, p.Address, nil
            }
        }
    }
    return p2putil.PeerInfo{}, "", errors.New("No suitable peer found in cache")
}


// Helper function that updates RCounts and changes peer reliability levels in cache
func (cache *PeerCache) updateCache() {
    cache.mux.Lock()
    defer cache.mux.Unlock()
    nLevels := cache.NLevels
    // First pass: update RCounts
    for l := uint(0); l < nLevels; l++ {
        for _, p := range cache.Levels[l] {
            // Ping peers to check performance
            // TODO: set timeout based on performance requirement
            responseChan := ping.Ping(cache.node.Ctx, cache.node.Host, p.Info.ID)
            result := <-responseChan
            // If peer isn't up set RCount to 0
            perf := p2putil.PerfInd{RTT: result.RTT}
            if result.RTT == 0 {
                p.RCount = 0
            // If peer is up and doesn't meet requirements decrement RCount by 10
            } else if p2putil.PerfIndCompare(cache.ReqPerf, perf) {
                p.Info.Perf = perf
                if p.RCount < 10 {
                    p.RCount = 0
                } else {
                    p.RCount -= 10
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
    // Second pass: move peers into appropriate new levels
    // Remove all peers in last level (unreliable peers)
    // TODO: check if this implementation causes memory leaks
    cache.Levels[nLevels-1] = []RPeerInfo{}
    // Move peers in top level down if they become unreliable
    for i, p := range cache.Levels[0] {
        if p.RCount < 90 {
            // Set RCount to 50 when dropping to penalize inconsistency
            p.RCount = 50
            cache.Levels[1] = append(cache.Levels[1], p)
            cache.Levels[0] = rpfs(cache.Levels[0], uint(i))
        }
    }
    // Move peers in middle level(s) to appropriate new levels
    for l := uint(1); l < (cache.NLevels-1); l++ {
        for i, p := range cache.Levels[l] {
            if p.RCount > 90 {
                // Do not change RCount so consistently reliable peers
                // get promoted quickly
                cache.Levels[l-1] = append(cache.Levels[l-1], p)
                cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
            } else if p.RCount < 10 {
                // Set RCount to 50 when dropping to give a slight
                // buffer so nodes do not chain drop to the last level
                // while it is recovering
                p.RCount = 50
                cache.Levels[l+1] = append(cache.Levels[l+1], p)
                cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
            }
        }
    }
}


// Takes care of adding new peers and updating cache levels
// UpdateCache must be run in a separate goroutine
// This function no longer reports errors as there's no good way to
// allow the calling function which itself is multithreaded to find
// the error message corresponding to its request
// The requesting function must loop on add and get until get succeeds
// See cache section of service-manager/proxy/proxy.go in requestHandler for example
func (cache *PeerCache) UpdateCache() {
    // Start a timer to track when to run update
    fmt.Println("Launching cache update function")
    ticker := time.NewTicker(1 * time.Second)
    for {
        select {
        case <-ticker.C:
            // Kill ticker to prevent ticking while updating cache
            ticker.Stop()
            cache.updateCache()
            // Create new ticker to restart ticking after update
            ticker = time.NewTicker(1 * time.Second)
        default:
            // Do nothing
        }
    }
}
