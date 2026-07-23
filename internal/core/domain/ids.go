package domain

// Discord snowflakes are kept as strings: they exceed float64-safe integer
// range and are never used arithmetically. Distinct types keep a UserID from
// being passed where a GuildID belongs.
type (
	// GuildID is a Discord guild snowflake.
	GuildID string
	// UserID is a Discord user snowflake.
	UserID string
	// GifID is the database identity of a gif row.
	GifID int64
)

// Empty reports whether the guild id is unset. Validate methods and adapter
// guards use it to reject a zero-value id.
func (id GuildID) Empty() bool { return id == "" }

// Empty reports whether the user id is unset.
func (id UserID) Empty() bool { return id == "" }
