package domain

import "time"

// User is a Discord user known to the service, recorded on first login or by
// the seeder (which attributes legacy gifs to a synthetic "system" user).
type User struct {
	ID          UserID
	Username    string
	Avatar      string
	FirstSeenAt time.Time
}
