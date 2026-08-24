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
		newAuthListCmd(d),
		newAuthSwitchCmd(d),
		newAuthPrintTokenCmd(d),
	)
	return cmd
}

func newAuthLoginCmd(d *deps) *cobra.Command {
	var device bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in interactively (loopback PKCE, or --use-device-code for headless)",
		Long: "Sign in as a user and cache the resulting Zentoris session for this account.\n\n" +
			"Default: a browser loopback-PKCE flow. On an SSH session or a headless host (no\n" +
			"local browser), zentoris automatically switches to the RFC 8628 device flow - print a\n" +
			"code, approve it in any browser. Pass --use-device-code to force the device flow.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if device || auth.IsHeadless() {
				return auth.RunDeviceLogin(c.Context(), d.cfg)
			}
			return auth.RunInteractiveLogin(c.Context(), d.cfg)
		},
	}
	cmd.Flags().BoolVar(&device, "use-device-code", false, "force the RFC 8628 device-code flow (no local browser; SSH/headless)")
	return cmd
}

func newAuthLogoutCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for the current account",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := auth.NewStore().Clear(d.cfg.Account); err != nil {
				return err
			}
			if err := auth.UnregisterLogout(d.cfg.Account); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Logged out account %q.\n", d.cfg.Account)
			return nil
		},
	}
}

func newAuthListCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the accounts you have logged in",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			infos, err := auth.ListAccounts()
			if err != nil {
				return err
			}
			if len(infos) == 0 {
				fmt.Fprintln(c.OutOrStdout(), "No accounts yet. Run `zentoris auth login`.")
				return nil
			}
			// A leading "*" marks the active default; other accounts need --account or `auth switch`.
			for _, in := range infos {
				marker := "  "
				if in.Active {
					marker = "* "
				}
				line := fmt.Sprintf("%s%s (%s)", marker, in.Name, in.Backend)
				if in.Subject != "" {
					line += " - " + in.Subject
				}
				if in.Expired {
					line += " [expired]"
				}
				fmt.Fprintln(c.OutOrStdout(), line)
			}
			return nil
		},
	}
}

func newAuthSwitchCmd(d *deps) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <account>",
		Short: "Set the active account used when --account is not given",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := auth.SwitchAccount(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Active account is now %q.\n", args[0])
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
			fmt.Fprintf(c.OutOrStdout(), "Active account: %s\n", d.cfg.Account)
			tok, src, err := d.resolver.Token(c.Context())
			if err != nil {
				fmt.Fprintf(c.OutOrStdout(), "Not authenticated: %v\n", err)
				return nil
			}
			fmt.Fprintf(c.OutOrStdout(), "Authenticated via %s (token %s...).\n", src, safePrefix(tok))
			// Name where the login credential is stored (keychain vs 0600 file) and, when we captured
			// one, which account it belongs to, so the user knows who they are acting as.
			if src == "login" {
				store := auth.NewStore()
				fmt.Fprintf(c.OutOrStdout(), "Credentials stored in: %s.\n", store.Backend(d.cfg.Account))
				if creds, _ := store.Load(d.cfg.Account); creds != nil && creds.Subject != "" {
					fmt.Fprintf(c.OutOrStdout(), "Account: %s\n", creds.Subject)
				}
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
