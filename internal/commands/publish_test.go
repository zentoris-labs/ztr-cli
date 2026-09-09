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

// catalogFixture is two services where the umbrella pins the leaf, so a run has to publish the leaf
// first and fill the umbrella's ${versionId:leaf} with what it just created.
const catalogFixture = `{
  "services": [
    {"name": "umbrella", "definition": {"components": [
      {"id": "leaf", "variants": [{"type": "service", "serviceId": "${serviceId:leaf}", "versionId": "${versionId:leaf}"}]}
    ]}},
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

func TestServicePublishWholeCatalog(t *testing.T) {
	var (
		order       []string
		publishBody = map[string]string{}
		resolved    []string
	)
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/services":
			name := r.URL.Query().Get("q")
			io.WriteString(w, `{"items":[{"id":"id-of-`+name+`","name":"`+name+`"}]}`)

		case r.Method == http.MethodPost && r.URL.Path == "/api/container-registry/resolve-image":
			b, _ := io.ReadAll(r.Body)
			resolved = append(resolved, string(b))
			io.WriteString(w, `{"digest":"sha256:deadbeef"}`)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/versions"):
			service := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/services/id-of-"), "/versions")
			order = append(order, service)
			b, _ := io.ReadAll(r.Body)
			publishBody[service] = string(b)
			io.WriteString(w, `{"id":"ver-of-`+service+`","track":"v1","versionNumber":7}`)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	file := writeFixture(t, "catalog.json", catalogFixture)
	out, progress, err := runSplit(t, newServicePublishCmd(d), "-f", file, "--org", "org_1", "--var", "commit=abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pinned ghcr.io/o/leaf:${commit} -> sha256:deadbeef", "published leaf v1-7"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress %q missing %q", progress, want)
		}
	}

	if strings.Join(order, ",") != "leaf,umbrella" {
		t.Errorf("publish order = %v, want leaf then umbrella", order)
	}
	if !strings.Contains(resolved[0], `"image":"ghcr.io/o/leaf:${commit}"`) {
		t.Errorf("resolve body %q, want the templated image sent as written", resolved[0])
	}
	if !strings.Contains(resolved[0], `"commit":"abc123"`) {
		t.Errorf("resolve body %q, want the --var forwarded for substitution", resolved[0])
	}
	if !strings.Contains(publishBody["leaf"], `"digest":"sha256:deadbeef"`) {
		t.Errorf("leaf declaration %q, want the resolved digest stamped", publishBody["leaf"])
	}
	if !strings.Contains(publishBody["leaf"], `"image":"ghcr.io/o/leaf:${commit}"`) {
		t.Errorf("leaf declaration %q, want the ${commit} form kept for publish to substitute", publishBody["leaf"])
	}
	if !strings.Contains(publishBody["leaf"], `"variables":{"commit":"abc123"}`) {
		t.Errorf("leaf request %q, want the variables forwarded to publish", publishBody["leaf"])
	}
	if !strings.Contains(publishBody["umbrella"], `"versionId":"ver-of-leaf"`) {
		t.Errorf("umbrella declaration %q, want ${versionId:leaf} filled with this run's version", publishBody["umbrella"])
	}
	if !strings.Contains(publishBody["umbrella"], `"serviceId":"id-of-leaf"`) {
		t.Errorf("umbrella declaration %q, want ${serviceId:leaf} filled", publishBody["umbrella"])
	}
	if strings.Contains(publishBody["umbrella"], "${") {
		t.Errorf("umbrella declaration %q still carries a placeholder", publishBody["umbrella"])
	}

	var summary []publishedVersion
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("output %q is not the JSON summary: %v", out, err)
	}
	if len(summary) != 2 || summary[0].Service != "leaf" || summary[0].VersionNumber != 7 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestServicePublishStopsWhenAServiceIsMissing(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/versions") {
			t.Error("published despite an unresolvable service")
		}
		io.WriteString(w, `{"items":[]}`)
	})
	file := writeFixture(t, "catalog.json", catalogFixture)
	_, err := run1(t, newServicePublishCmd(d), "-f", file, "--org", "org_1")
	if err == nil || !strings.Contains(err.Error(), "no service named") {
		t.Fatalf("err = %v, want the missing-service error", err)
	}
}

func TestServicePublishDryRunWritesNothing(t *testing.T) {
	d, _ := apiDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("dry run sent %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"items":[{"id":"id-x","name":"`+r.URL.Query().Get("q")+`"}]}`)
	})
	file := writeFixture(t, "catalog.json", catalogFixture)
	out, err := run1(t, newServicePublishCmd(d), "-f", file, "--org", "org_1", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DRY RUN", "publish leaf", "publish umbrella"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output %q missing %q", out, want)
		}
	}
	if strings.Index(out, "publish leaf") > strings.Index(out, "publish umbrella") {
		t.Errorf("dry-run output %q lists umbrella before leaf", out)
	}
}

func TestServicePublishRejectsABadCatalog(t *testing.T) {
	file := writeFixture(t, "catalog.json", `{"services":[]}`)
	_, err := run1(t, newServicePublishCmd(&deps{}), "-f", file, "--org", "org_1")
	if err == nil || !strings.Contains(err.Error(), "no services") {
		t.Fatalf("err = %v, want the empty-catalog error", err)
	}
}
