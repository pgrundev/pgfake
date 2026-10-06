package transformers

import "strings"

// Characters other than digits that may appear in a phone number without
// making it "malformed". Letters are limited to those of "x" / "ext" so that
// vanity numbers are not partially preserved.
const phonePunct = " +-.()/*:\t"

// phoneExt are the characters that start an extension ("x42", "ext. 42",
// "#42", ";ext=42"). Digits after one are replaced but are not part of the
// number itself.
const phoneExt = "xXeEtT#;,"

// phoneMaxPermute is the longest national number that fits the keyed
// permutation (10^9 per Feistel half).
const phoneMaxPermute = 18

// Phone replaces the digits of a phone number and keeps its formatting.
//
//   - A leading "+<country code>" is kept.
//   - North American numbers get the unassigned area code 555:
//     "+1 310 555 1234" -> "+1 555 xxx xxxx".
//   - Other numbers keep leading trunk zeros; all other digits are replaced.
//   - Extension digits ("x42") are replaced as well.
//   - Input that does not look like a phone number yields "+1555xxxxxxx".
//
// With Unique the national digits go through a keyed permutation instead:
// distinct inputs of the same format then never collide, at the price of
// leaving the 555 range.
func Phone(in *Input) (string, error) {
	src := in.Text
	var buf [24]byte
	ds := buf[:0]
	plus := false
	ext := -1 // number of digits before the extension, or -1
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c >= '0' && c <= '9':
			ds = append(ds, c)
		case c == '+' && len(ds) == 0:
			plus = true
		case strings.IndexByte(phonePunct, c) >= 0:
		case strings.IndexByte(phoneExt, c) >= 0:
			if ext < 0 && len(ds) > 0 {
				ext = len(ds)
			}
		default:
			return phoneFallback(in), nil
		}
	}
	if ext < 0 {
		ext = len(ds)
	}
	if ext < 5 {
		return phoneFallback(in), nil
	}

	cc := 0
	switch {
	case plus:
		cc = countryCodeLen(ds)
	case ext == 11 && ds[0] == '1':
		cc = 1
	}
	if ext-cc < 4 {
		cc = 0
	}
	nat, extension := ds[cc:ext], ds[ext:]

	nanp := len(nat) == 10 && ((cc == 1 && ds[0] == '1') || (cc == 0 && !plus && nat[0] >= '2'))
	switch {
	case in.Opts.Unique && len(nat) <= phoneMaxPermute && len(extension) <= phoneMaxPermute:
		permuteDigits(in, nat)
		if len(extension) > 0 {
			permuteDigits(in, extension)
		}
		extension = nil
	case nanp:
		copy(nat, "555")
		fillDigits(in, nat[3:])
	default:
		i := 0
		for i < len(nat)-1 && nat[i] == '0' {
			i++
		}
		nat[i] = '1' + byte(in.S.Pick(9))
		fillDigits(in, nat[i+1:])
	}
	fillDigits(in, extension)

	out := []byte(src)
	k := 0
	for i, c := range out {
		if c >= '0' && c <= '9' {
			out[i] = ds[k]
			k++
		}
	}
	return string(out), nil
}

func phoneFallback(in *Input) string {
	out := make([]byte, 0, 12)
	out = append(out, "+1555"...)
	return string(appendChars(out, in.S, digits, 7))
}

func fillDigits(in *Input, p []byte) {
	for i := range p {
		p[i] = '0' + byte(in.S.Pick(10))
	}
}

// permuteDigits replaces the decimal string p with its image under the keyed
// permutation of all decimal strings of the same length.
func permuteDigits(in *Input, p []byte) {
	var x uint64
	for _, c := range p {
		x = x*10 + uint64(c-'0')
	}
	y := in.S.Permute(x, pow10(len(p)/2), pow10(len(p)-len(p)/2))
	for i := len(p) - 1; i >= 0; i-- {
		p[i] = '0' + byte(y%10)
		y /= 10
	}
}

func pow10(n int) uint64 {
	v := uint64(1)
	for ; n > 0; n-- {
		v *= 10
	}
	return v
}

// countryCodeLen returns the length of the ITU-T E.164 country calling code
// at the start of ds, or 0 if there is none.
func countryCodeLen(ds []byte) int {
	switch ds[0] {
	case '0':
		return 0
	case '1', '7':
		return 1
	}
	if len(ds) < 2 {
		return 0
	}
	switch (ds[0]-'0')*10 + (ds[1] - '0') {
	case 20, 27, 30, 31, 32, 33, 34, 36, 39, 40, 41, 43, 44, 45, 46, 47, 48, 49,
		51, 52, 53, 54, 55, 56, 57, 58, 60, 61, 62, 63, 64, 65, 66,
		81, 82, 84, 86, 90, 91, 92, 93, 94, 95, 98:
		return 2
	}
	return 3
}
