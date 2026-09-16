package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/zentoris-labs/ztr-cli/internal/catalog"
)

// publishedVersion is the command's JSON result: what was published, and where.
type publishedVersion struct {
	Service       string `json:"service"`
	ServiceID     string `json:"serviceId"`
	Track         string `json:"track"`
	VersionID     string `json:"versionId"`
	VersionNumber int    `json:"versionNumber"`
}

func newServicePublishCmd(d *deps) *cobra.Command {
	var (
		file      string
		serviceID string
		track     string
		sets      []string
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a new immutable version of one service",
		Long: "Publish the definition committed in a file as a new immutable version of one service.\n" +
			"Built for CI, e.g.:\n\n" +
			"  zentoris service publish -f infra/my-api.json --service-id $SERVICE_ID --var commit=$GITHUB_SHA\n\n" +
			"The service must already exist (this command never creates one) and is named by id, so\n" +
			"the same committed file publishes to any deployment - what differs between deployments is\n" +
			"the id, which belongs in the pipeline's configuration rather than in the repository.\n\n" +
			"Container images are resolved to digests first, because publish itself is network-free and\n" +
			"only validates that every image is pinned. The ${...} form stays in the image reference and\n" +
			"the digest is stamped beside it, so both what was written and what it locked to are kept.\n\n" +
			"Publish one service per call. Several services are several calls, independent of each\n" +
			"other, so one failing leaves the rest untouched.\n\n" +
			"Authenticates with any configured credential source (see `zentoris auth status`).",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			if serviceID == "" {
				return fmt.Errorf("--service-id is required")
			}
			vars, err := parseKV(sets)
			if err != nil {
				return err
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			parsed, err := catalog.Parse(raw)
			if err != nil {
				return err
			}
			svc, err := parsed.Single()
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintln(c.OutOrStdout(), "DRY RUN")
				fmt.Fprintf(c.OutOrStdout(), "  publish %s -> POST /services/%s/versions (track %s)\n",
					svc.Name, serviceID, track)
				fmt.Fprintln(c.OutOrStdout(),
					"  the service is not read and no image is resolved, so this proves the file, not the target")
				return nil
			}

			ctx := c.Context()
			progress := c.ErrOrStderr()

			definition, err := catalog.Decode(svc.Definition)
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
			fmt.Fprintf(progress, "published %s %s-%d\n", svc.Name, out.Track, out.VersionNumber)

			return render(c, publishedVersion{
				Service:       svc.Name,
				ServiceID:     serviceID,
				Track:         out.Track,
				VersionID:     out.ID,
				VersionNumber: out.VersionNumber,
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "definition file to publish (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "id of the service to publish onto (required)")
	cmd.Flags().StringVar(&track, "track", "v1", "track to publish onto")
	cmd.Flags().StringArrayVar(&sets, "var", nil, "value for a definition ${name} placeholder, KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "read and check the file, then print what would be published")
	return cmd
}
