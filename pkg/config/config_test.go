package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Case 2: config.New correctly parses entries, including multi-file commands.
// Case 5: config.New returns an error for a missing or malformed config file.
func TestNew(t *testing.T) {
	cases := []struct {
		name        string
		path        func(t *testing.T) string
		wantErr     bool
		wantCount   int
		wantEntries map[string][]string // name -> expected filenames in order
	}{
		{
			name: "single entry single file",
			path: func(t *testing.T) string {
				return writeConfig(t, `{"files":[{"name":"razzle","description":"razzle dazzle","filenames":["razzle.png"]}]}`)
			},
			wantCount:   1,
			wantEntries: map[string][]string{"razzle": {"razzle.png"}},
		},
		{
			name: "multi-file entry preserves all files in order",
			path: func(t *testing.T) string {
				return writeConfig(t, `{"files":[{"name":"sfw","description":"cleanse","filenames":["a.png","b.png","c.png","d.png"]}]}`)
			},
			wantCount:   1,
			wantEntries: map[string][]string{"sfw": {"a.png", "b.png", "c.png", "d.png"}},
		},
		{
			name: "multiple entries each parsed independently",
			path: func(t *testing.T) string {
				return writeConfig(t, `{"files":[{"name":"foo","description":"d","filenames":["foo.gif"]},{"name":"bar","description":"d","filenames":["bar.png","bar2.png"]}]}`)
			},
			wantCount:   2,
			wantEntries: map[string][]string{"foo": {"foo.gif"}, "bar": {"bar.png", "bar2.png"}},
		},
		{
			name:        "missing file returns an error",
			path:        func(t *testing.T) string { return "/nonexistent/path/config.json" },
			wantErr:     true,
			wantCount:   0,
			wantEntries: map[string][]string{},
		},
		{
			name:        "malformed JSON returns an error",
			path:        func(t *testing.T) string { return writeConfig(t, `{"files": [not valid json`) },
			wantErr:     true,
			wantCount:   0,
			wantEntries: map[string][]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf, err := New(tc.path(t))
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			if got := len(conf["files"]); got != tc.wantCount {
				t.Fatalf("got %d entries, want %d", got, tc.wantCount)
			}

			byName := make(map[string]filesConfig, len(conf["files"]))
			for _, e := range conf["files"] {
				byName[e.Name] = e
			}

			for wantName, wantFiles := range tc.wantEntries {
				e, ok := byName[wantName]
				if !ok {
					t.Errorf("entry %q not found in config", wantName)
					continue
				}
				if len(e.Filenames) != len(wantFiles) {
					t.Errorf("entry %q: got %d filenames, want %d", wantName, len(e.Filenames), len(wantFiles))
					continue
				}
				for i, f := range e.Filenames {
					if f != wantFiles[i] {
						t.Errorf("entry %q filename[%d]: got %q, want %q", wantName, i, f, wantFiles[i])
					}
				}
			}
		})
	}
}
