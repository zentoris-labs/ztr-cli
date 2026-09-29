package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// The limits the platform enforces on an inline tool source. They are mirrored here so a file that
// cannot be published is named as such against the local file that broke it, rather than coming
// back as a rejection of the whole definition after the upload.
const (
	inlineFileNameMax   = 255
	inlineFileBytesMax  = 256 * 1024
	inlineTotalBytesMax = 1024 * 1024
	inlineFileCountMax  = 50
)

// The extensions an inline source may carry - the four shapes OpenTofu loads, matched case-sensitively
// as suffixes because the platform matches them the same way (a .tf.json is not what filepath.Ext
// reports). Anything else in the directory - a README, a lockfile, a .terraform cache - is skipped
// rather than rejected, because those sit beside real infrastructure code in every working tree and
// refusing to publish over them would help nobody.
var inlineExtensions = []string{".tf", ".tfvars", ".tf.json", ".tfvars.json"}

func isInfrastructureCode(name string) bool {
	for _, ext := range inlineExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// InlineDir is one managed variant whose inline source names a DIRECTORY on disk instead of spelling
// out file contents. The node is the live source object inside the definition, so Expand rewrites it
// in place: what reaches the platform is always the contents, and `dir` never leaves this machine.
type InlineDir struct {
	Component string
	Dir       string
	node      map[string]any
}

// InlinedFile is one file read into a definition, reported so a caller can show what it published.
type InlinedFile struct {
	Name  string
	Bytes int
}

// InlineDirs lists the inline sources that still name a directory. A source that already carries
// `files` is left alone, so a definition written the long way publishes unchanged; declaring both is
// an error rather than a precedence rule, because either answer would silently discard one of them.
func (d *Definition) InlineDirs() ([]InlineDir, error) {
	out := []InlineDir{}
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
			tool, _ := variant["tool"].(map[string]any)
			if tool == nil {
				continue
			}
			source, _ := tool["source"].(map[string]any)
			if source == nil {
				continue
			}
			if kind, _ := source["type"].(string); kind != "inline" {
				continue
			}
			dir, _ := source["dir"].(string)
			_, hasFiles := source["files"]
			if dir != "" && hasFiles {
				return nil, fmt.Errorf("component %q declares both dir and files on one inline source", componentID)
			}
			if dir == "" {
				continue
			}
			out = append(out, InlineDir{Component: componentID, Dir: dir, node: source})
		}
	}
	return out, nil
}

// Expand reads the directory and replaces `dir` with the `files` map the platform expects. Paths
// resolve against baseDir - the directory of the definition file, never the working directory - so
// the same file publishes identically from a repository root and from a CI checkout.
//
// The read is flat and takes regular files only: a subdirectory is not descended into (OpenTofu
// treats one working directory as one configuration, and a module belongs in a source of its own)
// and a symlink is not followed (it is the cheapest way to publish a file from outside the tree).
func (s InlineDir) Expand(baseDir string) ([]InlinedFile, error) {
	rel, err := safeRelativeDir(s.Dir)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(baseDir, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := map[string]any{}
	read := []InlinedFile{}
	total := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !isInfrastructureCode(name) {
			continue
		}
		if len(name) > inlineFileNameMax {
			return nil, fmt.Errorf("%s: file name is longer than %d characters", name, inlineFileNameMax)
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if len(content) > inlineFileBytesMax {
			return nil, fmt.Errorf("%s: %d bytes exceeds the %d byte limit for one file", name, len(content), inlineFileBytesMax)
		}
		if !utf8.Valid(content) {
			return nil, fmt.Errorf("%s: not valid UTF-8, so it cannot be carried in a definition", name)
		}
		total += len(content)
		files[name] = string(content)
		read = append(read, InlinedFile{Name: name, Bytes: len(content)})
	}

	if len(read) == 0 {
		return nil, fmt.Errorf("%s: no infrastructure code (%s)", s.Dir, strings.Join(inlineExtensions, ", "))
	}
	if len(read) > inlineFileCountMax {
		return nil, fmt.Errorf("%s: %d files exceeds the limit of %d", s.Dir, len(read), inlineFileCountMax)
	}
	if total > inlineTotalBytesMax {
		return nil, fmt.Errorf("%s: %d bytes in total exceeds the %d byte limit", s.Dir, total, inlineTotalBytesMax)
	}

	sort.Slice(read, func(i, j int) bool { return read[i].Name < read[j].Name })
	delete(s.node, "dir")
	s.node["files"] = files
	return read, nil
}

// safeRelativeDir rejects the paths that would read outside the definition file's own tree. A
// definition is published by a pipeline over a checkout it did not write, so an absolute path or a
// climb above the file is refused rather than resolved.
func safeRelativeDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("inline source dir is empty")
	}
	if filepath.IsAbs(dir) || strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("%s: dir must be relative to the definition file", dir)
	}
	clean := filepath.Clean(filepath.FromSlash(dir))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: dir must not climb above the definition file", dir)
	}
	return clean, nil
}
