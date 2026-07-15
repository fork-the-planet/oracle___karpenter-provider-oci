/*
** Karpenter Provider OCI
**
** Copyright (c) 2026 Oracle and/or its affiliates.
** Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/
 */

package instance

import "github.com/oracle/karpenter-provider-oci/pkg/oci"

type NoCapacityError struct {
}

func (e NoCapacityError) Error() string {
	return "No Capacity"
}

func IsNoCapacityError(err error) bool {
	if err == nil {
		return false
	}

	if _, ok := err.(NoCapacityError); ok {
		return true
	}

	return oci.IsOutOfHostCapacity(err)
}

// IsSkippableLaunchError returns true for launch failures that should be skipped
// in favor of trying another shape/offering: host-capacity exhaustion and
// service-limit/compartment-quota exhaustion. When every candidate shape fails
// with a skippable error, the caller surfaces an InsufficientCapacityError so
// core Karpenter deletes and reschedules the NodeClaim onto another offering or
// NodePool.
func IsSkippableLaunchError(err error) bool {
	return IsNoCapacityError(err) || oci.IsServiceLimitExceeded(err)
}
