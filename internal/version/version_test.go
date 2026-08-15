package version

import (
	"testing"
)

func TestIsRelease(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		want     bool
	}{
		// Clean release versions
		{"clean tag v1.0.0", "v1.0.0", true},
		{"clean tag v1.2.3", "v1.2.3", true},
		{"clean tag v0.0.1", "v0.0.1", true},
		{"clean tag with prerelease v1.0.0-alpha", "v1.0.0-alpha", true},
		{"clean tag with build metadata v1.0.0+build", "v1.0.0+build", true},

		// Pseudo-versions (not releases)
		{"pseudo-version with timestamp", "v0.0.0-20240815123456-abcdef123456", false},
		{"pseudo-version with commit", "v1.2.3-0.20240815123456-abcdef123456", false},

		// Dirty working tree (not a release)
		{"dirty suffix v1.0.0+dirty", "v1.0.0+dirty", false},
		{"dirty pseudo-version", "v0.0.0-20240815123456-abcdef123456+dirty", false},

		// Development versions (not releases)
		{"devel", "(devel)", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRelease(tt.version)
			if got != tt.want {
				t.Errorf("isRelease(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
