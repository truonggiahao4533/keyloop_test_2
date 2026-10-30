// Package domain holds the inventory entities, business rules and ports.
// It must not import any adapter or infrastructure package.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel error kinds. Use errors.Is to classify an error.
var (
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation failed")
	ErrConflict   = errors.New("conflict")
	ErrForbidden  = errors.New("forbidden")
)

// Error is a domain error with a client-safe message.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

// Validationf returns an ErrValidation with a formatted message.
func Validationf(format string, args ...any) error {
	return &Error{Kind: ErrValidation, Msg: fmt.Sprintf(format, args...)}
}

// NotFoundf returns an ErrNotFound with a formatted message.
func NotFoundf(format string, args ...any) error {
	return &Error{Kind: ErrNotFound, Msg: fmt.Sprintf(format, args...)}
}

// Conflictf returns an ErrConflict with a formatted message.
func Conflictf(format string, args ...any) error {
	return &Error{Kind: ErrConflict, Msg: fmt.Sprintf(format, args...)}
}
