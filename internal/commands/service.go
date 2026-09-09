package commands

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newServiceCmd(d *deps) *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Inspect and publish services"}
	cmd.AddCommand(newServiceListCmd(d), newServiceGetCmd(d), newServicePublishCmd(d))
	return cmd
}

func newServiceListCmd(d *deps) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List an organization's services",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if orgID == "" {
				return fmt.Errorf("--org is required")
			}
			var out any
			path := "/services?organizationId=" + url.QueryEscape(orgID)
			if err := d.api.Do(c.Context(), http.MethodGet, path, nil, "", &out); err != nil {
				return err
			}
			return render(c, out)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "organization id to list (required)")
	return cmd
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
