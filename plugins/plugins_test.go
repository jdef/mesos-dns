package plugins

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolver"
)

type FakePluginConfig struct {
	Foo int `json:"foo,omitempty"`
}

type fakePlugin struct {
	FakePluginConfig
	startFunc func(*resolver.Resolver)
	stopFunc  func(*resolver.Resolver)
	done      chan struct{}
	doneOnce  sync.Once
}

func (p *fakePlugin) Start(r *resolver.Resolver) {
	if p.startFunc != nil {
		p.startFunc(r)
	}
}

func (p *fakePlugin) Stop(r *resolver.Resolver) {
	p.doneOnce.Do(func() {
		close(p.done)
		if p.stopFunc != nil {
			p.stopFunc(r)
		}
	})
}

func (p *fakePlugin) Done() <-chan struct{} {
	return p.done
}

func TestPluginConfig(t *testing.T) {
	logging.SetupLogs()

	foo := 0
	Register("fake", Factory(func(raw json.RawMessage) (Plugin, error) {
		var c FakePluginConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("failed to unmarshal FakePluginConfig: %v", err)
		}
		return &fakePlugin{
			FakePluginConfig: c,
			done:             make(chan struct{}),
			startFunc: func(r *resolver.Resolver) {
				foo = c.Foo
			},
		}, nil
	}))
	json := `{"plugins":{"fake":{"Foo":123}}}`
	conf := records.ParseConfig([]byte(json), records.Config{Masters: []string{"bar"}, Email: "a@b.c"})
	raw, found := conf.Plugins["fake"]
	if !found {
		t.Fatalf("failed to locate fake plugin configuration")
	}

	p, err := New("fake", raw)
	if err != nil {
		t.Fatalf("failed to create fake plugin instance: %v", err)
	}

	p.Start(nil)
	if foo != 123 {
		t.Fatalf("plugin not started successfully")
	}
}
