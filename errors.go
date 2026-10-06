package pgfake

import (
	"errors"
	"strings"

	"github.com/pgrundev/pgfake/internal/transformers"
)

// Sentinel errors. Match them with errors.Is.
//
// No pgfake error ever contains a source value or the secret key: messages
// are built only from configuration (transformer names, column names, Go and
// PostgreSQL type names).
var (
	// ErrNoKey is returned by New when Options.Key is empty.
	ErrNoKey = errors.New("secret key is empty")
	// ErrUnknownTransformer is returned for a rule naming no built-in transformer.
	ErrUnknownTransformer = errors.New("unknown transformer")
	// ErrInvalidRule is returned for a rule or rule key that cannot be used.
	ErrInvalidRule = errors.New("invalid rule")
	// ErrUnsupportedValue is returned for Go types pgfake does not handle.
	ErrUnsupportedValue = errors.New("unsupported value type")
	// ErrTypeMismatch is returned when a transformer cannot produce a value of
	// the column's type (for example "email" on an integer column).
	ErrTypeMismatch = errors.New("transformer does not support this type")
	// ErrInvalidInput is returned when a typed value cannot be parsed.
	ErrInvalidInput = transformers.ErrInvalidInput
	// ErrInvalidJSON is returned when the "json" transformer gets a malformed document.
	ErrInvalidJSON = errors.New("malformed JSON document")
	// ErrValueTooLong is returned when a fake value cannot fit the column length.
	ErrValueTooLong = transformers.ErrTooLong
	// ErrRowShape is returned when a row does not match its table definition.
	ErrRowShape = errors.New("row does not match the table's columns")
)

// Error locates a failure. It wraps one of the sentinel errors and carries
// only metadata, never the value being transformed.
type Error struct {
	Schema      string
	Table       string
	Column      string
	Path        string // JSON path inside the column, if any
	Transformer string
	Err         error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("pgfake: ")
	n := b.Len()
	for _, part := range []string{e.Schema, e.Table, e.Column, e.Path} {
		if part == "" {
			continue
		}
		if b.Len() > n {
			b.WriteByte('.')
		}
		b.WriteString(part)
	}
	if b.Len() > n {
		b.WriteString(": ")
	}
	if e.Transformer != "" {
		b.WriteString(e.Transformer)
		b.WriteString(": ")
	}
	b.WriteString(e.Err.Error())
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }
