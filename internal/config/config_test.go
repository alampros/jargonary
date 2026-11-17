package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDatabaseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"default", "sources: []\n", ":memory:"},
		{"explicit memory", "database: ':memory:'\nsources: []\n", ":memory:"},
		{"relative file", "database: data/glossary.db\nsources: []\n", "data/glossary.db"},
		{"absolute file", "database: /data/glossary.db\nsources: []\n", "/data/glossary.db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.body)
			c, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if want != ":memory:" && !filepath.IsAbs(want) {
				want = filepath.Join(filepath.Dir(path), want)
			}
			if c.Database != want {
				t.Fatalf("database = %q, want %q", c.Database, want)
			}
		})
	}
}

func TestInputFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  string
		format  string
		wantErr bool
	}{
		{name: "relative YAML", source: "terms/team.yaml", format: "yaml"},
		{name: "absolute JSON", source: "/tmp/team.json", format: "json"},
		{name: "YML", source: "team.YML", format: "yml"},
		{name: "extensionless JSON", source: "glossary", format: "json"},
		{name: "stdin", source: "-", format: "json"},
		{name: "unsupported format", source: "team.csv", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			// Conflicting discovered files must not affect an explicit source.
			for _, name := range []string{"glossary.yaml", "glossary.json"} {
				if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"", writeConfig(t, "site_name: Selected glossary\nterms_dir: missing\nsources:\n  - id: ignored\n    url: https://ignored.test/glossary\n    auth:\n      shared_secret:\n        env: UNUSED_SOURCE_SECRET\n")} {
				c, err := LoadWithInputFile(path, tc.source)
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected invalid source error")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(c.Sources) != 1 || c.Sources[0].ID != "glossary" || c.Sources[0].Format != tc.format || c.IncludeProjectGlossary() {
					t.Fatalf("unexpected single-source configuration: %+v", c)
				}
				s := c.Sources[0]
				want := tc.source
				if want != "-" && !filepath.IsAbs(want) {
					want = filepath.Join(dir, want)
				}
				if s.File != want || s.URL != "" {
					t.Fatalf("unexpected file source: %+v; want %s", s, want)
				}
				if s.ScrapeInterval != c.ScrapeInterval || c.Origins["sources"] != "command-line -input-file" {
					t.Fatal("source defaults or origin missing")
				}
				if path != "" && c.SiteName != "Selected glossary" {
					t.Fatal("other configuration settings were lost")
				}
			}
		})
	}
}

func TestInputFormatOverride(t *testing.T) {
	for _, input := range []string{"-", "glossary.json", "glossary.data"} {
		c, err := LoadWithInput("", input, "yaml")
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Sources) != 1 || c.Sources[0].Format != "yaml" {
			t.Fatalf("format override lost for %q: %+v", input, c.Sources)
		}
		if _, err := LoadWithInput("", input, "csv"); err == nil {
			t.Fatalf("accepted unsupported format for %q", input)
		}
	}
}

func TestDefaultsAndSecretRedaction(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database != ":memory:" {
		t.Fatalf("default database = %q, want in-memory", c.Database)
	}
	if c.Admin.Enabled || c.Server.Listen != "0.0.0.0:8080" || c.Admin.Listen != "127.0.0.1:9090" || c.MCP.Enabled || c.MCP.Listen != "127.0.0.1:9091" {
		t.Fatal("unsafe listener defaults")
	}
	secret := strings.Repeat("secret", 8)
	t.Setenv("EXPORT_SECRET", secret)
	p := writeConfig(t, `admin:
  enabled: true
sources:
  - id: payments
    url: https://example.test/terms?credential=hidden-query
    auth:
      shared_secret:
        env: EXPORT_SECRET
exports:
  - id: public
    name: Public
    include:
      sources: [payments]
      sense_tags:
        any: [public]
    auth:
      shared_secret:
        env: EXPORT_SECRET
`)
	c, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Sources[0].Token != secret || c.Exports[0].Token != secret {
		t.Fatal("secret was not resolved")
	}
	b, err := json.Marshal(c.Diagnostics())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), "hidden-query") {
		t.Fatal("diagnostics leaked credentials")
	}
	if c.Origins["admin.enabled"] != "configuration file" {
		t.Fatal("missing setting origin")
	}
}

func TestRejectUnsafeConfiguration(t *testing.T) {
	for _, body := range []string{
		"admni:\n  enabled: true\n",
		"admin:\n  enabled: true\n  listen: '0.0.0.0:8080'\n",
		"mcp:\n  enabled: true\n  listen: '0.0.0.0:8080'\n",
		"mcp:\n  enabled: true\n  listen: '127.0.0.1:9090'\n",
		"sources:\n  - id: project\n    file: terms.json\n",
		"exports:\n  - id: public\n    name: Public\n    include: all\n",
		"exports:\n  - id: public\n    name: Public\n    include:\n      typo: true\n    auth:\n      public: true\n",
		"exports:\n  - id: public\n    name: Public\n    include:\n      sense_tags:\n        any: [public]\n      sources: [missing]\n    auth:\n      public: true\n",
	} {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Errorf("accepted invalid configuration: %s", body)
		}
	}
}

func TestRejectProjectExportSelector(t *testing.T) {
	for _, hidden := range []string{"false", "true"} {
		body := "hide_jargonary_glossary: " + hidden + "\nsources:\n  - id: company\n    file: company.json\nexports:\n  - id: public\n    name: Public\n    include:\n      sense_tags:\n        any: [public]\n      sources: [project]\n    auth:\n      public: true\n"
		if _, err := Load(writeConfig(t, body)); err == nil || !strings.Contains(err.Error(), "Jargonary project terms cannot be included in exports") {
			t.Fatalf("project export selector should be rejected: %v", err)
		}
	}
}

func TestProjectGlossaryInclusion(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"default with company", "sources:\n  - id: company\n    file: company.json\n", false},
		{"explicit false with company", "hide_jargonary_glossary: false\nsources:\n  - id: company\n    file: company.json\n", false},
		{"hidden with company", "hide_jargonary_glossary: true\nsources:\n  - id: company\n    file: company.json\n", false},
		{"empty default", "sources: []\n", true},
		{"empty fallback", "hide_jargonary_glossary: true\nsources: []\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeConfig(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if c.IncludeProjectGlossary() != tc.want {
				t.Fatalf("project inclusion = %v, want %v", c.IncludeProjectGlossary(), tc.want)
			}
			if c.HideJargonaryGlossary && (c.Diagnostics()["hide_jargonary_glossary"] != true || c.Origins["hide_jargonary_glossary"] != "configuration file") {
				t.Fatal("hide setting missing from resolved configuration or origins")
			}
		})
	}
	path := writeConfig(t, "terms_dir: terms\n")
	if err := os.Mkdir(filepath.Join(filepath.Dir(path), "terms"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "terms/company.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.IncludeProjectGlossary() {
		t.Fatalf("discovered company source should suppress fallback: %+v %v", c, err)
	}
	if _, err := Load(writeConfig(t, "hide_jargonary_glossary: true\nsources:\n  - id: company\n    file: company.json\nexports:\n  - id: public\n    name: Public\n    include:\n      all: true\n      sources: [project]\n    auth:\n      public: true\n")); err == nil {
		t.Fatal("export accepted a disabled project source")
	}
}

