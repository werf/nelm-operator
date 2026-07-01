package source

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getNoneZeroDuration returns d unless it is the zero duration, in which case fallback is
// returned. It lets a per-source interval inherit the Release-level interval
// when it is not explicitly set.
func getNoneZeroDuration(d, fallback metav1.Duration) metav1.Duration {
	if d.Duration == 0 {
		return fallback
	}
	return d
}
