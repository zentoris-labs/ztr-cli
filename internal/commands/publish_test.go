package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// definitionFixture is one service with one templated image, which is the whole shape publish
// handles: read the file, resolve the image, POST the version.
const definitionFixture = `{
  "services": [
    {"name": "leaf", "definition": {"components": [
      {"id": "api", "variants": [{"type": "container", "image": "ghcr.io/o/leaf:${commit}"}]}
    ]}}
  ]
}`

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServicePublishOneService(t *testing.T) {
	var (
		publishedTo string
		publishBody string
		resolved    []string
	)
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/container-registry/resolve-image":
			b, _ := io.ReadAll(r.Body)
			resolved = append(resolved, string(b))
			io.WriteString(w, `{"digest":"sha256:deadbeef"}`)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/versions"):
			publishedTo = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			publishBody = string(b)
			io.WriteString(w, `{"id":"ver-1","track":"v1","versionNumber":7}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	file := writeFixture(t, "leaf.json", definitionFixture)
	out, progress, err := runSplit(t, newServicePublishCmd(d),
		"-f", file, "--service-id", "svc_1", "--var", "commit=abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pinned ghcr.io/o/leaf:${commit} -> sha256:deadbeef", "published leaf v1-7"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress %q missing %q", progress, want)
		}
	}

	// The id the caller gave is the publish target, with no lookup in between.
	if publishedTo != "/api/services/svc_1/versions" {
		t.Errorf("published to %q, want the id passed on the command line", publishedTo)
	}
	if !strings.Contains(resolved[0], `"image":"ghcr.io/o/leaf:${commit}"`) {
		t.Errorf("resolve body %q, want the templated image sent as written", resolved[0])
	}
	if !strings.Contains(resolved[0], `"commit":"abc123"`) {
		t.Errorf("resolve body %q, want the --var forwarded for substitution", resolved[0])
	}
	if !strings.Contains(publishBody, `"digest":"sha256:deadbeef"`) {
		t.Errorf("declaration %q, want the resolved digest stamped", publishBody)
	}
	if !strings.Contains(publishBody, `"image":"ghcr.io/o/leaf:${commit}"`) {
		t.Errorf("declaration %q, want the ${commit} form kept for publish to substitute", publishBody)
	}
	if !strings.Contains(publishBody, `"variables":{"commit":"abc123"}`) {
		t.Errorf("request %q, want the variables forwarded to publish", publishBody)
	}

	var summary publishedVersion
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("output %q is not the JSON summary: %v", out, err)
	}
	if summary.Service != "leaf" || summary.ServiceID != "svc_1" || summary.VersionNumber != 7 {
		t.Fatalf("summary = %+v", summary)
	}
}

// Nothing in a definition is rewritten on the way out: a ${...} the platform owns has to arrive
// at the platform intact, because the CLI cannot know what it means.
func TestServicePublishLeavesPlaceholdersAlone(t *testing.T) {
	var publishBody string
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/versions") {
			publishBody = string(b)
			io.WriteString(w, `{"id":"ver-1","track":"v1","versionNumber":1}`)
			return
		}
		io.WriteString(w, `{"digest":"sha256:aa"}`)
	})
	file := writeFixture(t, "app.json", `{"services":[{"name":"app","definition":{"components":[
		{"id":"api","variants":[{"type":"container","image":"repo@sha256:bb","env":{"URL":"${auth_base_url}"}}]}
	]}}]}`)
	if _, err := run1(t, newServicePublishCmd(d), "-f", file, "--service-id", "svc_1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(publishBody, `"URL":"${auth_base_url}"`) {
		t.Errorf("declaration %q, want the deploy-time reference passed through untouched", publishBody)
	}
}

func TestServicePublishDryRunWritesNothing(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry run sent %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	file := writeFixture(t, "leaf.json", definitionFixture)
	out, err := run1(t, newServicePublishCmd(d), "-f", file, "--service-id", "svc_1", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DRY RUN", "publish leaf", "/services/svc_1/versions"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output %q missing %q", out, want)
		}
	}
}

func TestServicePublishRejectsABadFile(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"empty":    {`{"services":[]}`, "no services"},
		"two":      {`{"services":[{"name":"a","definition":{}},{"name":"b","definition":{}}]}`, "has 2"},
		"not json": {`{`, "parse definition file"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			file := writeFixture(t, "bad.json", tc.content)
			_, err := run1(t, newServicePublishCmd(&deps{}), "-f", file, "--service-id", "svc_1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
