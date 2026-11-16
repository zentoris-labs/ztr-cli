// Package catalog reads a committed service definition and prepares it for publishing.
//
// A definition file describes ONE service:
//
//	{ "name": "my-api", "definition": { "schemaVersion": 1, ... } }
//
// A file publishes to one service, named by id on the command line, so each service's definition
// can be read, reviewed and published on its own. Any other top-level key is ignored, so a file that
// also carries fields this command has no use for (a description, say) publishes without complaint.
//
// Every ${...} form in a definition is left exactly as written. An image variable is substituted
// by the platform at publish time and a variable reference is resolved at deploy time, so
// nothing here rewrites the text it publishes; the one thing this package adds is a resolved
// digest beside each container image, because publish itself performs no network I/O.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Service is a parsed definition file: a human-readable name for output, and the definition to
// publish as a new immutable version. The name identifies nothing - the publish target is the
// service id given on the command line - so renaming a service on the platform does not strand its
// file.
type Service struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

// Parse reads a definition file and rejects the shapes that would fail later with a worse message.
func Parse(data []byte) (Service, error) {
	var f struct {
		Service
		// Read only to name the file shape v0.8.0 and earlier took, `{ "services": [ ... ] }`, instead of
		// reporting it as a file with no name.
		Services json.RawMessage `json:"services"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return Service{}, fmt.Errorf("parse definition file: %w", err)
	}
	s := f.Service
	if strings.TrimSpace(s.Name) == "" {
		if len(f.Services) > 0 {
			return Service{}, fmt.Errorf("definition file uses the removed `services` envelope: " +
				"move its one entry's name and definition to the top level")
		}
		return Service{}, fmt.Errorf("definition file has no name")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(s.Definition), []byte("{")) {
		return Service{}, fmt.Errorf("service %q has no definition object", s.Name)
	}
	return s, nil
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

// SetArchs records the CPU architectures the image runs on, as the platform reported them.
func (v ContainerVariant) SetArchs(archs []string) { v.node["archs"] = archs }

// HasArchs reports whether the file set archs on the variant by hand.
func (v ContainerVariant) HasArchs() bool {
	archs, _ := v.node["archs"].([]any)
	return len(archs) > 0
}

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
