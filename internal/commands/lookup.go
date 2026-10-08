package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type resolvedImage struct {
	Digest string   `json:"digest"`
	Archs  []string `json:"archs"`
}

// resolveImage turns one image reference into its digest and archs through the platform, which
// authenticates to the registry with the organization's own connected provider. Publish is
// network-free and only validates that images are pinned, so this is the client's job.
func resolveImage(ctx context.Context, d *deps, serviceID, image string, vars map[string]string) (resolvedImage, error) {
	body := map[string]any{"image": image}
	if len(vars) > 0 {
		body["variables"] = vars
	}
	var out resolvedImage
	path := "/container-registry/resolve-image?serviceId=" + url.QueryEscape(serviceID)
	if err := d.api.Do(ctx, http.MethodPost, path, body, "", &out); err != nil {
		return resolvedImage{}, fmt.Errorf("resolve %s: %w", image, err)
	}
	if out.Digest == "" {
		return resolvedImage{}, fmt.Errorf("resolve %s: the platform returned no digest", image)
	}
	return out, nil
}
