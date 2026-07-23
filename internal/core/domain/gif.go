package domain

import (
	"fmt"
	"strings"
	"time"
)

// Gif is a named image invocable in one guild via /gif. (guild, name) is
// unique; a gif owns one or more files delivered together as attachments.
type Gif struct {
	ID             GifID
	GuildID        GuildID
	UploaderUserID UserID
	Name           string
	CreatedAt      time.Time
	Files          []GifFile // ordered by Ordinal
}

// GifFile is one stored object belonging to a gif. Assets are arbitrary
// images despite the "gif" name — ContentType is authoritative, not the
// object key's extension.
type GifFile struct {
	ID          int64
	GifID       GifID
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	Ordinal     int // display/attachment order
}

// Validate reports whether the file is well-formed: it must have an object key
// and a content type, a non-negative size, and a non-negative ordinal. A bad
// value returns a reason wrapped in ErrInvalidGif.
func (f GifFile) Validate() error {
	switch {
	case f.ObjectKey == "":
		return fmt.Errorf("%w: file has empty object key", ErrInvalidGif)
	case f.ContentType == "":
		return fmt.Errorf("%w: file %q has empty content type", ErrInvalidGif, f.ObjectKey)
	case f.SizeBytes < 0:
		return fmt.Errorf("%w: file %q has negative size", ErrInvalidGif, f.ObjectKey)
	case f.Ordinal < 0:
		return fmt.Errorf("%w: file %q has negative ordinal", ErrInvalidGif, f.ObjectKey)
	}
	return nil
}

// Validate reports whether the gif is well-formed: it must belong to a guild,
// name an uploader, have a non-empty name, and own at least one valid file.
// Ordinals need only be non-negative — a complete or unique sequence is not
// required, so seeded data is not rejected. A bad value returns a reason
// wrapped in ErrInvalidGif.
func (g Gif) Validate() error {
	switch {
	case g.GuildID.Empty():
		return fmt.Errorf("%w: missing guild id", ErrInvalidGif)
	case g.UploaderUserID.Empty():
		return fmt.Errorf("%w: missing uploader id", ErrInvalidGif)
	case strings.TrimSpace(g.Name) == "":
		return fmt.Errorf("%w: empty name", ErrInvalidGif)
	case len(g.Files) == 0:
		return fmt.Errorf("%w: gif %q has no files", ErrInvalidGif, g.Name)
	}
	for _, f := range g.Files {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// TotalBytes returns the summed size of the gif's files. The storage-usage
// rollup and the upload quota check both read this single helper.
func (g Gif) TotalBytes() int64 {
	var total int64
	for _, f := range g.Files {
		total += f.SizeBytes
	}
	return total
}
