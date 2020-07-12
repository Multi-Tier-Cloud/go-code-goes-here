package rcache

import (
	"sync"
	"time"

	"github.com/Multi-Tier-Cloud/service-registry/registry"
)

type RegistryCache struct {
	ttl time.Duration // time-to-live
	data map[string]cacheEntry
	mux sync.RWMutex
}

type cacheEntry struct {
	Info registry.ServiceInfo
	Expiry time.Time
}

// ttl: time-to-live in seconds
func NewRegistryCache(ttl int) *RegistryCache {
	return &RegistryCache{
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

func (rc *RegistryCache) UpdateCache() {

}