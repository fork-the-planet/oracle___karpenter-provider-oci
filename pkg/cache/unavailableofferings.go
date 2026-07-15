/*
** Karpenter Provider OCI
**
** Copyright (c) 2026 Oracle and/or its affiliates.
** Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/
 */

package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/patrickmn/go-cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// UnavailableOfferings stores offerings that recently failed to launch because of host-capacity
// exhaustion. Offerings present in the cache are treated as unavailable (Available=false) when
// instance types are listed, so the scheduler routes around them (including spot->on-demand and
// cross-NodePool fallback) until the entry's TTL expires.
type UnavailableOfferings struct {
	cache *cache.Cache
	ttl   time.Duration
	// disabled reports whether the feature is turned off (ttl of 0). When disabled, MarkUnavailable
	// is a no-op and IsUnavailable always returns false, so Karpenter never routes around
	// capacity-exhausted offerings.
	disabled bool
}

// NewUnavailableOfferings creates the cache with the given TTL for how long an offering observed to
// be out of host capacity stays unavailable before Karpenter retries it.
//
// A ttl of 0 disables the feature entirely: offerings are never recorded as unavailable and
// IsUnavailable always returns false. A negative ttl is treated as invalid and falls back to
// UnavailableOfferingsTTL.
func NewUnavailableOfferings(ttl time.Duration) *UnavailableOfferings {
	if ttl == 0 {
		return &UnavailableOfferings{disabled: true}
	}
	if ttl < 0 {
		ttl = UnavailableOfferingsTTL
	}
	return &UnavailableOfferings{
		cache: cache.New(ttl, UnavailableOfferingsCleanupInterval),
		ttl:   ttl,
	}
}

// MarkUnavailable records the given offering as unavailable for the configured TTL. Calling it
// again for an already-cached offering refreshes the TTL. For flexible shapes, ocpu and memoryInGbs
// scope the entry to a specific CPU/memory configuration so that a failure for one config does not
// suppress every other config of the same shape; they are nil for fixed shapes.
//
// compartment scopes the entry to a single compartment. It should be set for compartment-scoped
// failures (OCI QuotaExceeded, an administrator-defined quota for a specific compartment and
// resource) so the offering is not suppressed for NodeClasses launching into other compartments.
// It must be empty for failures that apply regardless of compartment (host-capacity exhaustion and
// tenancy-scoped LimitExceeded service limits).
func (u *UnavailableOfferings) MarkUnavailable(ctx context.Context, shape string, ocpu, memoryInGbs *float32,
	zone, capacityType, compartment string) {
	if u.disabled {
		return
	}
	log.FromContext(ctx).WithValues(
		"shape", shape,
		"ocpu", flexResourceLogValue(ocpu),
		"memory-in-gbs", flexResourceLogValue(memoryInGbs),
		"zone", zone,
		"capacity-type", capacityType,
		"compartment", compartmentLogValue(compartment),
		"ttl", u.ttl,
	).Info("marking offering as unavailable")
	u.cache.SetDefault(u.key(shape, ocpu, memoryInGbs, zone, capacityType, compartment), struct{}{})
}

// IsUnavailable returns true if the offering is currently cached as unavailable for the given
// compartment scope. Pass an empty compartment to check tenancy-wide entries (host capacity /
// LimitExceeded); pass the target compartment to check compartment-scoped entries (QuotaExceeded).
func (u *UnavailableOfferings) IsUnavailable(shape string, ocpu, memoryInGbs *float32,
	zone, capacityType, compartment string) bool {
	if u.disabled {
		return false
	}
	_, found := u.cache.Get(u.key(shape, ocpu, memoryInGbs, zone, capacityType, compartment))
	return found
}

func (u *UnavailableOfferings) Flush() {
	if u.disabled {
		return
	}
	u.cache.Flush()
}

// key returns the cache key for an offering. Format:
// <capacityType>:<shape>:<ocpu>:<memoryInGbs>:<zone>:<compartment>. The ocpu and memoryInGbs fields
// distinguish flexible-shape CPU/memory configurations and are empty for fixed shapes. compartment
// is empty for tenancy-wide entries and set for compartment-scoped (QuotaExceeded) entries.
func (u *UnavailableOfferings) key(shape string, ocpu, memoryInGbs *float32,
	zone, capacityType, compartment string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", capacityType, shape,
		flexResourceKeyValue(ocpu), flexResourceKeyValue(memoryInGbs), zone, compartment)
}

// flexResourceKeyValue renders a flexible-shape resource quantity for use in the cache key,
// returning an empty string when the quantity is unset (fixed shapes).
func flexResourceKeyValue(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', -1, 32)
}

func flexResourceLogValue(v *float32) string {
	if v == nil {
		return "<nil>"
	}
	return flexResourceKeyValue(v)
}

// compartmentLogValue renders the compartment scope for logging, distinguishing tenancy-wide
// entries (empty compartment) from compartment-scoped entries.
func compartmentLogValue(compartment string) string {
	if compartment == "" {
		return "<tenancy-wide>"
	}
	return compartment
}
