package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/zentoris-labs/ztr-cli/internal/catalog"
)

// publishedVersion is one line of the command's JSON result: what was published, and where.
type publishedVersion struct {
	Service       string `json:"service"`
	ServiceID     string `json:"serviceId"`
	Track         string `json:"track"`
	VersionID     string `json:"versionId"`
	VersionNumber int    `json:"versionNumber"`
}

func newServicePublishCmd(d *deps) *cobra.Command {
	var (
		file   string
		orgID  string
		track  string
		sets   []string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a new version of every service in a catalog file",
		Long: "Publish the service definitions committed in a catalog file, one immutable version\n" +
			"per service. Built for CI, e.g.:\n\n" +
			"  zentoris service publish -f infra/zentoris-catalog.json --org $ORG --var commit=$GITHUB_SHA\n\n" +
			"The file lists services by NAME, so the same file publishes to any deployment; each must\n" +
			"already exist in the organization (this command never creates one). Services publish\n" +
			"leaf-first, so a ${versionId:Other} reference resolves to the version this run just\n" +
			"published. Container images are resolved to digests first, because publish itself is\n" +
			"network-free and only validates that every image is pinned.\n\n" +
			"Authenticates with any configured credential source (see `zentoris auth status`).",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			if orgID == "" {
				return fmt.Errorf("--org is required")
			}
			vars, err := parseKV(sets)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			cat, err := catalog.Parse(raw)
			if err != nil {
				return err
			}
			ordered, err := cat.InDependencyOrder()
			if err != nil {
				return err
			}

			ctx := c.Context()
			progress := c.ErrOrStderr()

			// Resolve every catalog service to an id up front: a missing one is a setup problem, and
			// finding it out before the first publish avoids a half-published catalog.
			serviceIDs := make(map[string]string, len(ordered))
			for _, svc := range ordered {
				found, err := findService(ctx, d, orgID, svc.Name)
				if err != nil {
					return err
				}
				serviceIDs[svc.Name] = found.ID
			}

			if dryRun {
				fmt.Fprintln(c.OutOrStdout(), "DRY RUN")
				for _, svc := range ordered {
					fmt.Fprintf(c.OutOrStdout(), "  publish %-24s -> POST /services/%s/versions (track %s)\n",
						svc.Name, serviceIDs[svc.Name], track)
				}
				fmt.Fprintln(c.OutOrStdout(),
					"  images are not resolved and no ${versionId:...} reference is filled in a dry run")
				return nil
			}

			publishedIDs := make(map[string]string, len(ordered))
			results := make([]publishedVersion, 0, len(ordered))
			for _, svc := range ordered {
				serviceID := serviceIDs[svc.Name]
				resolved, err := catalog.Resolve(svc.Definition, func(kind, name string) (string, error) {
					switch kind {
					case "serviceId":
						if id, ok := serviceIDs[name]; ok {
							return id, nil
						}
						return "", fmt.Errorf("service %q is not in this catalog", name)
					case "versionId":
						if id, ok := publishedIDs[name]; ok {
							return id, nil
						}
						return "", fmt.Errorf("service %q is not in this catalog", name)
					default:
						return "", fmt.Errorf("unsupported reference kind %q", kind)
					}
				})
				if err != nil {
					return fmt.Errorf("service %q: %w", svc.Name, err)
				}

				definition, err := catalog.Decode(resolved)
				if err != nil {
					return fmt.Errorf("service %q: %w", svc.Name, err)
				}
				for _, image := range definition.UnpinnedImages() {
					digest, err := resolveImage(ctx, d, serviceID, image.Image, vars)
					if err != nil {
						return fmt.Errorf("service %q component %q: %w", svc.Name, image.Component, err)
					}
					image.SetDigest(digest)
					fmt.Fprintf(progress, "pinned %s -> %s\n", image.Image, digest)
				}

				body := map[string]any{"track": track, "declaration": definition.Body()}
				if len(vars) > 0 {
					body["variables"] = vars
				}
				var out struct {
					ID            string `json:"id"`
					Track         string `json:"track"`
					VersionNumber int    `json:"versionNumber"`
				}
				path := "/services/" + url.PathEscape(serviceID) + "/versions"
				if err := d.api.Do(ctx, http.MethodPost, path, body, "", &out); err != nil {
					return fmt.Errorf("publish %q: %w", svc.Name, err)
				}
				publishedIDs[svc.Name] = out.ID
				results = append(results, publishedVersion{
					Service:       svc.Name,
					ServiceID:     serviceID,
					Track:         out.Track,
					VersionID:     out.ID,
					VersionNumber: out.VersionNumber,
				})
				fmt.Fprintf(progress, "published %s %s-%d\n", svc.Name, out.Track, out.VersionNumber)
			}
			return render(c, results)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "catalog file to publish (required)")
	cmd.Flags().StringVar(&orgID, "org", "", "organization id the services belong to (required)")
	cmd.Flags().StringVar(&track, "track", "v1", "track to publish onto")
	cmd.Flags().StringArrayVar(&sets, "var", nil, "value for a definition ${name} placeholder, KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "resolve the services and print the publish order without publishing")
	return cmd
}
