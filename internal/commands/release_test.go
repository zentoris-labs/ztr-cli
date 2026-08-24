package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestReleaseCreateRequiresService(t *testing.T) {
	cmd := newReleaseCreateCmd(&deps{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--service is required") {
		t.Fatalf("err = %v, want '--service is required'", err)
	}
}

func TestReleaseListRequiresService(t *testing.T) {
	cmd := newReleaseListCmd(&deps{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--service is required") {
		t.Fatalf("err = %v, want '--service is required'", err)
	}
}
