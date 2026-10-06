// Package transformers implements the built-in value transformers.
//
// A transformer is a pure function from the text form of a value to the text
// form of its fake replacement. All randomness comes from the supplied
// deterministic stream; transformers never see the secret key and never keep
// state. The pgfake package handles NULLs, Go types and PostgreSQL types
// around them.
package transformers

import (
	"errors"
	"unicode/utf8"

	"github.com/pgrundev/pgfake/internal/deterministic"
)

// Kind is the logical type of the value being transformed.
type Kind uint8

const (
	Text Kind = iota
	Int
	UUID
	IP
	Date
	Timestamp
	TimestampTZ
	JSON
	Bool
	Numeric
)

func (k Kind) String() string {
	switch k {
	case Text:
		return "text"
	case Int:
		return "integer"
	case UUID:
		return "uuid"
	case IP:
		return "inet"
	case Date:
		return "date"
	case Timestamp:
		return "timestamp"
	case TimestampTZ:
		return "timestamptz"
	case JSON:
		return "json"
	case Bool:
		return "boolean"
	case Numeric:
		return "numeric"
	}
	return "unknown"
}

// Options are the per-rule knobs a transformer may consult.
type Options struct {
	Unique         bool
	PreserveDomain bool
	PreserveLength bool
	Length         int
	Days           int
}

// Input is everything a transformer may look at.
type Input struct {
	Text   string // text form of the source value
	Kind   Kind
	Bits   int  // usable magnitude bits when Kind == Int
	CIDR   bool // value is a PostgreSQL cidr (host bits must be zero)
	MaxLen int  // maximum output length in characters, 0 = unlimited
	Opts   Options
	S      *deterministic.Stream
}

// Func produces the text form of the fake value.
type Func func(in *Input) (string, error)

// UniquePolicy says what Options.Unique means for a transformer.
type UniquePolicy uint8

const (
	// UniqueNoop: the output domain is large enough that collisions are
	// negligible, or the mapping is already a bijection.
	UniqueNoop UniquePolicy = iota
	// UniqueSuffix: a deterministic digest suffix is appended.
	UniqueSuffix
	// UniqueNative: the transformer switches to a collision-free mapping.
	UniqueNative
	// UniqueUnsupported: the rule is rejected.
	UniqueUnsupported
)

// Spec describes a registered transformer.
type Spec struct {
	Name     string
	Fn       Func
	Kinds    []Kind
	Unique   UniquePolicy
	Truncate bool // output may be cut to MaxLen without losing validity
}

// Accepts reports whether the transformer can produce values of kind k.
func (s *Spec) Accepts(k Kind) bool {
	for _, x := range s.Kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Names lists the built-in transformers in documentation order. "copy",
// "null" and "json" are implemented by the pgfake package itself.
var Names = []string{
	"copy", "null", "hash", "string", "email", "username",
	"first_name", "last_name", "full_name", "phone", "company",
	"uuid", "ip", "date", "token", "secret", "json",
}

var text = []Kind{Text}

// Registry maps transformer names to their implementation.
var Registry = map[string]*Spec{
	"copy":       {Name: "copy"},
	"null":       {Name: "null"},
	"json":       {Name: "json", Kinds: []Kind{JSON, Text}},
	"hash":       {Name: "hash", Fn: Hash, Kinds: []Kind{Text, Int, UUID}, Truncate: true},
	"string":     {Name: "string", Fn: String, Kinds: text, Unique: UniqueUnsupported, Truncate: true},
	"email":      {Name: "email", Fn: Email, Kinds: text},
	"username":   {Name: "username", Fn: Username, Kinds: text, Truncate: true},
	"first_name": {Name: "first_name", Fn: FirstName, Kinds: text, Unique: UniqueSuffix, Truncate: true},
	"last_name":  {Name: "last_name", Fn: LastName, Kinds: text, Unique: UniqueSuffix, Truncate: true},
	"full_name":  {Name: "full_name", Fn: FullName, Kinds: text, Unique: UniqueSuffix, Truncate: true},
	"company":    {Name: "company", Fn: Company, Kinds: text, Unique: UniqueSuffix, Truncate: true},
	"phone":      {Name: "phone", Fn: Phone, Kinds: text, Unique: UniqueNative},
	"uuid":       {Name: "uuid", Fn: UUIDv4, Kinds: []Kind{Text, UUID}},
	"ip":         {Name: "ip", Fn: IPAddr, Kinds: []Kind{Text, IP}, Unique: UniqueUnsupported},
	"date":       {Name: "date", Fn: DateShift, Kinds: []Kind{Text, Date, Timestamp, TimestampTZ}, Unique: UniqueUnsupported},
	"token":      {Name: "token", Fn: Token, Kinds: text, Truncate: true},
	"secret":     {Name: "secret", Fn: Secret, Kinds: text, Truncate: true},
}

// ErrTooLong is returned when a structurally valid value cannot fit MaxLen.
var ErrTooLong = errors.New("value does not fit the column length")

const (
	hexDigits = "0123456789abcdef"
	lower     = "abcdefghijklmnopqrstuvwxyz"
	upper     = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits    = "0123456789"
	alnum     = lower + digits
	base62    = digits + upper + lower

	// uniqueSuffixHex is the number of hex digits appended by UniqueSuffix
	// transformers (48 bits).
	uniqueSuffixHex = 12
)

// appendHex appends n hex digits drawn from the stream (4 bits each).
func appendHex(dst []byte, s *deterministic.Stream, n int) []byte {
	for n >= 2 {
		b := s.Byte()
		dst = append(dst, hexDigits[b>>4], hexDigits[b&0x0f])
		n -= 2
	}
	if n == 1 {
		dst = append(dst, hexDigits[s.Byte()>>4])
	}
	return dst
}

// appendChars appends n characters drawn uniformly from alphabet.
func appendChars(dst []byte, s *deterministic.Stream, alphabet string, n int) []byte {
	for ; n > 0; n-- {
		dst = append(dst, alphabet[s.Pick(len(alphabet))])
	}
	return dst
}

// outLen resolves the output length of free-form transformers.
func outLen(in *Input, def int) int {
	switch {
	case in.Opts.Length > 0:
		return in.Opts.Length
	case in.Opts.PreserveLength:
		return utf8.RuneCountInString(in.Text)
	}
	return def
}