func TestExplicitEmptySourcesDisablesDiscovery(t *testing.T) {
	dir := t.TempDir()
	terms := filepath.Join(dir, "terms")
	if err := os.Mkdir(terms, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(terms, "company.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("terms_dir: terms\nsources: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sources) != 0 {
		t.Fatal("empty sources unexpectedly rediscovered files")
	}
	if err := os.WriteFile(p, []byte("terms_dir: terms\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil || len(c.Sources) != 1 || c.Sources[0].ID != "company" {
		t.Fatalf("startup discovery: %+v %v", c.Sources, err)
	}
}

func TestWorkingDirectoryDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []string
		settings string
		wantID   string
		format   string
		wantErr  bool
	}{
		{name: "yaml without config", files: []string{"glossary.yaml"}, wantID: "glossary", format: "yaml"},
		{name: "json without config", files: []string{"glossary.json"}, wantID: "glossary", format: "json"},
		{name: "config elsewhere", files: []string{"glossary.yaml"}, settings: "terms_dir: missing\n", wantID: "glossary", format: "yaml"},
		{name: "explicit empty", files: []string{"glossary.yaml"}, settings: "sources: []\n"},
		{name: "explicit source", files: []string{"glossary.json"}, settings: "sources:\n  - id: company\n    file: company.json\n", wantID: "company", format: "json"},
		{name: "unrelated files", files: []string{"other.yaml", "glossary.csv", "glossary.yml"}},
		{name: "duplicate formats", files: []string{"glossary.yaml", "glossary.json"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			for _, name := range tc.files {
				if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			path := ""
			if tc.settings != "" {
				path = writeConfig(t, tc.settings)
			}
			c, err := Load(path)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "duplicate") {
					t.Fatalf("expected duplicate source error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantID == "" {
				if len(c.Sources) != 0 {
					t.Fatalf("unexpected sources: %+v", c.Sources)
				}
				return
			}
			if len(c.Sources) != 1 || c.Sources[0].ID != tc.wantID || c.Sources[0].Format != tc.format {
				t.Fatalf("unexpected sources: %+v", c.Sources)
			}
			if c.IncludeProjectGlossary() {
				t.Fatal("configured or discovered source must disable the bundled project glossary")
			}
			if tc.wantID == "glossary" && c.Sources[0].File != filepath.Join(dir, "glossary."+tc.format) {
				t.Fatalf("glossary did not resolve from cwd: %+v", c.Sources[0])
			}
		})
	}
}

func TestWorkingDirectoryDiscoveryWithTermsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"glossary.yaml", "company.json"} {
		if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, termsDir := range []string{dir, filepath.Join(dir, "terms")} {
		if termsDir != dir {
			if err := os.Mkdir(termsDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(termsDir, "company.json"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		c, err := Load(writeConfig(t, "terms_dir: "+termsDir+"\n"))
		if err != nil || len(c.Sources) != 2 || c.Sources[0].ID != "company" || c.Sources[1].ID != "glossary" {
			t.Fatalf("expected distinct company and glossary sources: %+v %v", c.Sources, err)
		}
	}
}

func TestWorkingDirectoryDiscoverySkipsNonRegularFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("glossary.yaml", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("glossary.yaml", "glossary.json"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(writeConfig(t, "terms_dir: missing\n"))
	if err != nil || len(c.Sources) != 0 {
		t.Fatalf("expected non-regular files to be ignored: %+v %v", c.Sources, err)
	}
}

func TestScrapeIntervalBounds(t *testing.T) {
	for _, scope := range []string{"global", "source"} {
		for _, tc := range []struct {
			interval string
			valid    bool
		}{
			{"1s", false},
			{"59s", false},
			{"1m", true},
			{"5m", true},
			{"24h", true},
			{"24h1s", false},
		} {
			t.Run(scope+"/"+tc.interval, func(t *testing.T) {
				body := "sources:\n  - id: company\n    file: company.json\n"
				if scope == "global" {
					body = fmt.Sprintf("scrape_interval: %s\n", tc.interval) + body
				} else {
					body += fmt.Sprintf("    scrape_interval: %s\n", tc.interval)
				}
				c, err := Load(writeConfig(t, body))
				if !tc.valid {
					if err == nil || !strings.Contains(err.Error(), "1m") {
						t.Fatalf("expected interval bounds error, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want, err := time.ParseDuration(tc.interval)
				if err != nil {
					t.Fatal(err)
				}
				if c.Sources[0].ScrapeInterval != Duration(want) {
					t.Fatalf("source interval = %v, want %v", c.Sources[0].ScrapeInterval, want)
				}
			})
		}
	}
}

func TestUnsupportedSourceFormat(t *testing.T) {
	for _, source := range []Source{
		{ID: "team", File: "terms.csv"},
		{ID: "team", File: "terms.json", Format: "csv"},
		{ID: "team", URL: "https://example.com/terms.csv"},
	} {
		c, err := Load(writeConfig(t, "sources: []\n"))
		if err != nil {
			t.Fatal(err)
		}
		c.Sources = []Source{source}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported source format") {
			t.Fatalf("unsupported source format accepted: %+v %v", source, err)
		}
	}
}

func TestDiscoveryOnlyIncludesJSONAndYAML(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"team.json", "engineering.yaml", "product.yml", "ignored.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := writeConfig(t, "terms_dir: "+dir+"\n")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sources) != 3 {
		t.Fatalf("expected only JSON/YAML files: %+v", c.Sources)
	}
	for _, source := range c.Sources {
		if source.Format == "csv" {
			t.Fatal("discovered unsupported file")
		}
	}
}

func TestGitSources(t *testing.T) {
	for _, tc := range []struct {
		name, fields, path, format string
		invalid                    bool
	}{
		{name: "shorthand", fields: "git: alampros/jargonary", path: "glossary.yaml", format: "yaml"},
		{name: "HTTPS", fields: "git: https://example.com/team/repo.git\n    path: glossary/project.json", path: "glossary/project.json", format: "json"},
		{name: "SSH", fields: "git: git@example.com:team/repo.git\n    ref: main", path: "glossary.yaml", format: "yaml"},
		{name: "custom transport", fields: "git: custom::repository", path: "glossary.yaml", format: "yaml"},
		{name: "format override", fields: "git: owner/repo\n    path: terms\n    format: yaml", path: "terms", format: "yaml"},
		{name: "conflicting file", fields: "git: owner/repo\n    file: terms.json", invalid: true},
		{name: "conflicting URL", fields: "git: owner/repo\n    url: https://example.com", invalid: true},
		{name: "path without Git", fields: "file: terms.json\n    path: glossary.yaml", invalid: true},
		{name: "ref without Git", fields: "file: terms.json\n    ref: main", invalid: true},
		{name: "traversal", fields: "git: owner/repo\n    path: ../terms.yaml", invalid: true},
		{name: "absolute path", fields: "git: owner/repo\n    path: /terms.yaml", invalid: true},
		{name: "option repository", fields: "git: --upload-pack=bad", invalid: true},
		{name: "option ref", fields: "git: owner/repo\n    ref: --upload-pack=bad", invalid: true},
		{name: "blank repository", fields: "git: ' '", invalid: true},
		{name: "shared secret", fields: "git: owner/repo\n    auth: {shared_secret: {env: UNUSED_SECRET}}", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(writeConfig(t, "sources:\n  - id: team\n    "+tc.fields+"\n"))
			if tc.invalid {
				if err == nil {
					t.Fatal("expected validation failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Sources[0].Path != tc.path || c.Sources[0].Format != tc.format {
				t.Fatalf("unexpected Git source: %+v", c.Sources[0])
			}
			out, _ := json.Marshal(c.Diagnostics())
			if strings.Contains(string(out), c.Sources[0].Git) {
				t.Fatalf("diagnostics expose Git repository: %s", out)
			}
		})
	}
}
