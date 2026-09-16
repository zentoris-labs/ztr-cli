package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// resolveImage turns one image reference into a digest through the platform, which authenticates to
// the registry with the organization's own connected provider. Publish is network-free and only
// validates that images are pinned, so this is the client's job.
func resolveImage(ctx context.Context, d *deps, serviceID, image string, vars map[string]string) (string, error) {
	body := map[string]any{"image": image}
	if len(vars) > 0 {
		body["variables"] = vars
	}
	var out struct {
		Digest string `json:"digest"`
	}
	path := "/container-registry/resolve-image?serviceId=" + url.QueryEscape(serviceID)
	if err := d.api.Do(ctx, http.MethodPost, path, body, "", &out); err != nil {
		return "", fmt.Errorf("resolve %s: %w", image, err)
	}
	if out.Digest == "" {
		return "", fmt.Errorf("resolve %s: the platform returned no digest", image)
	}
	return out.Digest, nil
}
