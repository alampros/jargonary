package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alampros/jargonary/internal/config"
	"github.com/alampros/jargonary/internal/server"
	"github.com/alampros/jargonary/internal/store"
	"github.com/alampros/jargonary/internal/vocabulary"
)

func TestReadGlossaryInput(t *testing.T) {
	json := `{"version":"1","name":"Piped glossary","entries":[]}`
	yaml := "version: '1'\nname: Piped glossary\nentries: []\n"
	for _, tc := range []struct {
		name, body, format string
		limit              int64
		wantErr            bool
	}{
		{name: "default JSON", body: json},
		{name: "explicit JSON", body: json, format: "json"},
		{name: "YAML", body: yaml, format: "yaml"},
		{name: "exact size limit", body: json, limit: int64(len(json))},
		{name: "over limit", body: json, limit: int64(len(json) - 1), wantErr: true},
		{name: "empty input", wantErr: true},
		{name: "malformed", body: "{", wantErr: true},
		{name: "semantic error", body: `{"version":"1","name":"Piped glossary","entries":[{"id":"bad","term":"Bad","senses":[]}]}`, wantErr: true},
		{name: "multiple JSON documents", body: json + json, wantErr: true},
		{name: "multiple YAML documents", body: yaml + "---\n" + yaml, format: "yaml", wantErr: true},
		{name: "unsupported format", body: json, format: "csv", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := readGlossaryInput("-", tc.format, strings.NewReader(tc.body), tc.limit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && (d.Name != "Piped glossary" || len(d.Entries) != 0) {
				t.Fatalf("unexpected glossary: %+v", d)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte(json), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGlossaryInput(path, "json", failingReader{}, 0); err != nil {
		t.Fatalf("file input should not read stdin: %v", err)
	}
	if _, err := readGlossaryInput(path+".missing", "json", nil, 0); err == nil {
		t.Fatal("accepted missing input file")
	}
	if _, err := readGlossaryInput("-", "json", failingReader{}, 0); err == nil || strings.Contains(err.Error(), "private payload") {
		t.Fatalf("read error missing or leaked input: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("private payload")
}

func TestProjectGlossaryStartupTransitions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "glossary.db")
	company := vocabulary.Dictionary{Version: "1", Name: "Company", Entries: []vocabulary.Entry{{ID: "company-term", Term: "Company term", Senses: []vocabulary.Sense{{ID: "one", Definition: "Company definition."}}}}}
	for _, tc := range []struct {
		name        string
		hide        bool
		sources     []config.Source
		wantProject bool
	}{
		{"default", false, []config.Source{{ID: "company"}}, true},
		{"hidden on restart", true, []config.Source{{ID: "company"}}, false},
		{"reenabled on restart", false, []config.Source{{ID: "company"}}, true},
		{"empty fallback", true, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if len(tc.sources) > 0 {
				if _, err := st.Replace(ctx, "company", company, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			c := config.Config{HideJargonaryGlossary: tc.hide, Sources: tc.sources}
			if err := initializeSources(ctx, c, st); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{"", "entry"} {
				hits, err := st.Search(ctx, query, "project", 100, 0)
				if err != nil || (len(hits) > 0) != tc.wantProject {
					t.Fatalf("cached project query %q after restart: %d hits, %v", query, len(hits), err)
				}
			}
			snapshots, err := st.Snapshots(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := len(tc.sources)
			if tc.wantProject {
				want++
			}
			if len(snapshots) != want {
				t.Fatalf("source snapshots = %d, want %d", len(snapshots), want)
			}
			svc := &server.Server{Config: c, Store: st}
			w := httptest.NewRecorder()
			svc.Public().ServeHTTP(w, httptest.NewRequest("GET", "/.well-known/glossary", nil))
			wantStatus := 404
			if tc.wantProject {
				wantStatus = 200
			}
			if w.Code != wantStatus {
				t.Fatalf("project publication = %d, want %d", w.Code, wantStatus)
			}
		})
	}
}
