package deterministic

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func mac(key []byte, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func newStream(key, transformer, scope, value string) *Stream {
	s := New([]byte(key)).NewStream()
	s.Begin(transformer)
	s.Scope(scope)
	s.Seed(value)
	return s
}

// The construction documented in the package comment, written out
// independently with crypto/hmac.
func TestStreamConstruction(t *testing.T) {
	key := []byte("test-key-1")
	ns := []byte("pgfake:v1:email:user_email")
	value := []byte("alex@gmail.com")

	block0 := mac(key, ns, []byte{0x00}, value)
	want := append([]byte(nil), block0...)
	for i := uint32(1); i <= 3; i++ {
		want = append(want, mac(key, ns, []byte{0x02}, block0, binary.BigEndian.AppendUint32(nil, i))...)
	}

	s := newStream("test-key-1", "email", "user_email", "alex@gmail.com")
	got := make([]byte, len(want))
	s.Fill(got)
	if !bytes.Equal(got, want) {
		t.Fatalf("stream mismatch\n got %x\nwant %x", got, want)
	}

	// Byte-wise reads see the same stream.
	s.Seed("alex@gmail.com")
	for i, w := range want {
		if b := s.Byte(); b != w {
			t.Fatalf("byte %d = %02x, want %02x", i, b, w)
		}
	}
}

func TestScopePartsConcatenate(t *testing.T) {
	a := newStream("k", "email", "public.users.email", "v")
	b := New([]byte("k")).NewStream()
	b.Begin("email")
	for _, p := range []string{"public", ".", "users", ".", "email"} {
		b.Scope(p)
	}
	b.Seed("v")
	if a.Uint64() != b.Uint64() {
		t.Fatal("scope parts must concatenate")
	}
}

func TestStreamSeparation(t *testing.T) {
	base := newStream("k", "email", "s", "v").Uint64()
	for name, s := range map[string]*Stream{
		"key":         newStream("k2", "email", "s", "v"),
		"transformer": newStream("k", "token", "s", "v"),
		"scope":       newStream("k", "email", "s2", "v"),
		"value":       newStream("k", "email", "s", "v2"),
		"boundary":    newStream("k", "email", "", "sv"),
	} {
		if s.Uint64() == base {
			t.Errorf("changing the %s did not change the stream", name)
		}
	}
}

func TestStreamReuse(t *testing.T) {
	s := newStream("k", "email", "s", "a much longer value than the next one")
	s.Uint64()
	if !s.Wipe() {
		t.Fatal("small stream should be reusable")
	}
	if bytes.Contains(s.buf[:cap(s.buf)], []byte("longer value")) {
		t.Fatal("Wipe left the source value in the buffer")
	}
	s.Begin("email")
	s.Scope("s")
	s.Seed("v")
	if got, want := s.Uint64(), newStream("k", "email", "s", "v").Uint64(); got != want {
		t.Fatal("reused stream differs from a fresh one")
	}

	big := newStream("k", "email", "s", string(make([]byte, maxPooledBuf+1)))
	if big.Wipe() {
		t.Fatal("oversized stream should not be reused")
	}
}

func TestPick(t *testing.T) {
	s := newStream("k", "t", "s", "v")
	for _, n := range []int{1, 2, 10, 26, 36, 62, 255, 256} {
		seen := make([]bool, n)
		for i := 0; i < 20000; i++ {
			v := s.Pick(n)
			if v < 0 || v >= n {
				t.Fatalf("Pick(%d) = %d", n, v)
			}
			seen[v] = true
		}
		for v, ok := range seen {
			if !ok {
				t.Fatalf("Pick(%d) never produced %d", n, v)
			}
		}
	}
}

func TestPermuteIsBijection(t *testing.T) {
	s := newStream("k", "hash", "ids", "")
	for _, d := range []struct{ a, b uint64 }{
		{1, 1}, {1, 10}, {8, 16}, {10, 10}, {100, 1000}, {128, 256},
	} {
		n := d.a * d.b
		seen := make([]bool, n)
		fixed := 0
		for x := uint64(0); x < n; x++ {
			y := s.Permute(x, d.a, d.b)
			if y >= n {
				t.Fatalf("Permute(%d) = %d out of [0,%d)", x, y, n)
			}
			if seen[y] {
				t.Fatalf("collision in domain %dx%d at %d", d.a, d.b, y)
			}
			seen[y] = true
			if x == y {
				fixed++
			}
		}
		if n >= 1000 && fixed > 20 {
			t.Errorf("domain %dx%d: %d fixed points, permutation looks like identity", d.a, d.b, fixed)
		}
	}
}

func TestPermuteLargeDomains(t *testing.T) {
	s := newStream("k", "hash", "ids", "")
	const two32 = 1 << 32
	seen := map[uint64]bool{}
	for x := uint64(0); x < 50000; x++ {
		y := s.Permute(x, two32, two32)
		if seen[y] {
			t.Fatalf("collision at %d", x)
		}
		seen[y] = true
	}
	if y := s.Permute(1<<63-1, 1<<31, two32); y >= 1<<63 {
		t.Fatalf("63-bit permutation left its domain: %d", y)
	}
	// Depends on the namespace and key, not on the seeded value.
	a := newStream("k", "hash", "ids", "x").Permute(42, two32, two32)
	b := newStream("k", "hash", "ids", "y").Permute(42, two32, two32)
	c := newStream("k", "hash", "other", "x").Permute(42, two32, two32)
	d := newStream("k2", "hash", "ids", "x").Permute(42, two32, two32)
	if a != b || a == c || a == d {
		t.Fatalf("permutation keying is wrong: %d %d %d %d", a, b, c, d)
	}
}

// The round function documented in the package comment.
func TestPermuteConstruction(t *testing.T) {
	key := []byte("k")
	ns := []byte("pgfake:v1:hash:ids")
	const a, b = 100, 1000
	round := func(i int, half uint64) uint64 {
		msg := append([]byte{0x01, byte(i)}, binary.BigEndian.AppendUint64(nil, a)...)
		msg = binary.BigEndian.AppendUint64(msg, b)
		msg = binary.BigEndian.AppendUint64(msg, half)
		return binary.BigEndian.Uint64(mac(key, ns, msg)[:8])
	}
	x := uint64(12345)
	l, r := x/b, x%b
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			l = (l + round(i, r)%a) % a
		} else {
			r = (r + round(i, l)%b) % b
		}
	}
	if got, want := newStream("k", "hash", "ids", "").Permute(x, a, b), l*b+r; got != want {
		t.Fatalf("Permute = %d, want %d", got, want)
	}
}
