package storage

import "errors"

// ErrNotFound is returned when a referenced entity does not exist.
var ErrNotFound = errors.New("not found")
