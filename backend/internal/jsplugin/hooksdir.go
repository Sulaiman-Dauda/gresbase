package jsplugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// hooksDirPrefix namespaces plugins loaded from the hooks directory so they
// can be reloaded without touching dashboard-managed plugins.
const hooksDirPrefix = "gb_hooks:"

// LoadHooksDir loads every *.js file in dir (sorted by name) as a file-backed
// hook plugin. A missing directory is not an error — it simply means the
// project has no file hooks. Files that fail to compile are skipped with a
// warning so one broken hook cannot take the others down.
func (r *Runtime) LoadHooksDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	count := 0
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			log.Warn().Err(err).Str("hook", name).Msg("Failed to read JS hook file")
			continue
		}
		if _, err := r.LoadPlugin(hooksDirPrefix+name, name, string(data), 0); err != nil {
			log.Warn().Err(err).Str("hook", name).Msg("Failed to load JS hook file")
			continue
		}
		count++
	}
	return count, nil
}

// UnloadHooksDir removes all file-backed hook plugins.
func (r *Runtime) UnloadHooksDir() {
	for _, p := range r.ListPlugins() {
		if id, _ := p["id"].(string); strings.HasPrefix(id, hooksDirPrefix) {
			r.UnloadPlugin(id)
		}
	}
}

// WatchHooksDir polls dir and hot-reloads the file hooks whenever a .js file
// is added, removed, or modified. Polling keeps the binary dependency-free;
// the interval is coarse enough to be negligible. Blocks until ctx is done —
// run in a goroutine.
func (r *Runtime) WatchHooksDir(ctx context.Context, dir string, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	last := hooksDirSignature(dir)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sig := hooksDirSignature(dir)
			if sig == last {
				continue
			}
			last = sig
			r.UnloadHooksDir()
			n, err := r.LoadHooksDir(dir)
			if err != nil {
				log.Warn().Err(err).Str("dir", dir).Msg("JS hooks reload failed")
				continue
			}
			log.Info().Int("hooks", n).Str("dir", dir).Msg("JS hooks reloaded")
		}
	}
}

func hooksDirSignature(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s|%d|%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}
