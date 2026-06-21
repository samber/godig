package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/hot"
)

// invoker is the seam between the MCP handler and the underlying operation
// source. *dispatch.Dispatcher satisfies it; the caching decorator wraps it.
type invoker interface {
	Invoke(ctx context.Context, name string, args map[string]any) (any, error)
}

// cachingInvoker memoises successful operation results in an in-memory LRU cache
// with a TTL. It only fronts the MCP server (a long-running process); the CLI
// invokes the dispatcher directly and is never cached. pkg.go.dev data is
// read-only and changes slowly, so repeated tool calls for the same package are
// served from memory until the entry expires.
type cachingInvoker struct {
	next  invoker
	cache *hot.HotCache[string, any]
}

// newCachingInvoker wraps next with an LRU+TTL cache of the given capacity.
func newCachingInvoker(next invoker, ttl time.Duration, size int) *cachingInvoker {
	cache := hot.NewHotCache[string, any](hot.LRU, size).
		WithTTL(ttl).
		WithJanitor().
		Build()
	slog.Info("mcp result cache enabled", "ttl", ttl, "size", size)
	// The janitor goroutine lives for the lifetime of the server process, which
	// blocks in ServeStdio/ServeHTTP until exit, so there is no StopJanitor call.
	return &cachingInvoker{next: next, cache: cache}
}

// Invoke returns a cached result when present, otherwise delegates and caches
// the result on success. Errors are never cached.
func (c *cachingInvoker) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	key := cacheKey(name, args)
	if v, found, _ := c.cache.Get(key); found {
		slog.Debug("mcp cache hit", "tool", name)
		return v, nil
	}
	v, err := c.next.Invoke(ctx, name, args)
	if err != nil {
		return nil, err
	}
	c.cache.Set(key, v)
	return v, nil
}

// cacheKey derives a deterministic key from the operation name and its
// arguments. Keys are sorted so map iteration order never affects the result,
// and values are written directly (no reflection / JSON encoding) so the key is
// cheap to build on every tool call.
//
// 0x1f (unit separator) delimits fields and 0x1e (record separator) splits each
// key from its value; both are control characters that never occur in the
// argument names or in pkg.go.dev paths, versions and queries, so distinct
// argument sets cannot collide on the same key.
func cacheKey(name string, args map[string]any) string {
	if len(args) == 0 {
		return name
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		b.WriteByte(0x1f)
		b.WriteString(k)
		b.WriteByte(0x1e)
		writeValue(&b, args[k])
	}
	return b.String()
}

// writeValue appends a stable textual form of an MCP/CLI argument value. The
// argument map only ever holds JSON scalars (string, bool, number) or CLI
// strings; the reflection-free default keeps any unexpected type from panicking.
func writeValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		b.WriteString(x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case nil:
		// distinct from the empty string so "absent" and "" do not collide
		b.WriteByte(0x00)
	default:
		// Unreachable for the current scalar-only argument schema; kept robust.
		fmt.Fprint(b, x)
	}
}
