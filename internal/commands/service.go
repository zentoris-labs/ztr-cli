package commands

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newServiceCmd(d *deps) *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Inspect and update services"}
	cmd.AddCommand(newServiceListCmd(d), newServiceGetCmd(d), newServiceUpdateCmd(d))
	return cmd
}

func newServiceListCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List services",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var out any
			if err := d.api.Do(c.Context(), http.MethodGet, "/services", nil, "", &out); err != nil {
				return err
			}
			return render(c, out)
		},
	}
}

func newServiceGetCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <service-id>",
		Short: "Show one service",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var out any
			if err := d.api.Do(c.Context(), http.MethodGet, "/services/"+args[0], nil, "", &out); err != nil {
				return err
			}
			return render(c, out)
		},
	}
}

func newServiceUpdateCmd(d *deps) *cobra.Command {
	var (
		sets    []string
		release bool
		ifMatch string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "update <service-id>",
		Short: "Patch service variables and optionally cut a release",
		Long: "Update one or more service variables and, with --release, cut a new immutable\n" +
			"release that pins the current variable state. Built for CI, e.g.:\n\n" +
			"  zentoris service update svc_123 --set COMMIT_ID=$GITHUB_SHA --release\n\n" +
			"Authenticates with any configured credential source (see `zentoris auth status`).",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := args[0]
			vars, err := parseKV(sets)
			if err != nil {
				return err
			}
			patch := map[string]any{"variables": vars}

			if dryRun {
				fmt.Fprintln(c.OutOrStdout(), "DRY RUN")
				if len(vars) > 0 {
					fmt.Fprintf(c.OutOrStdout(), "  PATCH /services/%s  If-Match: %s\n    %v\n", id, ifMatchOrNone(ifMatch), vars)
				}
				if release {
					fmt.Fprintf(c.OutOrStdout(), "  POST  /services/%s/releases\n", id)
				}
				return nil
			}

			if len(vars) > 0 {
				if err := d.api.Do(c.Context(), http.MethodPatch, "/services/"+id, patch, ifMatch, nil); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "Updated %d variable(s) on service %s.\n", len(vars), id)
			}
			if release {
				var rel map[string]any
				if err := d.api.Do(c.Context(), http.MethodPost, "/services/"+id+"/releases", map[string]any{}, "", &rel); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "Release created: %v\n", rel["id"])
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&sets, "set", nil, "set a variable, KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&release, "release", false, "cut a new release after applying variables")
	cmd.Flags().StringVar(&ifMatch, "if-match", "", "If-Match ETag for optimistic concurrency; pass '*' to overwrite unconditionally (default: no precondition)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the requests instead of sending them")
	return cmd
}

// ifMatchOrNone labels an empty If-Match for dry-run output; an empty value sends no precondition.
func ifMatchOrNone(ifMatch string) string {
	if ifMatch == "" {
		return "(none)"
	}
	return ifMatch
}
