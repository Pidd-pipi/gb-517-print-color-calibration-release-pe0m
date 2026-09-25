package service

import "errors"

var (
	ErrInvalidTransition = errors.New("requested status transition is not allowed")
	ErrInvalidInput      = errors.New("business input validation failed")
	ErrUnauthorized      = errors.New("invalid username or password")
	ErrInactiveUser      = errors.New("user account is inactive")
	ErrForbidden         = errors.New("role is not permitted for this operation")
	ErrLocked            = errors.New("resolved record is immutable")
	// ErrReworkConflict covers every rejected batch-rework request (missing
	// reason, wrong run state, stale version, unmet re-release gate) so the
	// handler can answer 409 while the original decision and version chain
	// stay untouched.
	ErrReworkConflict = errors.New("batch rework request conflicts with the current state")
)
