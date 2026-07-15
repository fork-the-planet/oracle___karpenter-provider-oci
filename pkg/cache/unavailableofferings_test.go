/*
** Karpenter Provider OCI
**
** Copyright (c) 2026 Oracle and/or its affiliates.
** Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/
 */

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestUnavailableOfferings_MarkAndIsUnavailable(t *testing.T) {
	ctx := context.Background()
	u := NewUnavailableOfferings(UnavailableOfferingsTTL)

	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))

	u.MarkUnavailable(ctx, "VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", "")
	assert.True(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))

	// distinct capacity type, zone and shape are unaffected.
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "on-demand", ""))
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-2", "spot", ""))
	assert.False(t, u.IsUnavailable("VM.Standard.E4.Flex", nil, nil, "AD-1", "spot", ""))
}

func TestUnavailableOfferings_ScopedByFlexConfig(t *testing.T) {
	ctx := context.Background()
	u := NewUnavailableOfferings(UnavailableOfferingsTTL)

	const (
		shape = "VM.Standard.E4.Flex"
		zone  = "AD-1"
		capOD = "on-demand"
	)

	// Mark only the 2 OCPU / 16 GB config of a flexible shape as unavailable.
	u.MarkUnavailable(ctx, shape, lo.ToPtr(float32(2)), lo.ToPtr(float32(16)), zone, capOD, "")
	assert.True(t, u.IsUnavailable(shape, lo.ToPtr(float32(2)), lo.ToPtr(float32(16)), zone, capOD, ""))

	// Other CPU/memory configurations of the same shape/zone/capacity-type stay available.
	assert.False(t, u.IsUnavailable(shape, lo.ToPtr(float32(4)), lo.ToPtr(float32(16)), zone, capOD, ""),
		"a different ocpu config must not be suppressed")
	assert.False(t, u.IsUnavailable(shape, lo.ToPtr(float32(2)), lo.ToPtr(float32(32)), zone, capOD, ""),
		"a different memory config must not be suppressed")
	// A fixed-shape (nil config) lookup for the same shape is also unaffected.
	assert.False(t, u.IsUnavailable(shape, nil, nil, zone, capOD, ""),
		"a nil (fixed) config must not match a flex-config entry")
}

func TestUnavailableOfferings_ScopedByCompartment(t *testing.T) {
	ctx := context.Background()
	u := NewUnavailableOfferings(UnavailableOfferingsTTL)

	const (
		shape        = "VM.Standard.E4.Flex"
		zone         = "AD-1"
		capOD        = "on-demand"
		compartmentA = "ocid1.compartment.oc1..aaaa"
		compartmentB = "ocid1.compartment.oc1..bbbb"
	)

	// A compartment-scoped (QuotaExceeded) entry only suppresses that compartment.
	u.MarkUnavailable(ctx, shape, nil, nil, zone, capOD, compartmentA)
	assert.True(t, u.IsUnavailable(shape, nil, nil, zone, capOD, compartmentA))
	assert.False(t, u.IsUnavailable(shape, nil, nil, zone, capOD, compartmentB),
		"another compartment must not be suppressed by a compartment-scoped entry")
	assert.False(t, u.IsUnavailable(shape, nil, nil, zone, capOD, ""),
		"the tenancy-wide scope must not be suppressed by a compartment-scoped entry")

	// A tenancy-wide (host capacity / LimitExceeded) entry is independent of compartment scope.
	u.MarkUnavailable(ctx, shape, nil, nil, zone, "spot", "")
	assert.True(t, u.IsUnavailable(shape, nil, nil, zone, "spot", ""))
	assert.False(t, u.IsUnavailable(shape, nil, nil, zone, "spot", compartmentA),
		"a compartment-scoped lookup must not match a tenancy-wide entry")
}

func TestUnavailableOfferings_Flush(t *testing.T) {
	ctx := context.Background()
	u := NewUnavailableOfferings(UnavailableOfferingsTTL)

	u.MarkUnavailable(ctx, "VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", "")
	u.MarkUnavailable(ctx, "VM.Standard.E5.Flex", nil, nil, "AD-2", "on-demand", "")
	assert.True(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))
	assert.True(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-2", "on-demand", ""))

	u.Flush()
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-2", "on-demand", ""))
}

func TestUnavailableOfferings_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	// a short, configurable TTL is honored by MarkUnavailable so entries expire quickly.
	u := NewUnavailableOfferings(20 * time.Millisecond)

	u.MarkUnavailable(ctx, "VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", "")
	assert.True(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))

	time.Sleep(40 * time.Millisecond)
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""))
}

func TestUnavailableOfferings_DefaultsTTLWhenNegative(t *testing.T) {
	// A negative ttl is invalid and falls back to the default; an explicit ttl is honored as-is.
	assert.Equal(t, UnavailableOfferingsTTL, NewUnavailableOfferings(-time.Second).ttl)
	assert.False(t, NewUnavailableOfferings(-time.Second).disabled)
	assert.Equal(t, time.Minute, NewUnavailableOfferings(time.Minute).ttl)
	assert.False(t, NewUnavailableOfferings(time.Minute).disabled)
}

// A ttl of 0 disables the cache: offerings are never recorded and IsUnavailable always reports
// available, so Karpenter never routes around capacity-exhausted offerings.
func TestUnavailableOfferings_DisabledWhenZeroTTL(t *testing.T) {
	ctx := context.Background()
	u := NewUnavailableOfferings(0)

	assert.True(t, u.disabled)

	u.MarkUnavailable(ctx, "VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", "")
	assert.False(t, u.IsUnavailable("VM.Standard.E5.Flex", nil, nil, "AD-1", "spot", ""),
		"a disabled cache must never report an offering as unavailable")

	// Flush must be a safe no-op on a disabled cache.
	assert.NotPanics(t, u.Flush)
}

func TestUnavailableOfferings_Key(t *testing.T) {
	u := NewUnavailableOfferings(UnavailableOfferingsTTL)

	// Fixed shapes render empty ocpu/memory segments; tenancy-wide entries have an empty
	// trailing compartment segment.
	assert.Equal(t, "on-demand:VM.Standard.E3.8:::AD-1:", u.key("VM.Standard.E3.8", nil, nil, "AD-1", "on-demand", ""))
	// Flexible shapes include the CPU/memory configuration.
	assert.Equal(t, "spot:VM.Standard.E4.Flex:2:16:AD-1:",
		u.key("VM.Standard.E4.Flex", lo.ToPtr(float32(2)), lo.ToPtr(float32(16)), "AD-1", "spot", ""))
	// Fractional values are preserved without trailing zeros.
	assert.Equal(t, "on-demand:VM.Standard.E4.Flex:1.5:24:AD-2:",
		u.key("VM.Standard.E4.Flex", lo.ToPtr(float32(1.5)), lo.ToPtr(float32(24)), "AD-2", "on-demand", ""))
	// Compartment-scoped entries include the compartment in the trailing segment.
	assert.Equal(t, "on-demand:VM.Standard.E3.8:::AD-1:ocid1.compartment.oc1..aaaa",
		u.key("VM.Standard.E3.8", nil, nil, "AD-1", "on-demand", "ocid1.compartment.oc1..aaaa"))
}
