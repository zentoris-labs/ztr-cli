package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestIfMatchOrNone(t *testing.T) {
	if got := ifMatchOrNone(""); got != "(none)" {
		t.Errorf(`ifMatchOrNone("") = %q, want (none)`, got)
	}
	if got := ifMatchOrNone("etag"); got != "etag" {
		t.Errorf("ifMatchOrNone(etag) = %q, want etag", got)
	}
}

func TestServiceUpdateDryRun(t *testing.T) {
	cmd := newServiceUpdateCmd(&deps{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"svc_1", "--set", "A=B", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"DRY RUN", "PATCH /services/svc_1", "If-Match: (none)", "A:B"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output %q missing %q", out, want)
		}
	}
}

func TestServiceUpdateDryRunWithIfMatchAndRelease(t *testing.T) {
	cmd := newServiceUpdateCmd(&deps{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"svc_1", "--set", "A=B", "--if-match", "*", "--release", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "If-Match: *") {
		t.Errorf("output %q missing 'If-Match: *'", out)
	}
	if !strings.Contains(out, "POST  /services/svc_1/releases") {
		t.Errorf("output %q missing the release POST line", out)
	}
}

func TestServiceUpdateBadSet(t *testing.T) {
	cmd := newServiceUpdateCmd(&deps{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"svc_1", "--set", "bogus", "--dry-run"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a malformed --set")
	}
}
