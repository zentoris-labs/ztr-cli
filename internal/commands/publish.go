package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

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
			"An infrastructure component may keep its OpenTofu code as real files: give its inline source\n" +
			"a `dir` relative to this file instead of a `files` map, and the infrastructure code in that\n" +
			"directory (.tf, .tfvars, .tf.json, .tfvars.json) is read and published as its contents.\n" +
			"Nothing on disk is published that the directory does not hold - no subdirectory is descended\n" +
			"into and no symlink followed.\n\n" +
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

			definition, err := catalog.Decode(svc.Definition)
			if err != nil {
				return fmt.Errorf("service %q: %w", svc.Name, err)
			}
			inlined, err := inlineSources(definition, filepath.Dir(file))
			if err != nil {
				return fmt.Errorf("service %q: %w", svc.Name, err)
			}

			if dryRun {
				out := c.OutOrStdout()
				fmt.Fprintln(out, "DRY RUN")
				fmt.Fprintf(out, "  publish %s -> POST /services/%s/versions (track %s)\n",
					svc.Name, serviceID, track)
				for _, source := range inlined {
					fmt.Fprintf(out, "  inlined %s from %s\n", source.Component, source.Dir)
					for _, f := range source.Files {
						fmt.Fprintf(out, "    %s (%d bytes)\n", f.Name, f.Bytes)
					}
				}
				fmt.Fprintln(out,
					"  the service is not read and no image is resolved, so this proves the file, not the target")
				return nil
			}

			ctx := c.Context()
			progress := c.ErrOrStderr()

			for _, source := range inlined {
				fmt.Fprintf(progress, "inlined %s from %s (%d files)\n", source.Component, source.Dir, len(source.Files))
			}
			for _, image := range definition.UnpinnedImages() {
				resolved, err := resolveImage(ctx, d, serviceID, image.Image, vars)
				if err != nil {
					return fmt.Errorf("service %q component %q: %w", svc.Name, image.Component, err)
				}
				image.SetDigest(resolved.Digest)
				// An older platform reports no archs; the variant then stays as the file wrote it.
				if len(resolved.Archs) > 0 {
					image.SetArchs(resolved.Archs)
				}
				fmt.Fprintf(progress, "pinned %s -> %s\n", image.Image, resolved.Digest)
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

// inlinedSource records one expanded inline tool source, for the progress line that tells the
// operator which local files became part of the published version.
type inlinedSource struct {
	Component string
	Dir       string
	Files     []catalog.InlinedFile
}

// inlineSources reads every inline tool source that names a directory and replaces it with the file
// contents the platform stores. It runs before the image pass and before the dry-run report, because
// it needs no network and a definition that cannot be assembled locally is not worth resolving
// images for.
func inlineSources(definition *catalog.Definition, baseDir string) ([]inlinedSource, error) {
	dirs, err := definition.InlineDirs()
	if err != nil {
		return nil, err
	}
	out := make([]inlinedSource, 0, len(dirs))
	for _, dir := range dirs {
		files, err := dir.Expand(baseDir)
		if err != nil {
			return nil, fmt.Errorf("component %q: %w", dir.Component, err)
		}
		out = append(out, inlinedSource{Component: dir.Component, Dir: dir.Dir, Files: files})
	}
	return out, nil
}
