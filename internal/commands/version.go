package commands

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// devVersion is the placeholder used for an un-stamped local build.
const devVersion = "0.0.0+dev"

// version is overridable at build time via -ldflags "-X ...commands.version=v1.2.3" (GoReleaser
// stamps release binaries this way). When it is not stamped - notably a plain
// `go install .../cmd/zentoris@vX.Y.Z` - init falls back to the module version the Go toolchain
// embeds in the binary, so the CLI still reports a real version instead of the dev placeholder.
var version = devVersion

func init() { version = resolveVersion(version, debug.ReadBuildInfo) }

// resolveVersion prefers an ldflags-injected version; failing that, the module version from the
// build info (ignoring the "(devel)"/empty placeholders a non-release build carries); failing
// that, the dev placeholder. Kept as a pure function, with readBuildInfo injected, so it is testable.
func resolveVersion(ldflag string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if ldflag != devVersion {
		return ldflag
	}
	if info, ok := readBuildInfo(); ok && info != nil {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return devVersion
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the zentoris version",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(c.OutOrStdout(), "zentoris %s (%s %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}
