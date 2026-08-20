package commands

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newReleaseCmd(d *deps) *cobra.Command {
	cmd := &cobra.Command{Use: "release", Short: "Manage service releases"}
	cmd.AddCommand(newReleaseCreateCmd(d), newReleaseListCmd(d))
	return cmd
}

func newReleaseCreateCmd(d *deps) *cobra.Command {
	var (
		service string
		commit  string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Cut a new release for a service",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if service == "" {
				return fmt.Errorf("--service is required")
			}
			body := map[string]any{}
			if commit != "" {
				body["commitId"] = commit
			}
			var out any
			if err := d.api.Do(c.Context(), http.MethodPost, "/services/"+service+"/releases", body, "", &out); err != nil {
				return err
			}
			return render(c, out)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "service id (required)")
	cmd.Flags().StringVar(&commit, "commit", "", "commit id to pin (optional)")
	return cmd
}

func newReleaseListCmd(d *deps) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List releases for a service",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if service == "" {
				return fmt.Errorf("--service is required")
			}
			var out any
			if err := d.api.Do(c.Context(), http.MethodGet, "/services/"+service+"/releases", nil, "", &out); err != nil {
				return err
			}
			return render(c, out)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "service id (required)")
	return cmd
}
