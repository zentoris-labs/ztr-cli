package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// definitionWithDir builds a definition whose one managed variant reads its OpenTofu code from dir.
func definitionWithDir(t *testing.T, dir string) *Definition {
	t.Helper()
	raw := `{"components": [
	  {"id": "federation", "variants": [
	    {"type": "managed", "tool": {"type": "opentofu", "source": {"type": "inline", "dir": ` +
		mustJSON(t, dir) + `}}}
	  ]}
	]}`
	d, err := Decode(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return d
}

func mustJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// sourceNode digs out the one inline source object, to assert on what Expand left behind.
func sourceNode(t *testing.T, d *Definition) map[string]any {
	t.Helper()
	component := d.Body()["components"].([]any)[0].(map[string]any)
	variant := component["variants"].([]any)[0].(map[string]any)
	return variant["tool"].(map[string]any)["source"].(map[string]any)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpandReadsTheDirectoryIntoFiles(t *testing.T) {
	base := t.TempDir()
	writeFiles(t, filepath.Join(base, "federation"), map[string]string{
		"main.tf":       "resource \"null_resource\" \"a\" {}",
		"vars.tfvars":   "region = \"eu-central-1\"",
		"README.md":     "not published",
		"stack.tf.json": "{}",
		"settings.json": "not infrastructure code",
	})
	// A subdirectory is not descended into: a module belongs in a source of its own.
	writeFiles(t, filepath.Join(base, "federation", "modules"), map[string]string{"sub.tf": "ignored"})

	d := definitionWithDir(t, "federation")
	dirs, err := d.InlineDirs()
	if err != nil || len(dirs) != 1 {
		t.Fatalf("InlineDirs = %v, %v", dirs, err)
	}
	read, err := dirs[0].Expand(base)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(read) != 3 || read[0].Name != "main.tf" || read[1].Name != "stack.tf.json" || read[2].Name != "vars.tfvars" {
		t.Fatalf("read = %+v", read)
	}

	source := sourceNode(t, d)
	if _, stillThere := source["dir"]; stillThere {
		t.Fatal("dir survived the expansion; it must never reach the platform")
	}
	files := source["files"].(map[string]any)
	if len(files) != 3 || files["main.tf"] != "resource \"null_resource\" \"a\" {}" {
		t.Fatalf("files = %#v", files)
	}
}

func TestExpandIsByteIdenticalAcrossRuns(t *testing.T) {
	base := t.TempDir()
	writeFiles(t, filepath.Join(base, "infra"), map[string]string{
		"z.tf": "z", "a.tf": "a", "m.tf": "m",
	})
	encode := func() string {
		d := definitionWithDir(t, "infra")
		dirs, err := d.InlineDirs()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dirs[0].Expand(base); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(d.Body())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if first, second := encode(), encode(); first != second {
		t.Fatalf("publishing the same tree twice differed:\n%s\n%s", first, second)
	}
}

func TestExpandRejectsUnpublishableDirectories(t *testing.T) {
	base := t.TempDir()
	writeFiles(t, filepath.Join(base, "empty"), map[string]string{"README.md": "no code here"})
	writeFiles(t, filepath.Join(base, "binary"), map[string]string{"main.tf": "\xff\xfe\x00"})
	writeFiles(t, filepath.Join(base, "huge"), map[string]string{"main.tf": strings.Repeat("x", inlineFileBytesMax+1)})

	cases := []struct {
		name, dir, want string
	}{
		{"absolute", "/etc", "must be relative"},
		{"climbing", "../secrets", "must not climb"},
		{"empty dir field", "  ", "is empty"},
		{"missing", "nope", "no such file"},
		{"no code", "empty", "no infrastructure code"},
		{"not utf8", "binary", "not valid UTF-8"},
		{"file too big", "huge", "exceeds the"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := definitionWithDir(t, tc.dir)
			dirs, err := d.InlineDirs()
			if err != nil {
				t.Fatalf("InlineDirs: %v", err)
			}
			if len(dirs) != 1 {
				t.Fatalf("want one dir source, got %d", len(dirs))
			}
			_, err = dirs[0].Expand(base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExpandDoesNotFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside.tf")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "infra")
	writeFiles(t, dir, map[string]string{"main.tf": "real"})
	if err := os.Symlink(outside, filepath.Join(dir, "linked.tf")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	d := definitionWithDir(t, "infra")
	dirs, _ := d.InlineDirs()
	read, err := dirs[0].Expand(base)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(read) != 1 || read[0].Name != "main.tf" {
		t.Fatalf("a symlink was published: %+v", read)
	}
}

func TestInlineDirsLeavesAWrittenOutSourceAlone(t *testing.T) {
	raw := `{"components": [
	  {"id": "federation", "variants": [
	    {"type": "managed", "tool": {"type": "opentofu", "source":
	      {"type": "inline", "files": {"main.tf": "written out"}}}}
	  ]}
	]}`
	d, err := Decode(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := d.InlineDirs()
	if err != nil || len(dirs) != 0 {
		t.Fatalf("dirs = %v, err = %v", dirs, err)
	}
}

func TestInlineDirsRejectsDirAndFilesTogether(t *testing.T) {
	raw := `{"components": [
	  {"id": "federation", "variants": [
	    {"type": "managed", "tool": {"type": "opentofu", "source":
	      {"type": "inline", "dir": "infra", "files": {"main.tf": "x"}}}}
	  ]}
	]}`
	d, err := Decode(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.InlineDirs(); err == nil || !strings.Contains(err.Error(), "both dir and files") {
		t.Fatalf("err = %v", err)
	}
}
