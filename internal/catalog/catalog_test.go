package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const oneService = `{
  "systems": ["ignored"],
  "services": [
    {"name": "leaf", "definition": {"components": [
      {"id": "api", "variants": [{"type": "container", "image": "ghcr.io/o/r:${commit}"}]}
    ]}}
  ]
}`

func TestParseRejectsBadCatalogs(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"not json", `{`, "parse definition file"},
		{"no services", `{"services": []}`, "no services"},
		{"nameless", `{"services": [{"definition": {}}]}`, "has no name"},
		{"definitionless", `{"services": [{"name": "a"}]}`, "has no definition"},
		{"duplicate", `{"services": [{"name":"a","definition":{}},{"name":"a","definition":{}}]}`, "appears twice"},
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

func TestParseIgnoresSectionsItHasNoUseFor(t *testing.T) {
	f, err := Parse([]byte(oneService))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(f.Services))
	}
}

func TestSingleReturnsTheOneService(t *testing.T) {
	f, err := Parse([]byte(oneService))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := f.Single()
	if err != nil {
		t.Fatal(err)
	}
	if svc.Name != "leaf" {
		t.Fatalf("Single = %q, want leaf", svc.Name)
	}
}

// A file with two services must be refused rather than silently publishing the first onto the one
// id the caller gave: that would put the wrong definition on a real service, with no error to read.
func TestSingleRefusesMoreThanOneService(t *testing.T) {
	f, err := Parse([]byte(`{"services":[
		{"name":"a","definition":{"components":[]}},
		{"name":"b","definition":{"components":[]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Single(); err == nil || !strings.Contains(err.Error(), "has 2") {
		t.Fatalf("err = %v, want the count named", err)
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
