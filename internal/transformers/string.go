package transformers

import (
	"errors"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidInput is returned when a typed value (for example the text form
// of an integer column) cannot be parsed. It never carries the value.
var ErrInvalidInput = errors.New("input is not valid for its declared type")

// Hash is the generic type-preserving pseudonym:
//
//	text    -> 64 lowercase hex digits (the raw HMAC-SHA256 digest)
//	integer -> an integer of the same width via a keyed permutation, so
//	           distinct inputs always map to distinct outputs
//	uuid    -> a UUID
func Hash(in *Input) (string, error) {
	switch in.Kind {
	case Int:
		return hashInt(in)
	case UUID:
		return UUIDv4(in)
	}
	n := outLen(in, 64)
	return string(appendHex(make([]byte, 0, n), in.S, n)), nil
}

func hashInt(in *Input) (string, error) {
	bits := in.Bits
	if bits < 2 || bits > 64 {
		return "", ErrInvalidInput
	}
	a := uint64(1) << (bits / 2)
	b := uint64(1) << (bits - bits/2)

	if bits == 64 {
		u, err := strconv.ParseUint(in.Text, 10, 64)
		if err != nil {
			return "", ErrInvalidInput
		}
		return strconv.FormatUint(in.S.Permute(u, a, b), 10), nil
	}

	v, err := strconv.ParseInt(in.Text, 10, 64)
	if err != nil {
		return "", ErrInvalidInput
	}
	// Negative values are permuted through their bitwise complement, which
	// keeps the sign and the bijection.
	u := uint64(v)
	if v < 0 {
		u = ^u
	}
	if u>>bits != 0 {
		return "", ErrInvalidInput
	}
	p := int64(in.S.Permute(u, a, b))
	if v < 0 {
		p = ^p
	}
	return strconv.FormatInt(p, 10), nil
}

// String replaces every letter with a letter of the same case and every digit
// with a digit, keeping length, whitespace and punctuation. With Length set it
// returns that many lowercase alphanumeric characters instead.
func String(in *Input) (string, error) {
	if n := in.Opts.Length; n > 0 {
		return string(appendChars(make([]byte, 0, n), in.S, alnum, n)), nil
	}
	out := make([]byte, 0, len(in.Text))
	for _, r := range in.Text {
		switch {
		case r >= '0' && r <= '9':
			out = append(out, digits[in.S.Pick(10)])
		case r >= 'a' && r <= 'z':
			out = append(out, lower[in.S.Pick(26)])
		case r >= 'A' && r <= 'Z':
			out = append(out, upper[in.S.Pick(26)])
		case r < utf8.RuneSelf:
			out = append(out, byte(r))
		case unicode.IsDigit(r):
			out = append(out, digits[in.S.Pick(10)])
		case unicode.IsUpper(r):
			out = append(out, upper[in.S.Pick(26)])
		case r != utf8.RuneError && (unicode.IsSpace(r) || unicode.IsPunct(r)):
			out = utf8.AppendRune(out, r)
		default:
			// Other letters, symbols, marks and invalid bytes.
			out = append(out, lower[in.S.Pick(26)])
		}
	}
	return string(out), nil
}

// Token returns lowercase hex digits (32 by default). Nothing of the source
// value survives.
func Token(in *Input) (string, error) {
	n := outLen(in, 32)
	return string(appendHex(make([]byte, 0, n), in.S, n)), nil
}

// Secret returns base62 characters (40 by default). Nothing of the source
// value survives.
func Secret(in *Input) (string, error) {
	n := outLen(in, 40)
	return string(appendChars(make([]byte, 0, n), in.S, base62, n)), nil
}
