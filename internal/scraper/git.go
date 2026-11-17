package scraper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/alampros/jargonary/internal/config"
)

var githubShorthand = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)

func gitRepository(repository string) string {
	if githubShorthand.MatchString(repository) {
		return "https://github.com/" + strings.TrimSuffix(repository, ".git") + ".git"
	}
	return repository
}

func (s *Scraper) fetchGit(ctx context.Context, source config.Source) ([]byte, error) {
	// Each attempt uses an isolated bare repository: no working tree, checkout
	// filters, submodules, or repository-provided hooks are executed.
	dir, err := os.MkdirTemp("", "jargonary-git-*")
	if err != nil {
		return nil, errors.New("cannot create temporary Git repository")
	}
	defer os.RemoveAll(dir)
	if _, err := gitOutput(ctx, dir, 0, "init", "--bare", "--template=", "."); err != nil {
		return nil, errors.New("cannot initialize Git source; ensure git is available on PATH")
	}
	ref := source.Ref
	if ref == "" {
		ref = "HEAD"
	}
	if _, err := gitOutput(ctx, dir, 0, "fetch", "--depth=1", "--no-tags", "--", gitRepository(source.Git), ref); err != nil {
		return nil, errors.New("Git fetch failed; retaining last successful snapshot")
	}
	// Inspect the tree entry before reading it. In particular, a symlink blob
	// contains a target path, not glossary content, and must never be followed.
	entry, err := gitOutput(ctx, dir, 4096, "ls-tree", "-z", "--full-tree", "FETCH_HEAD", "--", ":(literal)"+source.Path)
	if err != nil {
		return nil, errors.New("cannot inspect Git glossary file")
	}
	header, _, ok := strings.Cut(string(entry), "\t")
	fields := strings.Fields(header)
	if !ok || len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return nil, errors.New("Git glossary path must identify a committed regular file")
	}
	body, err := gitOutput(ctx, dir, s.Config.MaxBytes, "cat-file", "blob", fields[2])
	if errors.Is(err, errGitOutputLimit) {
		return nil, errors.New("source exceeds max_bytes")
	}
	if err != nil {
		return nil, errors.New("cannot read Git glossary file")
	}
	return body, nil
}

var errGitOutputLimit = errors.New("Git output exceeds limit")

// Do not forward Git errors or stderr: remote URIs and helpers can expose secrets.
// Configuration and environment are inherited so Git owns auth and routing.
func gitOutput(ctx context.Context, dir string, limit int64, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	if limit == 0 {
		cmd.Stdout = io.Discard
		return nil, cmd.Run()
	}
	output := &gitBuffer{remaining: limit, cancel: cancel}
	cmd.Stdout = output
	err := cmd.Run()
	if output.exceeded {
		return nil, errGitOutputLimit
	}
	return output.buffer.Bytes(), err
}

type gitBuffer struct {
	buffer    bytes.Buffer
	remaining int64
	cancel    context.CancelFunc
	exceeded  bool
}

func (b *gitBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.remaining {
		b.exceeded = true
		b.cancel()
		return 0, errGitOutputLimit
	}
	b.remaining -= int64(len(p))
	return b.buffer.Write(p)
}
