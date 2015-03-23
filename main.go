package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/plugins"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolver"

	"github.com/miekg/dns"
)

var (
	cjson = flag.String("config", "config.json", "location of configuration file (json)")
)

type context struct {
	resolver resolver.Resolver
	filters  plugins.FilterSet
}

func (c *context) Resolver() *resolver.Resolver {
	return &c.resolver
}

func (c *context) Done() <-chan struct{} {
	// clients that use this chan will block forever
	return nil
}

func (c *context) AddFilter(f plugins.Filter) {
	if f != nil {
		c.filters = append(c.filters, f)
	}
}

func (c *context) newHandler() dns.Handler {
	return c.filters.Handler(dns.HandlerFunc(c.resolver.HandleMesos))
}

func (c *context) initialize() {
	c.resolver.Config = records.SetConfig(*cjson)
	for _, pconfig := range c.resolver.Config.Plugins {
		if pconfig.Name == "" {
			logging.Error.Printf("failed to register plugin with empty name")
			continue
		}
		plugin, err := plugins.New(pconfig.Name, pconfig.Settings)
		if err != nil {
			logging.Error.Printf("failed to create plugin: %v", err)
			continue
		}
		logging.Verbose.Printf("starting plugin %q", pconfig.Name)
		plugin.Start(c)
		go func() {
			select {
			case <-plugin.Done():
				logging.Verbose.Printf("plugin %q terminated", pconfig.Name)
			}
		}()
	}

	// reload the first time
	c.resolver.Reload()
	ticker := time.NewTicker(time.Second * time.Duration(c.resolver.Config.RefreshSeconds))
	go func() {
		for _ = range ticker.C {
			c.resolver.Reload()
			logging.PrintCurLog()
		}
	}()
}

func main() {
	ctx := &context{}

	versionFlag := false

	flag.BoolVar(&logging.VerboseFlag, "v", false, "verbose logging")
	flag.BoolVar(&logging.VeryVerboseFlag, "vv", false, "very verbose logging")
	flag.BoolVar(&versionFlag, "version", false, "output the version")
	flag.Parse()

	if versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}

	logging.SetupLogs()
	ctx.initialize()

	// handle for everything in this domain...
	ch := ctx.newHandler()
	dns.HandleFunc(ctx.resolver.Config.Domain+".", panicRecover(ch))
	dns.HandleFunc(".", panicRecover(ch))

	go ctx.resolver.Serve("tcp")
	go ctx.resolver.Serve("udp")

	// never returns
	select {}
}

// panicRecover catches any panics from the resolvers and sets an error
// code of server failure
func panicRecover(handler dns.Handler) func(w dns.ResponseWriter, r *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		defer func() {
			if rec := recover(); rec != nil {
				m := new(dns.Msg)
				m.SetReply(r)
				m.SetRcode(r, 2)
				_ = w.WriteMsg(m)
				logging.Error.Println(rec)
			}
		}()
		handler.ServeDNS(w, r)
	}
}
