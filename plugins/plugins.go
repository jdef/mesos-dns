package plugins

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mesosphere/mesos-dns/resolver"
)

// a plugin has a single use lifecycle: once started, it may be stopped. once stopped,
// it may not be restarted.
type Plugin interface {
	// starts any requisite background tasks for the plugin, should return immediately
	Start(*resolver.Resolver)
	// stops any running background tasks for the plugin, should return immediately
	Stop(*resolver.Resolver)
	// returns a signal chan that's closed once the plugin has terminated
	Done() <-chan struct{}
}

type Factory func(json.RawMessage) (Plugin, error)

var (
	pluginsLock sync.Mutex
	plugins     = map[string]Factory{}
)

func Register(name string, f Factory) error {
	if name == "" {
		return fmt.Errorf("illegal plugin name")
	}
	if f == nil {
		return fmt.Errorf("nil Factory not allowed")
	}

	pluginsLock.Lock()
	defer pluginsLock.Unlock()

	if _, found := plugins[name]; found {
		return fmt.Errorf("Factory for %q is already registered", name)
	}

	plugins[name] = f
	return nil
}

func New(name string, raw json.RawMessage) (Plugin, error) {
	factory, found := func() (f Factory, ok bool) {
		pluginsLock.Lock()
		defer pluginsLock.Unlock()
		f, ok = plugins[name]
		return
	}()
	if !found {
		return nil, fmt.Errorf("no Factory registered for plugin name %q", name)
	}
	return factory(raw)
}
