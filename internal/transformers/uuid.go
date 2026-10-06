package transformers

import "encoding/hex"

// UUIDv4 returns a UUID built from 122 digest bits with the version (4) and
// variant (RFC 4122) bits set.
func UUIDv4(in *Input) (string, error) {
	var b [16]byte
	in.S.Fill(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return FormatUUID(b), nil
}

// FormatUUID returns the canonical lowercase 8-4-4-4-12 form of b.
func FormatUUID(b [16]byte) string {
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}

// ParseUUID parses the canonical 8-4-4-4-12 form.
func ParseUUID(s string) (b [16]byte, ok bool) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return b, false
	}
	var h [32]byte
	copy(h[0:8], s[0:8])
	copy(h[8:12], s[9:13])
	copy(h[12:16], s[14:18])
	copy(h[16:20], s[19:23])
	copy(h[20:32], s[24:36])
	if _, err := hex.Decode(b[:], h[:]); err != nil {
		return [16]byte{}, false
	}
	return b, true
}
