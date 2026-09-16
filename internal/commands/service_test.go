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

func TestServicePublishRequiresFileAndServiceID(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no file", []string{"--service-id", "svc_1"}, "--file is required"},
		{"no service id", []string{"-f", "my-api.json"}, "--service-id is required"},
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
