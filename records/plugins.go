package records

import (
	"encoding/json"
	"fmt"
	"sync"
)

// a plugin has a single use lifecycle: once started, it may be stopped. once stopped,
// it may not be restarted.
type Plugin interface {
	// starts any requisite background tasks for the plugin, should return immediately
	Start()
	// stops any running background tasks for the plugin, should return immediately
	Stop()
	// returns a signal chan that's closed once the plugin has terminated
	Done() <-chan struct{}
}

type PluginFactory func(json.RawMessage) (Plugin, error)

var (
	pluginsLock sync.Mutex
	plugins     = map[string]PluginFactory{}
)

func RegisterPlugin(name string, f PluginFactory) error {
	if name == "" {
		return fmt.Errorf("illegal plugin name")
	}
	if f == nil {
		return fmt.Errorf("nil PluginFactory not allowed")
	}

	pluginsLock.Lock()
	defer pluginsLock.Unlock()

	if _, found := plugins[name]; found {
		return fmt.Errorf("PluginFactory for %q is already registered", name)
	}

	plugins[name] = f
	return nil
}

func NewPlugin(name string, c *Config) (Plugin, error) {
	raw, found := c.Plugins[name]
	if !found {
		return nil, fmt.Errorf("no configuration found for plugin name %q", name)
	}
	factory, found := func() (f PluginFactory, ok bool) {
		pluginsLock.Lock()
		defer pluginsLock.Unlock()
		f, ok = plugins[name]
		return
	}()
	if !found {
		return nil, fmt.Errorf("no PluginFactory registered for plugin name %q", name)
	}
	return factory(raw)
}
