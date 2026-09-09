package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// pagedList is the platform's list envelope. Only the fields the CLI reads are declared.
type pagedList[T any] struct {
	Items []T `json:"items"`
}

// namedResource is the shape a by-name lookup needs: the opaque id and the name matched on.
type namedResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// findByName resolves one resource by its exact name inside a parent scope. The platform's list
// endpoints take a `q` search, which narrows the page; the exact match is made here, so a name that
// is merely a prefix of another cannot win.
func findByName(ctx context.Context, d *deps, resource, scope, name string) (namedResource, error) {
	path := fmt.Sprintf("/%s?%s&q=%s&limit=200", resource, scope, url.QueryEscape(name))
	var page pagedList[namedResource]
	if err := d.api.Do(ctx, http.MethodGet, path, nil, "", &page); err != nil {
		return namedResource{}, err
	}
	for _, item := range page.Items {
		if item.Name == name {
			return item, nil
		}
	}
	return namedResource{}, fmt.Errorf("no %s named %q (%s)", singular(resource), name, scope)
}

// singular trims the list path's plural for an error message.
func singular(resource string) string {
	if len(resource) > 1 && resource[len(resource)-1] == 's' {
		return resource[:len(resource)-1]
	}
	return resource
}

func findService(ctx context.Context, d *deps, orgID, name string) (namedResource, error) {
	return findByName(ctx, d, "services", "organizationId="+url.QueryEscape(orgID), name)
}

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
