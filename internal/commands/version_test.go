package commands

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	buildInfo := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	noBuildInfo := func() (*debug.BuildInfo, bool) { return nil, false }

	cases := []struct {
		name          string
		ldflag        string
		readBuildInfo func() (*debug.BuildInfo, bool)
		want          string
	}{
		{"ldflags stamped wins", "v1.2.3", buildInfo("v9.9.9"), "v1.2.3"},
		{"go install module version", devVersion, buildInfo("v0.1.0"), "v0.1.0"},
		{"ignore (devel) placeholder", devVersion, buildInfo("(devel)"), devVersion},
		{"ignore empty module version", devVersion, buildInfo(""), devVersion},
		{"no build info", devVersion, noBuildInfo, devVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.ldflag, tc.readBuildInfo); got != tc.want {
				t.Errorf("resolveVersion(%q, ...) = %q, want %q", tc.ldflag, got, tc.want)
			}
		})
	}
}
