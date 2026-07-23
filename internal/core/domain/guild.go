package domain

import (
	"fmt"
	"time"
)

// Guild is a Discord server the bot has joined, upserted on GuildCreate and
// removed on GuildDelete.
type Guild struct {
	ID       GuildID
	Name     string
	JoinedAt time.Time
}

// GuildUsage is a per-guild storage rollup for the admin console.
type GuildUsage struct {
	GuildID  GuildID
	GifCount int
	Bytes    int64
}

// UploadPolicy controls who may upload gifs to a guild.
type UploadPolicy string

const (
	// UploadPolicyManageGuild restricts uploads to members holding the
	// MANAGE_GUILD permission. This is the default for every guild.
	UploadPolicyManageGuild UploadPolicy = "manage_guild"
	// UploadPolicyEveryone allows any guild member to upload.
	UploadPolicyEveryone UploadPolicy = "everyone"
)

// Valid reports whether p is one of the defined policies.
func (p UploadPolicy) Valid() bool {
	return p == UploadPolicyManageGuild || p == UploadPolicyEveryone
}

// GuildSettings is a guild's upload configuration. StorageQuota is a pointer so
// nil ("unlimited") stays distinct from a quota of zero — the data model's
// storage_quota_bytes is nullable for exactly this reason.
type GuildSettings struct {
	GuildID      GuildID
	UploadPolicy UploadPolicy
	StorageQuota *int64
}

// Validate reports whether the settings are well-formed: the policy must be a
// defined UploadPolicy, and a set quota must be non-negative. A bad value
// returns a reason wrapped in ErrInvalidUploadPolicy or ErrInvalidGuildSettings.
func (s GuildSettings) Validate() error {
	if !s.UploadPolicy.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidUploadPolicy, s.UploadPolicy)
	}
	if s.StorageQuota != nil && *s.StorageQuota < 0 {
		return fmt.Errorf("%w: negative storage quota", ErrInvalidGuildSettings)
	}
	return nil
}

// DefaultGuildSettings returns the settings that apply to a guild with no
// guild_settings row: the manage_guild upload policy and no storage quota
// (StorageQuota nil = unlimited). SettingsRepository.Get returns this when a
// guild has never been configured, so the manage_guild default is defined
// here once rather than re-derived in every adapter.
func DefaultGuildSettings(id GuildID) GuildSettings {
	return GuildSettings{
		GuildID:      id,
		UploadPolicy: UploadPolicyManageGuild,
		// StorageQuota left nil: unlimited until an admin sets a quota.
	}
}
