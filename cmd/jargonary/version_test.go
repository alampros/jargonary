package main

import (
	"path/filepath"
	"testing"
)

func TestVersionCLI(t *testing.T) {
	for _, name := range []string{"-v", "-version"} {
		t.Run(name, func(t *testing.T) {
			// Version output must not depend on valid configuration, schema
			// options, or reading glossary input from stdin.
			cmd := cliProcess(t, name, "-config", filepath.Join(t.TempDir(), "missing.yaml"), "-schema", "-f", "-")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("version command failed: %v", err)
			}
			if want := "jargonary " + version + "\n"; string(out) != want {
				t.Fatalf("output = %q, want %q", out, want)
			}
		})
	}
}
