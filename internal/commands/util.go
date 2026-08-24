package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// parseKV turns []{"KEY=VALUE"} into a map, erroring on malformed entries.
func parseKV(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --set %q: expected KEY=VALUE", p)
		}
		out[k] = v
	}
	return out, nil
}

// render prints v as indented JSON.
func render(cmd *cobra.Command, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}

// safePrefix returns a short, non-secret prefix of a token for status output.
func safePrefix(tok string) string {
	if len(tok) <= 8 {
		return "****"
	}
	return tok[:8]
}
