package rcache

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

func (rc *RegistryCache) Add(serviceName string, info registry.ServiceInfo) {
	entry := cacheEntry{
		Info: info,
		Expiry: time.Now().Add(rc.ttl),
	}
	rc.mux.Lock()
	rc.data[serviceName] = entry
	rc.mux.Unlock()
}

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

func (rc *RegistryCache) Delete(serviceName string) {
	rc.mux.Lock()
	delete(rc.data, serviceName)
	rc.mux.Unlock()
}

// Try to get service info from cache
// If not in cache, request it from registry-service
func (rc *RegistryCache) GetOrRequestService(serviceName string) (info registry.ServiceInfo, err error) {
    log.Println("Looking for service with name", serviceName, "in registry cache")
    info, ok := rc.Get(serviceName)
    if !ok {
        log.Println("Not cached, try querying registry-service")
        info, err = registry.GetServiceWithHostRouting(rc.ctx, rc.host, rc.routingDiscovery, serviceName)
        if err != nil {
            return info, err
        }
        rc.Add(serviceName, info)
	}
	return info, nil
}
