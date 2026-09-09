// Package catalog reads a committed service-definition catalog and prepares it for publishing.
//
// A catalog file is the same shape the platform's own seeder consumes, so one committed file
// serves both the first-time seed and every later publish from CI:
//
//	{ "services": [ { "name": "zentoris-auth", "definition": { "schemaVersion": 1, ... } } ] }
//
// Any other top-level key is ignored, so a file that also carries seed-only sections (connections,
// regions, systems) publishes without complaint.
//
// Two kinds of placeholder are the CLIENT's to resolve, because the platform does not know them:
// ${serviceId:Name} and ${versionId:Name}, which point at another service in the same catalog.
// Every other ${...} form is left alone - an image variable is substituted by the server at publish
// time, and a variable reference is resolved at deploy time.
package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Token prefixes this package resolves. Kept as constants because they appear in both the
// reference scan and the error message a stale reference produces.
const (
	serviceIDPrefix = "${serviceId:"
	versionIDPrefix = "${versionId:"
)

// refPattern captures the kind and the referenced service name of one placeholder.
var refPattern = regexp.MustCompile(`\$\{(serviceId|versionId):([^}]+)\}`)

// Service is one catalog entry: the service's name in the target organization, and the definition
// to publish as a new immutable version.
type Service struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

// File is a parsed catalog.
type File struct {
	Services []Service `json:"services"`
}

// Parse reads a catalog file and rejects the shapes that would fail later with a worse message.
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	if len(f.Services) == 0 {
		return nil, fmt.Errorf("catalog has no services")
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

// References returns the catalog service names one definition points at, deduplicated. A reference
// to a name the catalog does not carry is returned too: the caller decides whether that is fatal
// (publishing needs it) or fine (it may already exist on the target).
func References(definition json.RawMessage) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range refPattern.FindAllStringSubmatch(string(definition), -1) {
		if name := m[2]; !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// InDependencyOrder returns the services leaf-first, so a referenced service is always published
// before the one referencing it. Order within a tier is the catalog's own, which keeps a run's
// output stable and diffable. A reference cycle is an error, since no order can satisfy it.
func (f *File) InDependencyOrder() ([]Service, error) {
	byName := make(map[string]Service, len(f.Services))
	position := make(map[string]int, len(f.Services))
	for i, s := range f.Services {
		byName[s.Name] = s
		position[s.Name] = i
	}

	var (
		out      []Service
		done     = map[string]bool{}
		visiting = map[string]bool{}
		visit    func(name string, path []string) error
	)
	visit = func(name string, path []string) error {
		if done[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("reference cycle: %s -> %s", strings.Join(path, " -> "), name)
		}
		svc, ok := byName[name]
		if !ok {
			// Not in this catalog: nothing to order, and whether it must exist is the caller's call.
			return nil
		}
		visiting[name] = true
		deps := References(svc.Definition)
		sort.Slice(deps, func(i, j int) bool { return position[deps[i]] < position[deps[j]] })
		for _, dep := range deps {
			if dep == name {
				return fmt.Errorf("service %q references itself", name)
			}
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		visiting[name] = false
		done[name] = true
		out = append(out, svc)
		return nil
	}

	for _, s := range f.Services {
		if err := visit(s.Name, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Resolve substitutes every ${serviceId:Name} / ${versionId:Name} placeholder in a definition,
// asking lookup for each one. Substitution runs on the JSON text, so a placeholder is replaced
// wherever it appears and no other ${...} form is touched. An unresolvable reference is an error:
// publishing a definition that still names a placeholder would store the literal text in an
// immutable version.
func Resolve(definition json.RawMessage, lookup func(kind, name string) (string, error)) (json.RawMessage, error) {
	var failure error
	resolved := refPattern.ReplaceAllStringFunc(string(definition), func(token string) string {
		m := refPattern.FindStringSubmatch(token)
		value, err := lookup(m[1], m[2])
		if err != nil {
			if failure == nil {
				failure = fmt.Errorf("%s: %w", token, err)
			}
			return token
		}
		return value
	})
	if failure != nil {
		return nil, failure
	}
	return json.RawMessage(resolved), nil
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
