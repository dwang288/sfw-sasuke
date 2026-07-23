package domain

import "errors"

// Sentinel errors returned through ports. Adapters translate driver errors
// into these (e.g. pgx.ErrNoRows -> ErrGifNotFound, S3 NoSuchKey ->
// ErrObjectNotFound) so the core never inspects a provider error type.
var (
	ErrGifNotFound    = errors.New("gif not found")
	ErrGuildNotFound  = errors.New("guild not found")
	ErrUserNotFound   = errors.New("user not found")
	ErrObjectNotFound = errors.New("object not found")
	ErrGifNameTaken   = errors.New("gif name already taken in this guild")
)

// Validation sentinels. A domain type's Validate method wraps one of these with
// a reason (fmt.Errorf("%w: ...")), so callers can both read the reason and test
// the class with errors.Is. The app service calls Validate before persisting;
// adapters stay thin and do not validate.
var (
	ErrInvalidGif           = errors.New("invalid gif")
	ErrInvalidUploadPolicy  = errors.New("invalid upload policy")
	ErrInvalidGuildSettings = errors.New("invalid guild settings")
)
