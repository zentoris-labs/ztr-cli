package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zentoris-labs/ztr-cli/internal/auth"
)

func newAuthCmd(d *deps) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage Zentoris credentials"}
	cmd.AddCommand(
		newAuthLoginCmd(d),
		newAuthLogoutCmd(d),
		newAuthStatusCmd(d),
		newAuthPrintTokenCmd(d),
	)
	return cmd
}

func newAuthLoginCmd(d *deps) *cobra.Command {
	var device bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in interactively (loopback PKCE, or --device for headless)",
		Long: "Sign in as a user and cache the resulting Zentoris session for this profile.\n\n" +
			"Default: a browser loopback-PKCE flow. On an SSH session or a headless host (no\n" +
			"local browser), zentoris automatically switches to the RFC 8628 device flow - print a\n" +
			"code, approve it in any browser. Pass --device to force the device flow.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if device || auth.IsHeadless() {
				return auth.RunDeviceLogin(c.Context(), d.cfg)
			}
			return auth.RunInteractiveLogin(c.Context(), d.cfg)
		},
	}
	cmd.Flags().BoolVar(&device, "device", false, "use the RFC 8628 device flow (no local browser; SSH/headless)")
	return cmd
}

func newAuthLogoutCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for the current profile",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := auth.NewStore().Clear(d.cfg.Profile); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Logged out profile %q.\n", d.cfg.Profile)
			return nil
		},
	}
}

func newAuthStatusCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which credential source zentoris would use",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			tok, src, err := d.resolver.Token(c.Context())
			if err != nil {
				fmt.Fprintf(c.OutOrStdout(), "Not authenticated: %v\n", err)
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "Authenticated via %s (token %s...).\n", src, safePrefix(tok))
			// Name where the login credential is stored (keychain vs 0600 file) so the user knows.
			if src == "login" {
				fmt.Fprintf(c.OutOrStdout(), "Credentials stored in: %s.\n", auth.NewStore().Backend(d.cfg.Profile))
			}
			return nil
		},
	}
}

func newAuthPrintTokenCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "print-access-token",
		Short: "Print the resolved bearer token (for scripting)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			tok, _, err := d.resolver.Token(c.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), tok)
			return nil
		},
	}
}
