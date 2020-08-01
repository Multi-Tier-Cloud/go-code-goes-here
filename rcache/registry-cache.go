package rcache

// Cache registry service info
// Keeps track of when entries get cached and expires entries after a specified TTL

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
    "github.com/libp2p/go-libp2p-discovery"

	"github.com/Multi-Tier-Cloud/service-registry/registry"
)

func init() {
    log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

type RegistryCache struct {
	ctx context.Context
	host host.Host
	routingDiscovery *discovery.RoutingDiscovery

	ttl time.Duration // time-to-live
	data map[string]cacheEntry
	mux sync.RWMutex
}

type cacheEntry struct {
	Info registry.ServiceInfo
	Expiry time.Time
}

// Create new RegistryCache
// ctx, host, routingDiscovery: used when you call GetOrRequestService() to contact registry-service
// ttl: time-to-live in seconds
func NewRegistryCache(ctx context.Context, host host.Host,
	routingDiscovery *discovery.RoutingDiscovery, ttl int) *RegistryCache {

	return &RegistryCache{
		ctx: ctx,
		host: host,
		routingDiscovery: routingDiscovery,
		ttl: time.Duration(ttl) * time.Second,
		data: make(map[string]cacheEntry),
	}
}

// Maps serviceName to info in cache
// Sets expiry by checking current time and adding TTL
func (rc *RegistryCache) Add(serviceName string, info registry.ServiceInfo) {
	entry := cacheEntry{
		Info: info,
		Expiry: time.Now().Add(rc.ttl),
	}
	rc.mux.Lock()
	rc.data[serviceName] = entry
	rc.mux.Unlock()
}

// If serviceName entry doesn't exist in cache or is expired, ok is false
// Otherwise, returns mapped ServiceInfo and ok is true
func (rc *RegistryCache) Get(serviceName string) (info registry.ServiceInfo, ok bool) {
	rc.mux.RLock()
	entry, ok := rc.data[serviceName]
	rc.mux.RUnlock()
	if !ok {
		return info, false
	}
	if time.Now().After(entry.Expiry) {
		// Expired cache entry
		return info, false
	}
	return entry.Info, true
}

// Delete entry from cache
func (rc *RegistryCache) Delete(serviceName string) {
	rc.mux.Lock()
	delete(rc.data, serviceName)
	rc.mux.Unlock()
}

// Try to get service info from cache
// If not in cache or expired, request it from registry-service and add it to cache
func (rc *RegistryCache) GetOrRequestService(serviceName string) (info registry.ServiceInfo, err error) {
    // TODO: Log print below commented for now due to updateCache() loop in pcache, spams screen
    //       Can uncomment when a proper logging library with different levels is implemented
    //log.Println("Looking for service with name", serviceName, "in registry cache")
    info, ok := rc.Get(serviceName)
    if !ok {
        log.Println("Not cached or expired, try querying registry-service")
        info, err = registry.GetServiceWithHostRouting(rc.ctx, rc.host, rc.routingDiscovery, serviceName)
        if err != nil {
            return info, err
        }
        rc.Add(serviceName, info)
	}
	return info, nil
}
