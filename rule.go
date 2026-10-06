package pgfake

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pgrundev/pgfake/internal/transformers"
)

// Rule selects a transformer and its options. The zero value is invalid;
// Transformer is required. The JSON form uses snake_case keys.
type Rule struct {
	// Transformer is one of Transformers().
	Transformer string `json:"transformer"`

	// Scope names the relationship a value belongs to. Equal values under the
	// same transformer and scope always map to the same fake value, wherever
	// they are stored. When empty, the scope is the value's own column
	// ("schema.table.column", plus the JSON path if any), so unrelated
	// columns do not share fake identities by accident.
	Scope string `json:"scope,omitempty"`

	// Unique asks for collision-free output from transformers whose natural
	// output domain is small. Dictionary transformers (names, company) append
	// "-<12 hex>"; phone switches to a keyed permutation of the digits. It is
	// a no-op where collisions are already negligible and rejected where it
	// cannot be honored (string, ip, date).
	Unique bool `json:"unique,omitempty"`

	// PreserveDomain keeps the domain of an email address.
	PreserveDomain bool `json:"preserve_domain,omitempty"`

	// PreserveLength makes email, hash, token and secret outputs as long as
	// their input.
	PreserveLength bool `json:"preserve_length,omitempty"`

	// Length fixes the output length of hash, string, token and secret.
	Length int `json:"length,omitempty"`

	// MaxLength caps the output length in characters. It defaults to the
	// length modifier of PGType, e.g. 64 for varchar(64).
	MaxLength int `json:"max_length,omitempty"`

	// Days is the half-width of the date shift window (default 365).
	Days int `json:"days,omitempty"`

	// Paths maps dotted JSON paths to rules, for the "json" transformer.
	// Arrays along a path are traversed transparently.
	Paths map[string]Rule `json:"paths,omitempty"`
}

// maxLength bounds Length and MaxLength so a typo cannot allocate gigabytes.
const maxLength = 1 << 20

// compiled is a validated Rule.
type compiled struct {
	spec   *transformers.Spec
	scope  string
	opts   transformers.Options
	maxLen int
	paths  []compiledPath
}

type compiledPath struct {
	path string
	keys []string
	rule compiled
}

func compile(r Rule) (compiled, error) {
	if r.Transformer == "" {
		return compiled{}, fmt.Errorf("%w: transformer is required", ErrInvalidRule)
	}
	spec := transformers.Registry[r.Transformer]
	if spec == nil {
		return compiled{}, fmt.Errorf("%w %q", ErrUnknownTransformer, r.Transformer)
	}
	for i := 0; i < len(r.Scope); i++ {
		if r.Scope[i] < 0x20 || r.Scope[i] == 0x7f {
			return compiled{}, fmt.Errorf("%w: scope contains control characters", ErrInvalidRule)
		}
	}
	switch {
	case r.Length < 0 || r.Length > maxLength:
		return compiled{}, fmt.Errorf("%w: length out of range", ErrInvalidRule)
	case r.MaxLength < 0 || r.MaxLength > maxLength:
		return compiled{}, fmt.Errorf("%w: max_length out of range", ErrInvalidRule)
	case r.Days < 0 || r.Days > transformers.MaxDays:
		return compiled{}, fmt.Errorf("%w: days out of range", ErrInvalidRule)
	case r.Unique && spec.Unique == transformers.UniqueUnsupported:
		return compiled{}, fmt.Errorf("%w: unique is not supported by %q", ErrInvalidRule, r.Transformer)
	case len(r.Paths) > 0 && r.Transformer != "json":
		return compiled{}, fmt.Errorf("%w: paths require the json transformer", ErrInvalidRule)
	}

	c := compiled{
		spec:   spec,
		scope:  r.Scope,
		maxLen: r.MaxLength,
		opts: transformers.Options{
			Unique:         r.Unique,
			PreserveDomain: r.PreserveDomain,
			PreserveLength: r.PreserveLength,
			Length:         r.Length,
			Days:           r.Days,
		},
	}
	for path, sub := range r.Paths {
		keys := strings.Split(path, ".")
		for _, k := range keys {
			if k == "" {
				return compiled{}, fmt.Errorf("%w: malformed JSON path %q", ErrInvalidRule, path)
			}
		}
		sc, err := compile(sub)
		if err != nil {
			return compiled{}, fmt.Errorf("path %q: %w", path, err)
		}
		c.paths = append(c.paths, compiledPath{path: path, keys: keys, rule: sc})
	}
	// Map order is random; apply paths in a fixed order.
	sort.Slice(c.paths, func(i, j int) bool { return c.paths[i].path < c.paths[j].path })
	return c, nil
}

type ruleKey struct{ schema, table, column string }

func parseRuleKey(key string) (ruleKey, error) {
	parts := strings.Split(key, ".")
	for _, p := range parts {
		if p == "" {
			return ruleKey{}, fmt.Errorf("%w: malformed rule key %q", ErrInvalidRule, key)
		}
	}
	switch len(parts) {
	case 2:
		return ruleKey{table: parts[0], column: parts[1]}, nil
	case 3:
		return ruleKey{schema: parts[0], table: parts[1], column: parts[2]}, nil
	}
	return ruleKey{}, fmt.Errorf("%w: rule key %q is not table.column or schema.table.column", ErrInvalidRule, key)
}
