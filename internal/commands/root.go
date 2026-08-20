// Package commands assembles the zentoris cobra command tree.
package commands

import (
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
	f.StringVar(&cfg.APIBase, "api", cfg.APIBase, "Zentoris main API base URL")
	f.StringVar(&cfg.AuthBase, "auth-url", cfg.AuthBase, "Zentoris auth/OP base URL")
	// Base domain the service URLs derive from (<svc>.api.<domain>). The default reaches the hosted
	// platform; point it at another deployment's base domain. Hidden: most users override the full
	// URLs (--api / --auth-url) instead.
	f.StringVar(&cfg.Domain, "domain", cfg.Domain, "base domain for Zentoris service URLs (env ZENTORIS_DOMAIN)")
	_ = f.MarkHidden("domain")
	f.StringVar(&cfg.Tenant, "tenant", cfg.Tenant, "tenant id for token endpoints")
	f.StringVar(&cfg.Token, "token", cfg.Token, "explicit bearer token or PAT (env ZENTORIS_TOKEN)")
	f.StringVarP(&cfg.Output, "output", "o", cfg.Output, "output format: table|json")
	f.StringVar(&cfg.Profile, "profile", cfg.Profile, "named credential profile (env ZENTORIS_PROFILE)")
	f.BoolVar(&cfg.Insecure, "insecure", cfg.Insecure, "skip TLS verification for self-signed local dev")
	f.StringVar(&cfg.Resource, "resource", cfg.Resource, "RFC 8707 resource indicator / target API audience")

	// After flags parse, re-derive the base URLs from --domain unless a URL was pinned directly
	// (an explicit --api/--auth-url/--insecure flag always wins). This runs in OnInitialize, which
	// cobra invokes for the executed command regardless of any subcommand's own PersistentPreRunE,
	// so domain resolution can never be silently shadowed; the root hook surfaces its error.
	var resolveErr error
	cobra.OnInitialize(func() {
		resolveErr = cfg.ApplyDomain(f.Changed("api"), f.Changed("auth-url"), f.Changed("insecure"))
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
