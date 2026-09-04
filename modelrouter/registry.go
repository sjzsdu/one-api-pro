package modelrouter

import (
	"fmt"
	"strings"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]ModelRouter)
)

// Register makes a routing strategy available by name.
func Register(router ModelRouter) {
	if router == nil || strings.TrimSpace(router.Name()) == "" {
		return
	}
	registryMu.Lock()
	registry[strings.ToLower(strings.TrimSpace(router.Name()))] = router
	registryMu.Unlock()
}

// Get returns a registered routing strategy.
func Get(name string) (ModelRouter, bool) {
	registryMu.RLock()
	router, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	registryMu.RUnlock()
	return router, ok
}

// SetDefault selects the process-wide routing strategy.
func SetDefault(name string) error {
	router, ok := Get(name)
	if !ok {
		return fmt.Errorf("unknown model router strategy %q", name)
	}
	DefaultRouter = router
	return nil
}
