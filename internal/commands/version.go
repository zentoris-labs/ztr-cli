package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags "-X ...commands.version=v1.2.3".
var version = "0.0.0+dev"

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
