package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the real CLI in a child process without sharing flags or listeners
// with other tests, or requiring a separately built binary.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("JARGONARY_TEST_CLI") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"jargonary"}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("jargonary", flag.ExitOnError)
	main()
	os.Exit(0)
}

func cliProcess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "JARGONARY_TEST_CLI=1")
	return cmd
}

func TestValidateStdinCLI(t *testing.T) {
	for _, tc := range []struct {
		name, body, format string
		wantErr            bool
	}{
		{name: "JSON default", body: `{"version":"1","name":"Piped","entries":[]}`},
		{name: "YAML", body: "version: '1'\nname: Piped\nentries: []\n", format: "yaml"},
		{name: "invalid", body: "invalid", wantErr: true},
		{name: "empty", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"-validate", "-"}
			if tc.format != "" {
				args = append(args, "-format", tc.format)
			}
			cmd := cliProcess(t, args...)
			cmd.Stdin = strings.NewReader(tc.body)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantErr {
				t.Fatalf("exit = %v, output = %s", err, out)
			}
			if !tc.wantErr && !strings.Contains(string(out), "valid glossary: 0 entries") {
				t.Fatalf("unexpected validation output: %s", out)
			}
		})
	}
}

func TestServeStdinCLI(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("server:\n  listen: 127.0.0.1:0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			body := `{"version":"1","name":"Piped","entries":[{"id":"pipe","term":"Pipe","senses":[{"id":"one","definition":"From stdin."}]}]}`
			if format == "yaml" {
				body = "version: '1'\nname: Piped\nentries:\n  - id: pipe\n    term: Pipe\n    senses:\n      - id: one\n        definition: From stdin.\n"
			}
			cmd := cliProcess(t, "-config", path, "-f", "-")
			cmd.Stdin = strings.NewReader(body)
			cmd.Stderr = os.Stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Signal(os.Interrupt)
				if err := cmd.Wait(); err != nil {
					t.Errorf("server exit: %v", err)
				}
			})
			dec := json.NewDecoder(stdout)
			var address string
			for address == "" {
				var log struct {
					Interface string `json:"interface"`
					Address   string `json:"address"`
				}
				if err := dec.Decode(&log); err != nil {
					t.Fatalf("server did not start: %v", err)
				}
				if log.Interface == "public" {
					address = log.Address
				}
			}
			client := &http.Client{Timeout: 5 * time.Second}
			for _, path := range []string{"/api/v1/sources/glossary/entries/pipe", "/.well-known/glossary"} {
				resp, err := client.Get("http://" + address + path)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(path, "/api/") {
					if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), "From stdin.") {
						t.Fatalf("stdin snapshot unavailable: %d %s", resp.StatusCode, data)
					}
				} else if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("bundled glossary still published: %d", resp.StatusCode)
				}
			}
		})
	}
}

func TestServeStdinRejectsInvalidInput(t *testing.T) {
	for _, body := range []string{"", "invalid", `{"version":"1","name":"Larger than the limit","entries":[]}`} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "cache.db")
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("database: cache.db\nmax_bytes: 32\nserver:\n  listen: 127.0.0.1:0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := cliProcess(t, "-config", path, "-f", "-")
			cmd.Stdin = strings.NewReader(body)
			out, err := cmd.CombinedOutput()
			if err == nil || strings.Contains(string(out), "listener started") {
				t.Fatalf("invalid input started serving: %v, %s", err, out)
			}
			if _, err := os.Stat(db); !os.IsNotExist(err) {
				t.Fatalf("invalid input touched the database: %v", err)
			}
		})
	}
}
