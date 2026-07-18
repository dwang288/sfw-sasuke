package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/dwang288/sfw-sasuke/pkg/config"
)

// configFromJSON writes raw JSON to a temp file and returns the parsed config.Map.
func configFromJSON(t *testing.T, raw string) config.Map {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	conf, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return conf
}

// tempFileWithMagic creates a temp file whose first bytes are magic, padded to
// 512 bytes so http.DetectContentType has enough data to read.
func tempFileWithMagic(t *testing.T, magic []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "testimg")
	if err != nil {
		t.Fatal(err)
	}
	padded := make([]byte, 512)
	copy(padded, magic)
	if _, err := f.Write(padded); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// Case 1 (structural): each handler key matches a config entry name exactly.
// Case 3: handler count equals config entry count — no entries are dropped or duplicated.
func TestBuildHandlers(t *testing.T) {
	// Silence the slog calls inside buildHandlers during tests.
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	cases := []struct {
		name     string
		json     string
		wantKeys []string
	}{
		{
			name:     "single entry produces one handler with matching key",
			json:     `{"files":[{"name":"razzle","description":"d","filenames":["razzle.png"]}]}`,
			wantKeys: []string{"razzle"},
		},
		{
			name:     "each config entry produces exactly one handler keyed by its name",
			json:     `{"files":[{"name":"foo","description":"d","filenames":["foo.gif"]},{"name":"bar","description":"d","filenames":["bar.png"]},{"name":"baz","description":"d","filenames":["baz.jpg"]}]}`,
			wantKeys: []string{"bar", "baz", "foo"},
		},
		{
			name:     "empty config produces no handlers",
			json:     `{"files":[]}`,
			wantKeys: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf := configFromJSON(t, tc.json)
			handlers := buildHandlers(conf)

			if len(handlers) != len(tc.wantKeys) {
				t.Fatalf("got %d handlers, want %d", len(handlers), len(tc.wantKeys))
			}

			keys := make([]string, 0, len(handlers))
			for k := range handlers {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			wantSorted := append([]string(nil), tc.wantKeys...)
			sort.Strings(wantSorted)

			for i, k := range keys {
				if k != wantSorted[i] {
					t.Errorf("handler key[%d]: got %q, want %q", i, k, wantSorted[i])
				}
			}
		})
	}
}

func TestBuildCommands(t *testing.T) {
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	cases := []struct {
		name      string
		json      string
		wantCount int
		wantNames []string
	}{
		{
			name:      "single entry",
			json:      `{"files":[{"name":"razzle","description":"razzle dazzle","filenames":["razzle.png"]}]}`,
			wantCount: 1,
			wantNames: []string{"razzle"},
		},
		{
			name:      "multiple entries all become commands",
			json:      `{"files":[{"name":"foo","description":"d","filenames":["foo.gif"]},{"name":"bar","description":"d","filenames":["bar.png"]}]}`,
			wantCount: 2,
			wantNames: []string{"bar", "foo"},
		},
		{
			name:      "empty config produces no commands",
			json:      `{"files":[]}`,
			wantCount: 0,
			wantNames: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf := configFromJSON(t, tc.json)
			cmds := buildCommands(conf)

			if len(cmds) != tc.wantCount {
				t.Fatalf("got %d commands, want %d", len(cmds), tc.wantCount)
			}

			names := make([]string, len(cmds))
			for i, c := range cmds {
				names[i] = c.Name
			}
			sort.Strings(names)

			wantSorted := append([]string(nil), tc.wantNames...)
			sort.Strings(wantSorted)

			for i, n := range names {
				if n != wantSorted[i] {
					t.Errorf("command[%d]: got %q, want %q", i, n, wantSorted[i])
				}
			}
		})
	}
}

// Case 4: sniffContentType sniffs the correct MIME type from file magic bytes.
func TestSniffContentType(t *testing.T) {
	cases := []struct {
		name     string
		magic    []byte
		wantMIME string
	}{
		{
			name:     "GIF",
			magic:    []byte("GIF89a"),
			wantMIME: "image/gif",
		},
		{
			name:     "PNG",
			magic:    []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
			wantMIME: "image/png",
		},
		{
			name:     "JPEG",
			magic:    []byte{0xff, 0xd8, 0xff, 0xe0},
			wantMIME: "image/jpeg",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tempFileWithMagic(t, tc.magic)
			got, _, err := sniffContentType(f)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantMIME {
				t.Errorf("got MIME %q, want %q", got, tc.wantMIME)
			}
		})
	}
}

// Case 5: sniffContentType must work on a non-seekable stream and must not
// consume it — the reader it returns has to replay the sniffed prefix followed
// by the rest of the input, byte for byte. This is the property that lets the
// same code serve an *os.File today and an S3 GetObject body later.
func TestSniffContentTypePreservesStream(t *testing.T) {
	over512 := make([]byte, 600)
	for i := range over512 {
		over512[i] = byte(i)
	}
	cases := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "over 512 bytes",
			payload: over512,
		},
		{
			name:    "under 512 bytes",
			payload: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := struct{ io.Reader }{bytes.NewReader(tc.payload)}
			_, reader, err := sniffContentType(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tc.payload) != len(got) {
				t.Errorf("got payload length %d, want %d", len(got), len(tc.payload))
			}
			for i := 0; i < min(len(tc.payload), len(got)); i++ {
				if tc.payload[i] != got[i] {
					t.Errorf("payload content differs at position %d, got %#x, want %#x", i, got[i], tc.payload[i])
					break
				}
			}
		})
	}
}

// Case 6: generateFiles surfaces a missing file as an error instead of
// crashing the process (readImage/sniffContentType no longer call log.Fatal).
func TestGenerateFilesMissingFile(t *testing.T) {
	t.Setenv("ASSETS_DIR", t.TempDir())

	if _, _, err := generateFiles([]string{"does-not-exist.png"}); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

// Case 7: readImage and sniffContentType return errors rather than calling
// checkErr/log.Fatal on failure.
func TestReadImageMissingFile(t *testing.T) {
	if _, err := readImage(filepath.Join(t.TempDir(), "does-not-exist.png")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}
