package pgfake

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pgrundev/pgfake/internal/deterministic"
	"github.com/pgrundev/pgfake/internal/transformers"
)

const defaultSchema = "public"

// apply transforms one value of any supported Go type.
func (f *Faker) apply(loc *location, value any, c *compiled) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch c.spec.Name {
	case "copy":
		return value, nil
	case "null":
		return nil, nil
	}
	var (
		out any
		err error
	)
	if c.spec.Name == "json" {
		out, err = f.applyJSON(loc, value, c)
	} else {
		out, err = f.applyTyped(loc, value, c)
	}
	if err != nil {
		// Never hand back a partial result next to an error.
		return nil, loc.wrap(c.spec.Name, err)
	}
	return out, nil
}

func (f *Faker) applyTyped(loc *location, value any, c *compiled) (any, error) {
	pt := parsePGType(loc.pgtype)

	switch v := value.(type) {
	case string:
		if pt.array {
			return nil, errArrayLiteral
		}
		return f.text(loc, v, pt.kind, pt.bits, &pt, c)
	case []byte:
		if v == nil {
			return value, nil
		}
		if pt.array {
			return nil, errArrayLiteral
		}
		out, err := f.text(loc, string(v), pt.kind, pt.bits, &pt, c)
		return []byte(out), err
	case json.RawMessage:
		if v == nil {
			return value, nil
		}
		out, err := f.text(loc, string(v), pt.kind, pt.bits, &pt, c)
		return json.RawMessage(out), err

	case int:
		n, err := f.signed(loc, int64(v), 63, &pt, c)
		return int(n), err
	case int8:
		n, err := f.signed(loc, int64(v), 7, &pt, c)
		return int8(n), err
	case int16:
		n, err := f.signed(loc, int64(v), 15, &pt, c)
		return int16(n), err
	case int32:
		n, err := f.signed(loc, int64(v), 31, &pt, c)
		return int32(n), err
	case int64:
		return f.signed(loc, v, 63, &pt, c)
	case uint:
		n, err := f.unsigned(loc, uint64(v), 64, &pt, c)
		return uint(n), err
	case uint8:
		n, err := f.unsigned(loc, uint64(v), 8, &pt, c)
		return uint8(n), err
	case uint16:
		n, err := f.unsigned(loc, uint64(v), 16, &pt, c)
		return uint16(n), err
	case uint32:
		n, err := f.unsigned(loc, uint64(v), 32, &pt, c)
		return uint32(n), err
	case uint64:
		return f.unsigned(loc, v, 64, &pt, c)
	case json.Number:
		if _, err := strconv.ParseInt(string(v), 10, 64); err != nil {
			return nil, mismatch(c, transformers.Numeric)
		}
		out, err := f.text(loc, string(v), transformers.Int, intBits(63, &pt), &pt, c)
		return json.Number(out), err
	case bool:
		return nil, mismatch(c, transformers.Bool)
	case float32, float64:
		return nil, mismatch(c, transformers.Numeric)

	case time.Time:
		return f.time(loc, v, &pt, c)
	case [16]byte:
		out, err := f.text(loc, transformers.FormatUUID(v), transformers.UUID, 0, &pt, c)
		if err != nil {
			return nil, err
		}
		b, ok := transformers.ParseUUID(out)
		if !ok {
			return nil, ErrInvalidInput
		}
		return b, nil
	case netip.Addr:
		if !v.IsValid() {
			return value, nil
		}
		out, err := f.text(loc, v.String(), transformers.IP, 0, &pt, c)
		if err != nil {
			return nil, err
		}
		addr, err := netip.ParseAddr(out)
		if err != nil {
			return nil, ErrInvalidInput
		}
		return addr, nil
	case netip.Prefix:
		if !v.IsValid() {
			return value, nil
		}
		return f.prefix(loc, v, &pt, c)
	case net.IP:
		if v == nil {
			return value, nil
		}
		out, err := f.text(loc, v.String(), transformers.IP, 0, &pt, c)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(out)
		if ip == nil {
			return nil, ErrInvalidInput
		}
		return ip, nil

	case []string:
		if v == nil {
			return value, nil
		}
		et := elemType(&pt)
		out := make([]string, len(v))
		for i, s := range v {
			var err error
			if out[i], err = f.text(loc, s, et.kind, et.bits, &et, c); err != nil {
				return nil, err
			}
		}
		return out, nil
	case []any:
		if v == nil {
			return value, nil
		}
		eloc := *loc
		eloc.pgtype = pt.elem
		out := make([]any, len(v))
		for i, e := range v {
			var err error
			if out[i], err = f.apply(&eloc, e, c); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %T", ErrUnsupportedValue, value)
}

var errArrayLiteral = fmt.Errorf("%w: array literals in text form are not supported, pass a Go slice", ErrUnsupportedValue)

func mismatch(c *compiled, k transformers.Kind) error {
	return fmt.Errorf("%w: %q cannot produce %s values", ErrTypeMismatch, c.spec.Name, k)
}

func elemType(pt *pgType) pgType {
	if pt.array {
		return parsePGType(pt.elem)
	}
	return *pt
}

// intBits narrows the Go integer width to the column's width.
func intBits(goBits int, pt *pgType) int {
	if pt.kind == transformers.Int && pt.bits < goBits {
		return pt.bits
	}
	return goBits
}

func (f *Faker) signed(loc *location, v int64, goBits int, pt *pgType, c *compiled) (int64, error) {
	out, err := f.text(loc, strconv.FormatInt(v, 10), transformers.Int, intBits(goBits, pt), pt, c)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return 0, ErrInvalidInput
	}
	return n, nil
}

func (f *Faker) unsigned(loc *location, v uint64, goBits int, pt *pgType, c *compiled) (uint64, error) {
	out, err := f.text(loc, strconv.FormatUint(v, 10), transformers.Int, intBits(goBits, pt), pt, c)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(out, 10, 64)
	if err != nil {
		return 0, ErrInvalidInput
	}
	return n, nil
}

func (f *Faker) time(loc *location, v time.Time, pt *pgType, c *compiled) (any, error) {
	kind := transformers.TimestampTZ
	switch pt.kind {
	case transformers.Date, transformers.Timestamp:
		kind = pt.kind
	}
	if kind == transformers.Date {
		const layout = "2006-01-02"
		out, err := f.text(loc, v.Format(layout), kind, 0, pt, c)
		if err != nil {
			return nil, err
		}
		t, err := time.ParseInLocation(layout, out, v.Location())
		if err != nil {
			return nil, ErrInvalidInput
		}
		return t, nil
	}
	out, err := f.text(loc, v.UTC().Format(time.RFC3339Nano), kind, 0, pt, c)
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, out)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return t.In(v.Location()), nil
}

