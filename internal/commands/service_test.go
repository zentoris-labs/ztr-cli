package commands

import (
	"strings"
	"testing"
)

func TestServiceListRequiresOrg(t *testing.T) {
	_, err := run1(t, newServiceListCmd(&deps{}))
	if err == nil || !strings.Contains(err.Error(), "--org is required") {
		t.Fatalf("err = %v, want the missing --org error", err)
	}
}

func TestServicePublishRequiresFileAndOrg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no file", []string{"--org", "org_1"}, "--file is required"},
		{"no org", []string{"-f", "catalog.json"}, "--org is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run1(t, newServicePublishCmd(&deps{}), tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
