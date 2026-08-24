package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseKV(t *testing.T) {
	got, err := parseKV([]string{"A=1", "B=two=parts", "C="})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two=parts", "C": ""}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseKVInvalid(t *testing.T) {
	for _, bad := range []string{"noequals", "=novalue"} {
		if _, err := parseKV([]string{bad}); err == nil {
			t.Errorf("parseKV(%q) = nil error, want an error", bad)
		}
	}
}

func TestSafePrefix(t *testing.T) {
	if got := safePrefix("short"); got != "****" {
		t.Errorf("safePrefix(short) = %q, want ****", got)
	}
	if got := safePrefix("abcdefghijkl"); got != "abcdefgh" {
		t.Errorf("safePrefix(long) = %q, want abcdefgh", got)
	}
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	if err := render(cmd, map[string]any{"id": "r1"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, `"id": "r1"`) {
		t.Fatalf("render output = %q, want indented JSON containing the id", out)
	}
}
