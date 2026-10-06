package pgfake

import (
	"strconv"
	"strings"

	"github.com/pgrundev/pgfake/internal/transformers"
)

// pgType is what pgfake needs to know about a PostgreSQL type.
type pgType struct {
	kind   transformers.Kind
	bits   int  // magnitude bits for integer types
	maxLen int  // length modifier of character types, 0 = none
	citext bool // compare case-insensitively
	cidr   bool
	array  bool
	elem   string // element type name when array
}

// parsePGType understands the type names produced by format_type() and
// pg_type.typname. Anything it does not recognize is treated as text.
func parsePGType(s string) pgType {
	// Fast path for the spellings drivers report most often.
	if t, ok := baseType(s); ok {
		return t
	}

	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimPrefix(t, "pg_catalog.")
	if strings.HasSuffix(t, "[]") {
		return pgType{array: true, elem: strings.TrimSuffix(t, "[]")}
	}
	if strings.HasPrefix(t, "_") {
		return pgType{array: true, elem: t[1:]}
	}

	mod := 0
	if i := strings.IndexByte(t, '('); i >= 0 {
		if j := strings.IndexByte(t[i:], ')'); j > 0 {
			arg, _, _ := strings.Cut(t[i+1:i+j], ",")
			mod, _ = strconv.Atoi(strings.TrimSpace(arg))
			t = strings.TrimSpace(t[:i]) + t[i+j+1:]
		}
	}
	pt, _ := baseType(t)
	if pt.kind == transformers.Text && !pt.citext && mod > 0 && isCharType(t) {
		pt.maxLen = mod
	}
	return pt
}

func isCharType(t string) bool {
	switch t {
	case "varchar", "character varying", "char", "character", "bpchar":
		return true
	}
	return false
}

func baseType(t string) (pgType, bool) {
	switch t {
	case "", "text", "varchar", "character varying", "char", "character", "bpchar", "name":
		return pgType{kind: transformers.Text}, true
	case "citext":
		return pgType{kind: transformers.Text, citext: true}, true
	case "smallint", "int2", "smallserial", "serial2":
		return pgType{kind: transformers.Int, bits: 15}, true
	case "integer", "int", "int4", "serial", "serial4":
		return pgType{kind: transformers.Int, bits: 31}, true
	case "bigint", "int8", "bigserial", "serial8":
		return pgType{kind: transformers.Int, bits: 63}, true
	case "uuid":
		return pgType{kind: transformers.UUID}, true
	case "date":
		return pgType{kind: transformers.Date}, true
	case "timestamp", "timestamp without time zone":
		return pgType{kind: transformers.Timestamp}, true
	case "timestamptz", "timestamp with time zone":
		return pgType{kind: transformers.TimestampTZ}, true
	case "inet":
		return pgType{kind: transformers.IP}, true
	case "cidr":
		return pgType{kind: transformers.IP, cidr: true}, true
	case "json", "jsonb":
		return pgType{kind: transformers.JSON}, true
	case "boolean", "bool":
		return pgType{kind: transformers.Bool}, true
	case "numeric", "decimal", "real", "float4", "double precision", "float8", "money":
		return pgType{kind: transformers.Numeric}, true
	}
	return pgType{kind: transformers.Text}, false
}
