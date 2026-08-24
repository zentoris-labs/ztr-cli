// Package commands assembles the zentoris cobra command tree.
package commands

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/zentoris-labs/ztr-cli/internal/api"
	"github.com/zentoris-labs/ztr-cli/internal/auth"
	"github.com/zentoris-labs/ztr-cli/internal/config"
)

// deps carries the shared, lazily-read dependencies down to each command.
type deps struct {
	cfg      *config.Config
	resolver *auth.Resolver
	api      *api.Client
}

// NewRootCmd builds the zentoris command tree.
func NewRootCmd() *cobra.Command {
	cfg := config.Load()
	resolver := auth.DefaultChain(cfg)
	d := &deps{cfg: cfg, resolver: resolver, api: api.New(cfg, resolver, version)}

	root := &cobra.Command{
		Use:   "zentoris",
		Short: "Zentoris platform CLI",
		Long: "zentoris drives the Zentoris platform API: update services, cut releases, and manage\n" +
			"auth from your terminal or CI pipeline.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	f := root.PersistentFlags()
	// Base domain the service URLs derive from (<svc>.api.<domain>). This is the single endpoint
	// knob: the default reaches the hosted platform; point it at another deployment's base domain.
	f.StringVar(&cfg.Domain, "domain", cfg.Domain, "base domain for Zentoris service URLs (env ZENTORIS_DOMAIN)")
	f.StringVar(&cfg.Token, "token", cfg.Token, "explicit bearer token or PAT (env ZENTORIS_TOKEN)")
	f.StringVar(&cfg.Account, "account", cfg.Account, "named account / login to act as (env ZENTORIS_ACCOUNT)")
	f.BoolVar(&cfg.Insecure, "insecure", cfg.Insecure, "skip TLS verification for self-signed local dev")

	// After flags parse, re-derive the base URLs from --domain; an explicit --insecure still wins
	// over the derived TLS default. This runs in OnInitialize, which cobra invokes for the executed
	// command regardless of any subcommand's own PersistentPreRunE, so domain resolution can never
	// be silently shadowed; the root hook surfaces its error.
	var resolveErr error
	cobra.OnInitialize(func() {
		resolveErr = cfg.ApplyDomain(f.Changed("insecure"))
		// Resolve the active account when the caller pinned neither --account nor ZENTORIS_ACCOUNT:
		// fall back to the account last selected by `auth switch`, else "default".
		if !f.Changed("account") && os.Getenv("ZENTORIS_ACCOUNT") == "" {
			if active := auth.ActiveAccount(); active != "" {
				cfg.Account = active
			} else {
				cfg.Account = "default"
			}
		}
	})
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		return resolveErr
	}

	root.AddCommand(
		newAuthCmd(d),
		newServiceCmd(d),
		newReleaseCmd(d),
		newVersionCmd(),
	)
	return root
}
