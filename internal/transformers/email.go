package transformers

import (
	"strings"
	"unicode/utf8"
)

const (
	// EmailDomain is reserved by RFC 2606 / RFC 6761 and can never deliver.
	EmailDomain = "example.test"

	emailPrefix   = "user_"
	emailHex      = 20 // 80 bits
	emailMaxLocal = 64
)

// Email returns a syntactically valid address at a non-deliverable domain:
//
//	user_<20 hex>@example.test
//
// PreserveDomain keeps the source domain when it is a plain ASCII host name.
// PreserveLength matches the source length where a valid address allows it
// (the local part is clamped to 1..64 characters). The source local part is
// never used, so malformed input still yields a valid address.
func Email(in *Input) (string, error) {
	domain := EmailDomain
	if in.Opts.PreserveDomain {
		if d := emailDomainOf(in.Text); d != "" {
			domain = d
		}
	}

	local := 0 // 0 selects the default user_<hex> form
	if in.Opts.PreserveLength {
		local = utf8.RuneCountInString(in.Text) - 1 - len(domain)
		local = min(max(local, 1), emailMaxLocal)
	}
	if in.MaxLen > 0 {
		avail := in.MaxLen - 1 - len(domain)
		if avail < 1 {
			return "", ErrTooLong
		}
		if local == 0 && avail < len(emailPrefix)+emailHex {
			local = avail
		}
		local = min(local, avail)
	}

	out := make([]byte, 0, len(emailPrefix)+emailHex+1+len(domain)+local)
	if local == 0 {
		out = append(out, emailPrefix...)
		out = appendHex(out, in.S, emailHex)
	} else {
		out = appendChars(out, in.S, alnum, local)
	}
	out = append(out, '@')
	out = append(out, domain...)
	return string(out), nil
}

// emailDomainOf returns the domain of s if it looks like an ASCII host name,
// or "" otherwise.
func emailDomainOf(s string) string {
	at := strings.LastIndexByte(s, '@')
	if at <= 0 {
		return ""
	}
	d := s[at+1:]
	if len(d) == 0 || len(d) > 253 {
		return ""
	}
	prev := byte('.')
	for i := 0; i < len(d); i++ {
		c := d[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-':
			if prev == '.' {
				return ""
			}
		case c == '.':
			if prev == '.' || prev == '-' {
				return ""
			}
		default:
			return ""
		}
		prev = c
	}
	if prev == '.' || prev == '-' {
		return ""
	}
	return d
}
