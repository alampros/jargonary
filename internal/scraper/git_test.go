package scraper

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alampros/jargonary/internal/config"
	"github.com/alampros/jargonary/internal/store"
)

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitSourcePolling(t *testing.T) {
	// Isolate the test from the operator's Git config while exercising normal
	// Git URL rewriting, including expansion of the GitHub shorthand.
	dir := t.TempDir()
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	testGit(t, dir, "init", "--initial-branch=main", "--template=")
	testGit(t, dir, "config", "user.name", "Test")
	testGit(t, dir, "config", "user.email", "test@example.com")
	testGit(t, dir, "config", "commit.gpgsign", "false")
	testGit(t, dir, "config", "--global", "url.file://"+filepath.ToSlash(dir)+".insteadOf", "https://github.com/owner/repo.git")
	file := filepath.Join(dir, "glossary.yaml")
	commit := func(body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		testGit(t, dir, "add", "glossary.yaml")
		testGit(t, dir, "commit", "-m", "Update glossary")
	}
	commit("version: '1'\nname: Git glossary\nentries:\n  - id: git\n    term: Git\n    senses:\n      - id: vcs\n        definition: Version control.\n")
	testGit(t, dir, "tag", "original")
	original := testGit(t, dir, "rev-parse", "HEAD")
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := config.Config{Timeout: config.Duration(10 * time.Second), MaxBytes: 4096}
	s := New(c, st)
	source := config.Source{ID: "team", Git: "owner/repo", Path: "glossary.yaml", Format: "yaml"}
	ctx := context.Background()
	status := s.Once(ctx, source, store.Status{})
	if status.Failures != 0 || status.Entries != 1 {
		t.Fatalf("initial Git fetch: %+v", status)
	}
	status = s.Once(ctx, source, status)
	if status.Recent[len(status.Recent)-1].Result != "unchanged" {
		t.Fatalf("unchanged content: %+v", status)
	}
	commit("not a glossary")
	status = s.Once(ctx, source, status)
	if status.Failures != 1 || status.Entries != 1 {
		t.Fatalf("invalid update did not preserve snapshot: %+v", status)
	}
	commit("version: '1'\nname: Git glossary\nentries: []\n")
	status = s.Once(ctx, source, status)
	if status.Failures != 0 || status.Entries != 0 {
		t.Fatalf("empty update did not clear snapshot: %+v", status)
	}
	for _, ref := range []string{"main", "original", original} {
		source.Ref = ref
		status = s.Once(ctx, source, status)
		want := 1
		if ref == "main" {
			want = 0
		}
		if status.Failures != 0 || status.Entries != want {
			t.Fatalf("ref %s: %+v", ref, status)
		}
	}
	source.Git = "file://" + filepath.ToSlash(dir)
	source.Ref = "original"
	if status = s.Once(ctx, source, status); status.Failures != 0 {
		t.Fatalf("fully qualified URI: %+v", status)
	}
	for _, tc := range []struct{ path, ref string }{{"missing.yaml", "original"}, {"glossary.yaml", "missing-ref"}} {
		bad := source
		bad.Path, bad.Ref = tc.path, tc.ref
		failed := s.Once(ctx, bad, status)
		if failed.Failures != 1 || failed.Entries != 1 {
			t.Fatalf("missing file/ref lost snapshot: %+v", failed)
		}
	}
	s.Config.MaxBytes = 8
	if failed := s.Once(ctx, source, status); failed.Failures != 1 || failed.Recent[len(failed.Recent)-1].Detail != "source exceeds max_bytes" {
		t.Fatalf("size limit: %+v", failed)
	}
	s.Config.MaxBytes = 4096
	if err := os.Symlink("glossary.yaml", filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "link.yaml")
	testGit(t, dir, "commit", "-m", "Add symlink")
	source.Path, source.Ref = "link.yaml", "main"
	if _, err := s.fetchGit(ctx, source); err == nil {
		t.Fatal("accepted symlink")
	}
	source.Git = "file:///missing/secret-token"
	failed := s.Once(ctx, source, status)
	out, _ := json.Marshal(failed)
	if failed.Failures != 1 || strings.Contains(string(out), "secret-token") {
		t.Fatalf("failed fetch or URI redaction: %s", out)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.fetchGit(canceled, source); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestGitRepository(t *testing.T) {
	for input, want := range map[string]string{
		"alampros/jargonary":              "https://github.com/alampros/jargonary.git",
		"owner/.github":                   "https://github.com/owner/.github.git",
		"owner/repo.git":                  "https://github.com/owner/repo.git",
		"git@github.com:owner/repo.git":   "git@github.com:owner/repo.git",
		"ssh://git@example.com/team/repo": "ssh://git@example.com/team/repo",
		"https://example.com/repo.git":    "https://example.com/repo.git",
		"git://example.com/repo":          "git://example.com/repo",
		"custom::repository":              "custom::repository",
	} {
		if got := gitRepository(input); got != want {
			t.Errorf("gitRepository(%q) = %q, want %q", input, got, want)
		}
	}
}
