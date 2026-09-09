package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const twoServices = `{
  "systems": ["ignored"],
  "services": [
    {"name": "umbrella", "definition": {"components": [
      {"id": "auth", "variants": [{"type": "service", "serviceId": "${serviceId:leaf}", "versionId": "${versionId:leaf}"}]}
    ]}},
    {"name": "leaf", "definition": {"components": [
      {"id": "api", "variants": [{"type": "container", "image": "ghcr.io/o/r:${commit}"}]}
    ]}}
  ]
}`

func TestParseRejectsBadCatalogs(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"not json", `{`, "parse catalog"},
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

func TestParseIgnoresSeedOnlySections(t *testing.T) {
	f, err := Parse([]byte(twoServices))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(f.Services))
	}
}

func TestReferences(t *testing.T) {
	got := References(json.RawMessage(`{"a":"${serviceId:x}","b":"${versionId:x}","c":"${versionId:y}","d":"${commit}"}`))
	want := []string{"x", "y"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("References = %v, want %v (deduplicated, no ${commit})", got, want)
	}
}

func TestInDependencyOrderIsLeafFirst(t *testing.T) {
	f, err := Parse([]byte(twoServices))
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := f.InDependencyOrder()
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Name != "leaf" || ordered[1].Name != "umbrella" {
		t.Fatalf("order = %s, %s; want leaf before umbrella", ordered[0].Name, ordered[1].Name)
	}
}

func TestInDependencyOrderRejectsCycles(t *testing.T) {
	cases := map[string]string{
		"pair": `{"services":[
			{"name":"a","definition":{"r":"${versionId:b}"}},
			{"name":"b","definition":{"r":"${versionId:a}"}}]}`,
		"self": `{"services":[{"name":"a","definition":{"r":"${versionId:a}"}}]}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			f, err := Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.InDependencyOrder(); err == nil {
				t.Fatal("expected a cycle error")
			}
		})
	}
}

func TestResolveSubstitutesOnlyItsOwnTokens(t *testing.T) {
	in := json.RawMessage(`{"s":"${serviceId:leaf}","v":"${versionId:leaf}","img":"repo:${commit}"}`)
	out, err := Resolve(in, func(kind, name string) (string, error) {
		return kind + "-of-" + name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{`"s":"serviceId-of-leaf"`, `"v":"versionId-of-leaf"`, `"img":"repo:${commit}"`} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved %s missing %s", got, want)
		}
	}
}

func TestResolveReportsAnUnresolvableReference(t *testing.T) {
	_, err := Resolve(json.RawMessage(`{"v":"${versionId:missing}"}`), func(_, name string) (string, error) {
		return "", fmt.Errorf("%s is not in this catalog", name)
	})
	if err == nil || !strings.Contains(err.Error(), "${versionId:missing}") {
		t.Fatalf("err = %v, want the offending token named", err)
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