func (f *Faker) prefix(loc *location, v netip.Prefix, pt *pgType, c *compiled) (any, error) {
	// Match PostgreSQL's text output: host addresses print without "/bits".
	text := v.String()
	if v.Bits() == v.Addr().BitLen() && !pt.cidr {
		text = v.Addr().String()
	}
	out, err := f.text(loc, text, transformers.IP, 0, pt, c)
	if err != nil {
		return nil, err
	}
	if strings.IndexByte(out, '/') < 0 {
		addr, err := netip.ParseAddr(out)
		if err != nil {
			return nil, ErrInvalidInput
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	p, err := netip.ParsePrefix(out)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return p, nil
}

// text runs a transformer on the text form of a value. It is the single place
// where the deterministic stream is seeded.
func (f *Faker) text(loc *location, text string, kind transformers.Kind, bits int, pt *pgType, c *compiled) (string, error) {
	if !c.spec.Accepts(kind) {
		return "", mismatch(c, kind)
	}
	if text == "" {
		// Like NULL, the empty string carries no data and is preserved.
		return "", nil
	}

	in := f.pool.Get().(*transformers.Input)
	defer f.release(in)

	canonical := text
	if pt.citext {
		canonical = strings.ToLower(text)
	}
	seed(in.S, loc, c, canonical)

	in.Text = text
	in.Kind = kind
	in.Bits = bits
	in.CIDR = pt.cidr
	in.Opts = c.opts
	in.MaxLen = c.maxLen
	if in.MaxLen == 0 {
		in.MaxLen = pt.maxLen
	}

	out, err := c.spec.Fn(in)
	if err != nil {
		return "", err
	}
	if in.MaxLen > 0 && utf8.RuneCountInString(out) > in.MaxLen {
		if !c.spec.Truncate {
			return "", ErrValueTooLong
		}
		out = truncate(out, in.MaxLen)
	}
	return out, nil
}

func (f *Faker) release(in *transformers.Input) {
	in.Text = ""
	if in.S.Wipe() {
		f.pool.Put(in)
	}
}

// seed binds the stream to "pgfake:v1:<transformer>:<scope>" and the value.
// An explicit scope is used verbatim; otherwise the scope is the value's
// location.
func seed(s *deterministic.Stream, loc *location, c *compiled, canonical string) {
	s.Begin(c.spec.Name)
	switch {
	case c.scope != "":
		s.Scope(c.scope)
	case loc.table != "" || loc.column != "":
		schema := loc.schema
		if schema == "" {
			schema = defaultSchema
		}
		s.Scope(schema)
		s.Scope(".")
		s.Scope(loc.table)
		s.Scope(".")
		s.Scope(loc.column)
		if loc.path != "" {
			s.Scope(".")
			s.Scope(loc.path)
		}
	default:
		s.Scope(loc.path)
	}
	s.Seed(canonical)
}

func truncate(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
