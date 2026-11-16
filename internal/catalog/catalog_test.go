package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const oneService = `{
  "name": "leaf",
  "description": "ignored",
  "definition": {"components": [
    {"id": "api", "variants": [{"type": "container", "image": "ghcr.io/o/r:${commit}"}]}
  ]}
}`

func TestParseRejectsBadFiles(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"not json", `{`, "parse definition file"},
		{"not an object", `[]`, "parse definition file"},
		{"nameless", `{"definition": {}}`, "has no name"},
		{"definitionless", `{"name": "a"}`, "has no definition"},
		{"null definition", `{"name": "a", "definition": null}`, "has no definition"},
		{"array definition", `{"name": "a", "definition": []}`, "has no definition"},
		{"string definition", `{"name": "a", "definition": "s"}`, "has no definition"},
		{"old envelope", `{"services": [{"name": "a", "definition": {}}]}`, "removed `services` envelope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseReturnsTheServiceAndIgnoresFieldsItHasNoUseFor(t *testing.T) {
	svc, err := Parse([]byte(oneService))
	if err != nil {
		t.Fatal(err)
	}
	if svc.Name != "leaf" {
		t.Fatalf("Name = %q, want leaf", svc.Name)
	}
	if !strings.Contains(string(svc.Definition), `"components"`) {
		t.Fatalf("Definition = %s, want the definition object", svc.Definition)
	}
}

func TestUnpinnedImagesSkipsWhatIsAlreadyPinned(t *testing.T) {
	def := json.RawMessage(`{"components":[
		{"id":"api","variants":[{"type":"container","image":"repo:tag"}]},
		{"id":"pinned-field","variants":[{"type":"container","image":"repo:tag","digest":"sha256:aa"}]},
		{"id":"pinned-ref","variants":[{"type":"container","image":"repo:tag@sha256:bb"}]},
		{"id":"db","variants":[{"type":"operated","engine":"postgres"}]},
		{"id":"no-image","variants":[{"type":"container"}]}
	]}`)
	d, err := Decode(def)
	if err != nil {
		t.Fatal(err)
	}
	images := d.UnpinnedImages()
	if len(images) != 1 || images[0].Component != "api" {
		t.Fatalf("UnpinnedImages = %+v, want only the api component", images)
	}

	images[0].SetDigest("sha256:cc")
	body, err := json.Marshal(d.Body())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"digest":"sha256:cc"`) {
		t.Fatalf("body %s, want the digest stamped beside the image", body)
	}
	if !strings.Contains(string(body), `"image":"repo:tag"`) {
		t.Fatalf("body %s, want the image reference left as written", body)
	}
}
