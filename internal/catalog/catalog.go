// Package catalog reads a committed service definition and prepares it for publishing.
//
// A definition file describes ONE service:
//
//	{ "services": [ { "name": "my-api", "definition": { "schemaVersion": 1, ... } } ] }
//
// The `services` array is a one-entry envelope rather than a list: a file publishes to one
// service, named by id on the command line, so each service's definition can be read, reviewed
// and published on its own. Any other top-level key is ignored, so a file that also carries
// sections this command has no use for publishes without complaint.
//
// Every ${...} form in a definition is left exactly as written. An image variable is substituted
// by the platform at publish time and a variable reference is resolved at deploy time, so
// nothing here rewrites the text it publishes; the one thing this package adds is a resolved
// digest beside each container image, because publish itself performs no network I/O.
package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Service is the file's one entry: a human-readable name for output, and the definition to publish
// as a new immutable version. The name identifies nothing - the publish target is the service id
// given on the command line - so renaming a service on the platform does not strand its file.
type Service struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

// File is a parsed definition file.
type File struct {
	Services []Service `json:"services"`
}

// Parse reads a definition file and rejects the shapes that would fail later with a worse message.
// It accepts the envelope as written and leaves "exactly one service" to Single, so a malformed
// entry is reported as what it is rather than as a wrong count.
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse definition file: %w", err)
	}
	if len(f.Services) == 0 {
		return nil, fmt.Errorf("file declares no services")
	}
	seen := make(map[string]bool, len(f.Services))
	for i, s := range f.Services {
		if strings.TrimSpace(s.Name) == "" {
			return nil, fmt.Errorf("service #%d has no name", i+1)
		}
		if len(s.Definition) == 0 {
			return nil, fmt.Errorf("service %q has no definition", s.Name)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("service %q appears twice", s.Name)
		}
		seen[s.Name] = true
	}
	return &f, nil
}

// Single returns the file's one service. A file describes exactly one, so anything else is a
// mistake worth naming rather than a list to iterate: publishing takes one service id, and
// silently using the first entry would publish the wrong definition onto it.
func (f *File) Single() (Service, error) {
	if len(f.Services) != 1 {
		return Service{}, fmt.Errorf("a definition file describes one service, but this one has %d", len(f.Services))
	}
	return f.Services[0], nil
}

// ContainerVariant is one container variant of a definition, exposed so the caller can resolve its
// image to a digest. The map is the live node inside Definition, so writing Digest updates it.
type ContainerVariant struct {
	Component string
	Image     string
	node      map[string]any
}

// SetDigest stamps a resolved digest BESIDE the image reference rather than into it, so the
// reference somebody wrote and the digest it locked to are both readable in the published version.
func (v ContainerVariant) SetDigest(digest string) { v.node["digest"] = digest }

// Definition is a definition decoded for mutation.
type Definition struct{ root map[string]any }

// Decode parses a definition for the image pass. Decoding and re-encoding may reorder object keys,
// which JSON does not give meaning to.
func Decode(definition json.RawMessage) (*Definition, error) {
	var root map[string]any
	if err := json.Unmarshal(definition, &root); err != nil {
		return nil, fmt.Errorf("decode definition: %w", err)
	}
	return &Definition{root: root}, nil
}

// Body returns the definition to publish.
func (d *Definition) Body() map[string]any { return d.root }

// UnpinnedImages lists the container variants that still need a digest, skipping any already
// pinned - by its own digest field, or by an @sha256: in the reference. Publish is network-free and
// only VALIDATES that images are pinned, so resolving them is the client's job.
func (d *Definition) UnpinnedImages() []ContainerVariant {
	out := []ContainerVariant{}
	components, _ := d.root["components"].([]any)
	for _, c := range components {
		component, _ := c.(map[string]any)
		if component == nil {
			continue
		}
		componentID, _ := component["id"].(string)
		variants, _ := component["variants"].([]any)
		for _, v := range variants {
			variant, _ := v.(map[string]any)
			if variant == nil {
				continue
			}
			if kind, _ := variant["type"].(string); kind != "container" {
				continue
			}
			image, _ := variant["image"].(string)
			if image == "" || strings.Contains(image, "@sha256:") {
				continue
			}
			if digest, _ := variant["digest"].(string); digest != "" {
				continue
			}
			out = append(out, ContainerVariant{Component: componentID, Image: image, node: variant})
		}
	}
	return out
}
