package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/alampros/jargonary/glossary"
	"github.com/alampros/jargonary/internal/config"
	"github.com/alampros/jargonary/internal/mcp"
	"github.com/alampros/jargonary/internal/scraper"
	"github.com/alampros/jargonary/internal/server"
	"github.com/alampros/jargonary/internal/store"
	"github.com/alampros/jargonary/internal/vocabulary"
)

func main() {
	slog.SetDefault(newLogger(os.Stdout, term.IsTerminal(int(os.Stdout.Fd()))))
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "YAML configuration file")
	var inputFile string
	flag.StringVar(&inputFile, "input-file", "", "serve a single glossary file (- for stdin) instead of configured or discovered sources")
	flag.StringVar(&inputFile, "f", "", "alias for -input-file")
	devUI := flag.Bool("dev-ui", false, "load frontend modules from local Vite (run from repository root)")
	validate := flag.String("validate", "", "validate a glossary file (- for stdin) and exit")
	format := flag.String("format", "", "glossary format for -validate or -input-file: json, yaml (default auto-detect; input files infer from extension)")
	schema := flag.Bool("schema", false, "print the structural dictionary JSON Schema and exit")
	configSchema := flag.Bool("config-schema", false, "print the configuration JSON Schema and exit")
	schemaVersion := flag.String("schema-version", "", "release version for schema generation (required with -schema or -config-schema)")
	flag.Parse()
	if *configSchema || *schema {
		return writeSchema(os.Stdout, *schemaVersion, *configSchema)
	}
	if *validate != "" {
		if inputFile != "" {
			return errors.New("-validate and -input-file cannot be combined")
		}
		d, err := readGlossaryInput(*validate, *format, os.Stdin, 0)
		if err != nil {
			return err
		}
		fmt.Printf("valid glossary: %d entries\n", len(d.Entries))
		return nil
	}
	c, err := config.LoadWithInput(*configPath, inputFile, *format)
	if err != nil {
		return err
	}
	var stdinGlossary *vocabulary.Dictionary
	if inputFile == "-" {
		d, err := readGlossaryInput("-", c.Sources[0].Format, os.Stdin, c.MaxBytes)
		if err != nil {
			return err
		}
		stdinGlossary = &d
	}
	logResolvedConfiguration(slog.Default(), c, *devUI)
	st, err := store.Open(c.Database)
	if err != nil {
		return errors.New("cannot initialize SQLite database; verify path, permissions, and schema compatibility")
	}
	defer st.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := initializeSources(ctx, c, st); err != nil {
		return err
	}
	if stdinGlossary != nil {
		if _, err := st.Replace(ctx, "glossary", *stdinGlossary, "", ""); err != nil {
			return err
		}
	}
	svc := &server.Server{Config: c, Store: st}
	if *devUI {
		svc.DevUI = os.DirFS("ui")
		slog.Info("development UI enabled", "vite", "http://127.0.0.1:5173")
	}
	type listener struct {
		name string
		http *http.Server
		net  net.Listener
	}
	var listeners []listener
	add := func(name, addr string, handler http.Handler) error {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("cannot bind %s listener; verify address availability", name)
		}
		listeners = append(listeners, listener{name, &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}, ln})
		return nil
	}
	defer func() {
		for _, l := range listeners {
			_ = l.net.Close()
		}
	}()
	if err := add("public", c.Server.Listen, svc.Public()); err != nil {
		return err
	}
	if c.Admin.Enabled {
		if err := add("admin", c.Admin.Listen, svc.Admin()); err != nil {
			return err
		}
		slog.Info("admin diagnostics enabled; restrict network access")
	}
	if c.MCP.Enabled {
		if err := add("mcp", c.MCP.Listen, mcp.New(st)); err != nil {
			return err
		}
		slog.Info("mcp server enabled; restrict network access")
	}
	errs := make(chan error, len(listeners))
	for _, l := range listeners {
		go func() {
			logListenerStarted(slog.Default(), l.name, l.net.Addr(), c.PublicURL)
			errs <- l.http.Serve(l.net)
		}()
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if err := scraper.New(c, st).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("scraper stopped")
			errs <- errors.New("scraper failed")
		}
	}()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
		stop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, l := range listeners {
		if err := l.http.Shutdown(shutdownCtx); err != nil {
			_ = l.http.Close()
		}
	}
	<-workerDone
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("application worker or listener failed")
	}
	slog.Info("shutdown complete")
	return nil
}

// readGlossaryInput reads one complete document. A zero limit preserves the
// validation command's unrestricted file size; serving uses configured max_bytes.
func readGlossaryInput(path, format string, stdin io.Reader, maxBytes int64) (vocabulary.Dictionary, error) {
	var empty vocabulary.Dictionary
	r := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return empty, errors.New("cannot read glossary input")
		}
		defer f.Close()
		r = f
	}
	if maxBytes > 0 {
		r = io.LimitReader(r, maxBytes+1)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return empty, errors.New("cannot read glossary input")
	}
	if maxBytes > 0 && int64(len(b)) > maxBytes {
		return empty, errors.New("glossary input exceeds max_bytes")
	}
	return vocabulary.Parse(b, format)
}

func initializeSources(ctx context.Context, c config.Config, st *store.Store) error {
	ids := []string{}
	for _, source := range c.Sources {
		ids = append(ids, source.ID)
	}
	if c.IncludeProjectGlossary() {
		ids = append(ids, "project")
	}
	if err := st.Keep(ctx, ids); err != nil {
		return err
	}
	if !c.IncludeProjectGlossary() {
		return nil
	}
	d, err := vocabulary.Parse(glossary.JSON, "json")
	if err != nil {
		return fmt.Errorf("embedded glossary: %w", err)
	}
	_, err = st.Replace(ctx, "project", d, "", "")
	return err
}
