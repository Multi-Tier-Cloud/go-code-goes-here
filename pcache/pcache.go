package pcache

import (
    "context"
    "errors"
    "fmt"
    "sort"
    "sync"
    "time"

    "github.com/libp2p/go-libp2p/p2p/protocol/ping"
    "github.com/libp2p/go-libp2p-core/peer"

    "github.com/Multi-Tier-Cloud/common/p2pnode"
    "github.com/Multi-Tier-Cloud/common/p2putil"
)

type PerfConf struct {
    SoftReq p2putil.PerfInd
    HardReq p2putil.PerfInd
}

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
    ReqPerf PerfConf
    NLevels uint
    Levels  [][]RPeerInfo
    // Private variables
    node    *p2pnode.Node
    mux     sync.Mutex
    rmax    uint
}

// Request struct to request addition of peer in UpdateCache
type PeerRequest struct {
    ID      peer.ID
    Hash    string
    Address string
}

func RPeerInfoCompare(l, r RPeerInfo) bool {
    return p2putil.PerfIndCompare(l.Info.Perf, r.Info.Perf)
}

// Constructor for PeerCache
// Takes performance requirements (reqPerf) as argument
func NewPeerCache(reqPerf PerfConf, node *p2pnode.Node) PeerCache {
    var peerCache PeerCache
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
    // Private variables
    peerCache.node = node
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

func (cache *PeerCache) AddPeer(p PeerRequest) {
    fmt.Println("Adding new peer with ID", p.ID)
    // Add peer to cache in second lowest level
    cache.mux.Lock()
    defer cache.mux.Unlock()
    cache.Levels[cache.NLevels-2] = apts(cache.Levels[cache.NLevels-2],
        RPeerInfo{
            // Set RCount to 50 so it doesn't immediately get kicked
            // to the last level upon cache update
            RCount: 50, Info: p2putil.PeerInfo{
                Perf: p2putil.PerfInd{}, ID: p.ID,
            }, Hash: p.Hash, Address: p.Address,
        },
    )
}

func (cache *PeerCache) RemovePeer(id peer.ID, address string) {
    cache.mux.Lock()
    defer cache.mux.Unlock()
    for l := uint(0); l < (cache.NLevels-1); l++ {
        count := uint(0)
        for i, p := range cache.Levels[l] {
            // In each cache level look at the first rmax peers
            if count < cache.rmax {
                // Check if the current peer is the one to delete
                if id == p.Info.ID && address == p.Address {
                    cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
                    return
                }
                count++
            } else {
                // Go to next level
                break
            }
        }
    }
}

// Gets a reliable peer from cache
func (cache *PeerCache) GetPeer(hash string) (peer.ID, string, error) {
    // Search levels starting from level 0 (most reliable)
    // omitting the last level (non-performant peers due for removal)
    cache.mux.Lock()
    defer cache.mux.Unlock()
    for l := uint(0); l < (cache.NLevels-1); l++ {
        for _, p := range cache.Levels[l] {
            // Return the first performant peer
            if p.Hash == hash {
                fmt.Println("Getting peer with ID", p.Info.ID, "from pcache")
                return p.Info.ID, p.Address, nil
            }
        }
    }
    return peer.ID(""), "", errors.New("No suitable peer found in cache")
}


// Helper function that updates RCounts and changes peer reliability levels in cache
func (cache *PeerCache) updateCache() {
    cache.mux.Lock()
    defer cache.mux.Unlock()
    // Setup context
    ctx, cancel := context.WithCancel(cache.node.Ctx)
    defer cancel()
    nLevels := cache.NLevels
    // First pass: update RCounts
    for l := uint(0); l < nLevels; l++ {
        for i, p := range cache.Levels[l] {
            // Ping peers to check performance
            // TODO: set timeout based on performance requirement
            responseChan := ping.Ping(ctx, cache.node.Host, p.Info.ID)
            result := <-responseChan
            // If peer isn't up or doesn't meet hard requirements remove from cache
            perf := p2putil.PerfInd{RTT: result.RTT}
            if result.RTT == 0 || p2putil.PerfIndCompare(cache.ReqPerf.HardReq, perf) {
                cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
            // If peer is up and doesn't meet requirements decrement RCount by 10
            } else if p2putil.PerfIndCompare(cache.ReqPerf.SoftReq, perf) {
                cache.Levels[l][i].Info.Perf = perf
                if p.RCount < 10 {
                    cache.Levels[l][i].RCount = 0
                } else {
                    cache.Levels[l][i].RCount -= 10
                }
            // If it does meet requirements then increment RCount
            } else {
                cache.Levels[l][i].Info.Perf = perf
                if p.RCount < 100 {
                    cache.Levels[l][i].RCount++
                }
            }
        }
    }
    // Second pass: move updated peers into appropriate new levels
    // Move peers in top level down if they become unreliable
    for i := 0; i < len(cache.Levels[0]); i++ {
        if cache.Levels[0][i].RCount < 90 {
            // Set RCount to 50 when dropping to penalize inconsistency
            cache.Levels[0][i].RCount = 50
            cache.Levels[1] = apts(cache.Levels[1], cache.Levels[0][i])
            cache.Levels[0] = rpfs(cache.Levels[0], uint(i))
            // Decrement i to account for rpfs
            i--
        }
    }
    // Move peers in middle level(s) to appropriate new levels
    for l := uint(1); l < (cache.NLevels-1); l++ {
        for i := 0; i < len(cache.Levels[l]); i++ {
            if cache.Levels[l][i].RCount > 90 {
                // Do not change RCount when promoting so consistently
                // reliable peers get promoted quickly
                cache.Levels[l-1] = apts(cache.Levels[l-1], cache.Levels[l][i])
                cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
                // Decrement i to account for rpfs
                i--
            } else if cache.Levels[l][i].RCount < 10 {
                // Set RCount to 50 when dropping to give a slight
                // buffer so nodes do not chain drop to the last level
                // while it is recovering
                cache.Levels[l][i].RCount = 50
                cache.Levels[l+1] = apts(cache.Levels[l+1], cache.Levels[l][i])
                cache.Levels[l] = rpfs(cache.Levels[l], uint(i))
                // Decrement i to account for rpfs
                i--
            }
        }
    }
    // Remove all peers in last level (unreliable peers)
    cache.Levels[nLevels-1] = cache.Levels[nLevels-1][0:0]
    // Third pass: sort elements based on performance
    for l := uint(0); l < nLevels; l++ {
        sort.Slice(cache.Levels[l], func(i, j int) bool {
            return RPeerInfoCompare(cache.Levels[l][i], cache.Levels[l][j])
        })
    }
}


// Takes care of adding new peers and updating cache levels
// UpdateCache ideally is run in a separate goroutine
func (cache *PeerCache) UpdateCache() {
    // Start a timer to track when to run update
    fmt.Println("Launching cache update function")
    ticker := time.NewTicker(1 * time.Second)
    for {
        if cache.node.Ctx.Err() != nil {
            ticker.Stop()
            return
        }
        select {
        case <-cache.node.Ctx.Done():
            ticker.Stop()
            return
        case <-ticker.C:
            // Kill ticker to prevent ticking while updating cache
            ticker.Stop()
            cache.updateCache()
            // Create new ticker to restart ticking after update
            ticker = time.NewTicker(1 * time.Second)
        }
    }
}
