package cli

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// nameCache keeps completion candidates for a short time so tab presses do not hit the API every time.
// The clock is injected; entries older than ttl are refetched.
type nameCache struct {
	clock model.Clock
	ttl   time.Duration

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	at    time.Time
	names []string
}

func newNameCache(clock model.Clock, ttl time.Duration) *nameCache {
	return &nameCache{clock: clock, ttl: ttl, entries: map[string]cacheEntry{}}
}

// get returns the cached names for key or loads them.
func (c *nameCache) get(key string, load func() ([]string, error)) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock.Now()
	if e, ok := c.entries[key]; ok && now.Sub(e.at) < c.ttl {
		return e.names, nil
	}
	names, err := load()
	if err != nil {
		return nil, err
	}
	c.entries[key] = cacheEntry{at: now, names: names}
	return names, nil
}

type completeFunc = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

// resourceNames returns the names of a resource through the cache. The key includes the controller URL.
func resourceNames(cmd *cobra.Command, env *Env, o *opts, resource string) ([]string, error) {
	c, conn, err := o.client(env)
	if err != nil {
		return nil, err
	}
	return env.cache.get(conn.URL+"|"+resource, func() ([]string, error) {
		k, err := lookupKind(resource)
		if err != nil {
			return nil, err
		}
		items, err := fetchAll(cmd, c, k, nil)
		if err != nil {
			return nil, err
		}
		key := "name"
		if resource == "instances" {
			key = "id"
		}
		names := make([]string, 0, len(items))
		for _, it := range items {
			if n, _ := it[key].(string); n != "" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return names, nil
	})
}

func filterPrefix(names []string, prefix string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// completeResource completes the names of one resource.
func completeResource(env *Env, o *opts, resource string) completeFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		names, err := resourceNames(cmd, env, o, resource)
		if err != nil {
			// Completion must never print errors into the user's line; no candidates is the honest answer.
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFirstArg applies f for the first positional argument only.
func completeFirstArg(f completeFunc) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return f(cmd, args, toComplete)
	}
}

// completeKindAndName completes the resource kind first, then the names of that kind.
func completeKindAndName(env *Env, o *opts) completeFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return filterPrefix(kindNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
		case 1:
			k, err := lookupKind(args[0])
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeResource(env, o, k.name)(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func fixed(values ...string) completeFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return filterPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeContexts(env *Env) completeFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		cfg, err := env.loadConfig()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var names []string
		for _, c := range cfg.Contexts {
			names = append(names, c.Name)
		}
		return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}
