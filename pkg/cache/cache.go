/*
** Karpenter Provider OCI
**
** Copyright (c) 2026 Oracle and/or its affiliates.
** Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/
 */

package cache

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultTTL = time.Hour

	LongTTL = 24 * time.Hour
	// DefaultCleanupInterval triggers cache cleanup (lazy eviction) at this interval.
	DefaultCleanupInterval = 10 * time.Minute

	// UnavailableOfferingsTTL is the default duration an offering observed to be out of host
	// capacity is considered unavailable before Karpenter retries it. It is overridable via the
	// --unavailable-offerings-ttl-seconds option.
	UnavailableOfferingsTTL = 3 * time.Minute
	// UnavailableOfferingsCleanupInterval triggers cleanup of the unavailable-offerings cache.
	UnavailableOfferingsCleanupInterval = time.Minute
)

type LoaderFunc[T any] func(ctx context.Context, key string) (T, error)

type GetOrLoadCache[T any] struct {
	cache *cache.Cache
	locks sync.Map // map[string]*sync.Mutex
}

func NewGetOrLoadCache[T any](defaultExpiration, cleanupInterval time.Duration) *GetOrLoadCache[T] {
	return &GetOrLoadCache[T]{
		cache: cache.New(defaultExpiration, cleanupInterval),
	}
}

func NewDefaultGetOrLoadCache[T any]() *GetOrLoadCache[T] {
	return &GetOrLoadCache[T]{
		cache: cache.New(DefaultTTL, DefaultCleanupInterval),
	}
}

func (c *GetOrLoadCache[T]) GetOrLoad(ctx context.Context, key string,
	loader LoaderFunc[T]) (T, error) {
	t, found := c.getFromCache(ctx, key)
	if found {
		return t, nil
	}

	// Lock for this key to avoid duplicate loads
	muIface, _ := c.locks.LoadOrStore(key, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	defer c.locks.Delete(key)

	// Double-check cache after acquiring lock (with safe assertion)
	t, found = c.getFromCache(ctx, key)
	if found {
		return t, nil
	}

	// Load, store, return
	return c.load(ctx, key, loader)
}

func (c *GetOrLoadCache[T]) Evict(ctx context.Context, key string) {
	c.cache.Delete(key)
}

func (c *GetOrLoadCache[T]) getFromCache(ctx context.Context, key string) (T, bool) {
	// get from cache with safe type assertion
	if v, found := c.cache.Get(key); found {
		if typed, ok := v.(T); ok {
			log.FromContext(ctx).V(1).Info("serving from cache", "key", key)
			return typed, true
		}

		c.cache.Delete(key)
	}

	var zero T
	return zero, false
}

func (c *GetOrLoadCache[T]) load(ctx context.Context, key string, loader LoaderFunc[T]) (T, error) {
	// Load, store, return
	log.FromContext(ctx).V(1).Info("cache loading", "key", key)
	v, err := loader(ctx, key)
	if err == nil {
		c.cache.Set(key, v, cache.DefaultExpiration)
		return v, nil
	}

	var zero T
	return zero, err
}

func MakeCompositeKey(values ...string) string {
	return strings.Join(values, "|")
}
