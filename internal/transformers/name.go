package transformers

import (
	"strings"

	"github.com/pgrundev/pgfake/internal/datasets"
	"github.com/pgrundev/pgfake/internal/deterministic"
)

func pick(s *deterministic.Stream, list []string) string {
	return list[s.Intn(uint64(len(list)))]
}

// withSuffix appends "-<12 hex>" when Unique is requested. Dictionaries are
// finite, so this is what keeps unique columns unique.
func withSuffix(in *Input, v string) string {
	if !in.Opts.Unique {
		return v
	}
	out := make([]byte, 0, len(v)+1+uniqueSuffixHex)
	out = append(out, v...)
	out = append(out, '-')
	return string(appendHex(out, in.S, uniqueSuffixHex))
}

// FirstName picks a first name from the embedded dictionary.
func FirstName(in *Input) (string, error) {
	return withSuffix(in, pick(in.S, datasets.FirstNames)), nil
}

// LastName picks a last name from the embedded dictionary.
func LastName(in *Input) (string, error) {
	return withSuffix(in, pick(in.S, datasets.LastNames)), nil
}

// FullName returns "<first> <last>".
func FullName(in *Input) (string, error) {
	first := pick(in.S, datasets.FirstNames)
	last := pick(in.S, datasets.LastNames)
	return withSuffix(in, first+" "+last), nil
}

// Company returns "<name> <suffix>", for example "Horizon Labs".
func Company(in *Input) (string, error) {
	name := pick(in.S, datasets.CompanyNames)
	suffix := pick(in.S, datasets.CompanySuffixes)
	return withSuffix(in, name+" "+suffix), nil
}

// Username returns "<first name>_<16 hex>", for example
// "daniel_3f9a1c0e5b7d2a46". The 64-bit tail keeps collisions negligible.
func Username(in *Input) (string, error) {
	first := strings.ToLower(pick(in.S, datasets.FirstNames))
	out := make([]byte, 0, len(first)+17)
	out = append(out, first...)
	out = append(out, '_')
	return string(appendHex(out, in.S, 16)), nil
}
