package modelrouter

import (
	"fmt"
	"sync"
)

type RouterFactory func() ModelRouter

var (
	registryMu sync.RWMutex
	registry   = make(map[string]RouterFactory)
)

func Register(name string, factory RouterFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

func Get(name string) (ModelRouter, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("model router strategy not found: %s", name)
	}
	return factory(), nil
}

func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
