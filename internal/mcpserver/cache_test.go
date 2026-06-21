package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeInvoker records how many times Invoke is called and returns a configurable
// value/error.
type fakeInvoker struct {
	calls int
	val   any
	err   error
}

func (f *fakeInvoker) Invoke(context.Context, string, map[string]any) (any, error) {
	f.calls++
	return f.val, f.err
}

func TestCachingInvoker_CachesSuccess(t *testing.T) {
	t.Parallel()
	f := &fakeInvoker{val: "result"}
	c := newCachingInvoker(f, time.Minute, 10)

	args := map[string]any{"path": "github.com/samber/lo"}
	v1, err := c.Invoke(context.Background(), "package-info", args)
	require.NoError(t, err)
	v2, err := c.Invoke(context.Background(), "package-info", args)
	require.NoError(t, err)

	assert.Equal(t, "result", v1)
	assert.Equal(t, "result", v2)
	assert.Equal(t, 1, f.calls, "second identical call should be served from cache")
}

func TestCachingInvoker_DoesNotCacheErrors(t *testing.T) {
	t.Parallel()
	f := &fakeInvoker{err: errors.New("boom")}
	c := newCachingInvoker(f, time.Minute, 10)

	_, err := c.Invoke(context.Background(), "package-info", map[string]any{"path": "x"})
	require.Error(t, err)
	_, err = c.Invoke(context.Background(), "package-info", map[string]any{"path": "x"})
	require.Error(t, err)

	assert.Equal(t, 2, f.calls, "errors must not be cached")
}

func TestCachingInvoker_DistinctArgsDistinctEntries(t *testing.T) {
	t.Parallel()
	f := &fakeInvoker{val: "result"}
	c := newCachingInvoker(f, time.Minute, 10)

	_, _ = c.Invoke(context.Background(), "package-info", map[string]any{"path": "a"})
	_, _ = c.Invoke(context.Background(), "package-info", map[string]any{"path": "b"})
	_, _ = c.Invoke(context.Background(), "module-info", map[string]any{"path": "a"})

	assert.Equal(t, 3, f.calls, "different args or operation names must not collide")
}

func TestCacheKey_DeterministicRegardlessOfOrder(t *testing.T) {
	t.Parallel()
	a := map[string]any{"path": "p", "version": "v1.0.0", "limit": 10, "examples": true}
	b := map[string]any{"limit": 10, "examples": true, "version": "v1.0.0", "path": "p"}
	assert.Equal(t, cacheKey("overview", a), cacheKey("overview", b))
}

func TestCacheKey_ValuesAffectKey(t *testing.T) {
	t.Parallel()
	base := cacheKey("package-info", map[string]any{"path": "p"})
	assert.NotEqual(t, base, cacheKey("package-info", map[string]any{"path": "q"}))
	assert.NotEqual(t, base, cacheKey("module-info", map[string]any{"path": "p"}))
	// nil and empty string produce the same key
	assert.Equal(t,
		cacheKey("x", map[string]any{"k": nil}),
		cacheKey("x", map[string]any{"k": ""}),
	)
	// types with the same textual representation must not collide
	assert.Equal(t,
		cacheKey("x", map[string]any{"k": true}),
		cacheKey("x", map[string]any{"k": "true"}),
	)
	assert.Equal(t,
		cacheKey("x", map[string]any{"k": 10}),
		cacheKey("x", map[string]any{"k": "10"}),
	)
}
