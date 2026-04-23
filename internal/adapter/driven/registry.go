package driven

import (
	"fmt"
	"sort"
	"sync"

	"github.com/dpopsuev/conty/internal/adapter/driven/cache"
	"github.com/dpopsuev/conty/internal/config"
	"github.com/dpopsuev/conty/internal/port/driven"
)

type Factory func(name string, backend config.Backend) (driven.CIAdapter, error)

type entry struct {
	name     string
	priority int
	factory  Factory
}

var (
	mu       sync.Mutex
	registry []entry
)

func Register(name string, priority int, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, entry{name: name, priority: priority, factory: factory})
	sort.Slice(registry, func(i, j int) bool {
		return registry[i].priority > registry[j].priority
	})
}

func Available() []string {
	mu.Lock()
	defer mu.Unlock()
	names := make([]string, len(registry))
	for i, e := range registry {
		names[i] = e.name
	}
	return names
}

func CreateFromConfig(cfg *config.Config) (adapters []driven.CIAdapter, warnings []string) {
	mu.Lock()
	entries := make([]entry, len(registry))
	copy(entries, registry)
	mu.Unlock()

	for name, backend := range cfg.Backends {
		backendType := backend.ResolveType(name)
		var found bool
		for _, e := range entries {
			if e.name != backendType {
				continue
			}
			found = true
			adapter, err := e.factory(name, backend)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", name, err))
				break
			}
			if adapter != nil {
				adapters = append(adapters, cache.New(adapter))
			}
			break
		}
		if !found {
			warnings = append(warnings, fmt.Sprintf("unknown backend type %q for %q", backendType, name))
		}
	}
	return adapters, warnings
}
