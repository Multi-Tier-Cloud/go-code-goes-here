package scache

import (
	"sync"
	"time"

	"github.com/Multi-Tier-Cloud/service-registry/registry"
)

type ServiceCache struct {
	ttl time.Duration // time-to-live
	data map[string]cacheEntry
	mux sync.RWMutex
}

type cacheEntry struct {
	Info registry.ServiceInfo
	Expiry time.Time
}

func NewServiceCache(ttl time.Duration) *ServiceCache {
	return &ServiceCache{
		ttl: ttl,
		data: make(map[string]cacheEntry),
	}
}

func (sc *ServiceCache) Add(serviceName string, info registry.ServiceInfo) {
	entry := cacheEntry{
		Info: info,
		Expiry: time.Now().Add(sc.ttl),
	}
	sc.mux.Lock()
	sc.data[serviceName] = entry
	sc.mux.Unlock()
}

func (sc *ServiceCache) Get(serviceName string) (info registry.ServiceInfo, ok bool) {
	sc.mux.RLock()
	entry, ok := sc.data[serviceName]
	sc.mux.RUnlock()
	if !ok {
		return info, false
	}
	if time.Now().After(entry.Expiry) {
		// Expired cache entry
		return info, false
	}
	return entry.Info, true
}

func (sc *ServiceCache) Delete(serviceName string) {
	sc.mux.Lock()
	delete(sc.data, serviceName)
	sc.mux.Unlock()
}

func (sc *ServiceCache) UpdateCache() {

}