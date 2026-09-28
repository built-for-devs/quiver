// Package apperr defines typed errors that map to stable process exit codes.
//
// Exit codes are part of the CLI's public contract; scripts and CI depend on
// them. Do not renumber existing codes.
package apperr

import (
	"errors"
	"fmt"
)

// Exit codes.
const (
	CodeOK          = 0
	CodeGeneral     = 1 // unexpected / unclassified failure
	CodeUsage       = 2 // bad flags/args, missing required config, refused guarded action
	CodeAuth        = 3 // missing, invalid, or under-scoped token
	CodeNotFound    = 4 // requested resource does not exist
	CodeValidation  = 5 // server rejected input, or a --check failed
	CodeUnavailable = 6 // network error, timeout, or server-side 5xx
	CodeConflict    = 7 // remote changed since the local copy was pulled
)

// Error carries an exit code alongside a human-readable message.
type Error struct {
	Code int
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil && e.Msg != "" {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "error"
}

func (e *Error) Unwrap() error { return e.Err }

func newf(code int, format string, a ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// New returns an error with the given exit code and message.
func New(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

func Usage(format string, a ...any) *Error       { return newf(CodeUsage, format, a...) }
func Auth(format string, a ...any) *Error        { return newf(CodeAuth, format, a...) }
func NotFound(format string, a ...any) *Error    { return newf(CodeNotFound, format, a...) }
func Validation(format string, a ...any) *Error  { return newf(CodeValidation, format, a...) }
func Unavailable(format string, a ...any) *Error { return newf(CodeUnavailable, format, a...) }
func Conflict(format string, a ...any) *Error    { return newf(CodeConflict, format, a...) }

// Wrap attaches an exit code and context message to err.
func Wrap(code int, err error, msg string) *Error {
	return &Error{Code: code, Msg: msg, Err: err}
}

// CodeOf returns the exit code for err. nil maps to CodeOK; untyped errors
// map to CodeGeneral.
func CodeOf(err error) int {
	if err == nil {
		return CodeOK
	}
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return CodeGeneral
}

// Name returns a stable machine-readable name for an exit code, used in
// --json error output.
func Name(code int) string {
	switch code {
	case CodeOK:
		return "ok"
	case CodeUsage:
		return "usage"
	case CodeAuth:
		return "auth"
	case CodeNotFound:
		return "not_found"
	case CodeValidation:
		return "validation"
	case CodeUnavailable:
		return "unavailable"
	case CodeConflict:
		return "conflict"
	default:
		return "error"
	}
}
