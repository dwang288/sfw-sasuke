package domain

import "time"

// AuditEntry records a destructive or administrative action. Action values
// are dotted strings such as "gif.delete" or "settings.update". GuildID and
// GifID are zero when not applicable. Detail carries optional structured
// context; adapters handle its serialization.
type AuditEntry struct {
	ID          int64
	ActorUserID UserID
	Action      string
	GuildID     GuildID
	GifID       GifID
	Detail      map[string]any
	CreatedAt   time.Time
}
