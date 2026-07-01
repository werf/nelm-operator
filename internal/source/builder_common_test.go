package source

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNz(t *testing.T) {
	fallback := metav1.Duration{Duration: 5 * time.Minute}

	tests := []struct {
		name     string
		d        metav1.Duration
		fallback metav1.Duration
		want     metav1.Duration
	}{
		{
			name:     "zero falls back",
			d:        metav1.Duration{},
			fallback: fallback,
			want:     fallback,
		},
		{
			name:     "non-zero kept",
			d:        metav1.Duration{Duration: 2 * time.Minute},
			fallback: fallback,
			want:     metav1.Duration{Duration: 2 * time.Minute},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getNoneZeroDuration(tt.d, tt.fallback); got != tt.want {
				t.Errorf("nz() = %v, want %v", got, tt.want)
			}
		})
	}
}
