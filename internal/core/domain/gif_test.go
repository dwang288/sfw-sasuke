package domain_test

import (
	"errors"
	"testing"

	"github.com/dwang288/sfw-sasuke/internal/core/domain"
)

// validGif returns a gif that passes Validate; tests mutate a copy of it to
// exercise one failure mode at a time.
func validGif() domain.Gif {
	return domain.Gif{
		GuildID:        "guild-1",
		UploaderUserID: "user-1",
		Name:           "sfw",
		Files: []domain.GifFile{
			{ObjectKey: "k0", ContentType: "image/png", SizeBytes: 10, Ordinal: 0},
			{ObjectKey: "k1", ContentType: "image/gif", SizeBytes: 20, Ordinal: 1},
		},
	}
}

func TestGifValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*domain.Gif)
		wantErr error // nil means valid
	}{
		{"valid", func(*domain.Gif) {}, nil},
		{"missing guild", func(g *domain.Gif) { g.GuildID = "" }, domain.ErrInvalidGif},
		{"missing uploader", func(g *domain.Gif) { g.UploaderUserID = "" }, domain.ErrInvalidGif},
		{"empty name", func(g *domain.Gif) { g.Name = "" }, domain.ErrInvalidGif},
		{"whitespace name", func(g *domain.Gif) { g.Name = "   " }, domain.ErrInvalidGif},
		{"no files", func(g *domain.Gif) { g.Files = nil }, domain.ErrInvalidGif},
		{"bad file propagates", func(g *domain.Gif) { g.Files[1].ObjectKey = "" }, domain.ErrInvalidGif},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := validGif()
			tc.mutate(&g)
			err := g.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate: got %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate: got %v, want errors.Is %v", err, tc.wantErr)
			}
		})
	}
}

func TestGifFileValidate(t *testing.T) {
	base := domain.GifFile{ObjectKey: "k", ContentType: "image/png", SizeBytes: 1, Ordinal: 0}
	tests := []struct {
		name    string
		mutate  func(*domain.GifFile)
		wantErr error
	}{
		{"valid", func(*domain.GifFile) {}, nil},
		{"zero size ok", func(f *domain.GifFile) { f.SizeBytes = 0 }, nil},
		{"empty object key", func(f *domain.GifFile) { f.ObjectKey = "" }, domain.ErrInvalidGif},
		{"empty content type", func(f *domain.GifFile) { f.ContentType = "" }, domain.ErrInvalidGif},
		{"negative size", func(f *domain.GifFile) { f.SizeBytes = -1 }, domain.ErrInvalidGif},
		{"negative ordinal", func(f *domain.GifFile) { f.Ordinal = -1 }, domain.ErrInvalidGif},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.mutate(&f)
			err := f.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate: got %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate: got %v, want errors.Is %v", err, tc.wantErr)
			}
		})
	}
}

func TestGifTotalBytes(t *testing.T) {
	g := validGif() // 10 + 20
	if got := g.TotalBytes(); got != 30 {
		t.Errorf("TotalBytes: got %d, want 30", got)
	}
	empty := domain.Gif{}
	if got := empty.TotalBytes(); got != 0 {
		t.Errorf("TotalBytes of no files: got %d, want 0", got)
	}
}
