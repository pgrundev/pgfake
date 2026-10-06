// Package deterministic derives pseudo-random bytes and keyed permutations
// from a secret key with HMAC-SHA256.
//
// Everything pgfake outputs is a pure function of what this package produces,
// so the constructions below are frozen for algorithm version v1:
//
//	ns      = "pgfake:v1:" + transformer + ":" + scope
//	block 0 = HMAC-SHA256(key, ns || 0x00 || value)
//	block i = HMAC-SHA256(key, ns || 0x02 || block 0 || uint32be(i))   for i >= 1
//	stream  = block 0 || block 1 || ...
//
// and for the keyed permutation (a 10-round alternating Feistel network over
// [0,a) x [0,b)):
//
//	F(round, half) = uint64be(HMAC-SHA256(key,
//	    ns || 0x01 || byte(round) || uint64be(a) || uint64be(b) || uint64be(half))[:8])
//
// The separator bytes 0x00, 0x01 and 0x02 cannot appear in ns (transformer
// names are fixed identifiers and scopes containing control characters are
// rejected), so the three message families never collide.
package deterministic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// Version is the algorithm version baked into every HMAC namespace.
const Version = "v1"

const (
	prefix    = "pgfake:" + Version + ":"
	blockSize = sha256.Size
	rounds    = 10

	sepValue   = 0x00
	sepPermute = 0x01
	sepExpand  = 0x02

	// Streams whose scratch buffer grew beyond this are not worth pooling, so
	// one huge value does not pin memory forever.
	maxPooledBuf = 64 << 10
)

// Deriver creates Streams keyed with a single secret. It is safe for
// concurrent use.
type Deriver struct {
	key []byte
}

// New returns a Deriver for key. The key is copied.
func New(key []byte) *Deriver {
	return &Deriver{key: append([]byte(nil), key...)}
}

// NewStream returns a fresh Stream. Streams are reusable: callers are
// expected to pool them and call Wipe between uses.
func (d *Deriver) NewStream() *Stream {
	return &Stream{mac: hmac.New(sha256.New, d.key), sum: make([]byte, 0, blockSize)}
}

// Wipe erases any source value left in the scratch buffer. It reports whether
// the stream is worth keeping for reuse.
func (s *Stream) Wipe() bool {
	clear(s.buf[:s.dirty])
	s.dirty = 0
	return cap(s.buf) <= maxPooledBuf
}

// Stream is a deterministic byte stream bound to one (namespace, value) pair.
// A Stream is not safe for concurrent use.
type Stream struct {
	mac   hash.Hash
	buf   []byte // ns, followed by scratch space for the current message
	sum   []byte
	nsLen int
	dirty int // high-water mark of buf since Get

	seed  [blockSize]byte
	block [blockSize]byte
	pos   int
	ctr   uint32
}

// Begin starts a new namespace for the given transformer. Scope parts are
// appended with Scope, then Seed binds the stream to a value.
func (s *Stream) Begin(transformer string) {
	s.buf = append(s.buf[:0], prefix...)
	s.buf = append(s.buf, transformer...)
	s.buf = append(s.buf, ':')
	s.nsLen = len(s.buf)
}

// Scope appends part to the namespace scope.
func (s *Stream) Scope(part string) {
	s.buf = append(s.buf[:s.nsLen], part...)
	s.nsLen = len(s.buf)
}

// Seed binds the stream to value and rewinds it.
func (s *Stream) Seed(value string) {
	s.buf = append(s.buf[:s.nsLen], sepValue)
	s.buf = append(s.buf, value...)
	s.dirty = max(s.dirty, len(s.buf))
	s.hmac(s.seed[:])
	s.block = s.seed
	s.pos = 0
	s.ctr = 0
}

func (s *Stream) hmac(dst []byte) {
	s.mac.Reset()
	s.mac.Write(s.buf)
	s.sum = s.mac.Sum(s.sum[:0])
	copy(dst, s.sum)
}

func (s *Stream) next() {
	s.ctr++
	s.buf = append(s.buf[:s.nsLen], sepExpand)
	s.buf = append(s.buf, s.seed[:]...)
	s.buf = binary.BigEndian.AppendUint32(s.buf, s.ctr)
	s.hmac(s.block[:])
	s.pos = 0
}

// Byte returns the next stream byte.
func (s *Stream) Byte() byte {
	if s.pos == blockSize {
		s.next()
	}
	b := s.block[s.pos]
	s.pos++
	return b
}

// Fill fills p with the next len(p) stream bytes.
func (s *Stream) Fill(p []byte) {
	for len(p) > 0 {
		if s.pos == blockSize {
			s.next()
		}
		n := copy(p, s.block[s.pos:])
		s.pos += n
		p = p[n:]
	}
}

// Uint64 returns the next 8 stream bytes as a big-endian integer.
func (s *Stream) Uint64() uint64 {
	var b [8]byte
	s.Fill(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// Intn returns a value in [0, n) from the next 8 stream bytes. The modulo
// bias is below n/2^64 and ignored. n must be > 0.
func (s *Stream) Intn(n uint64) uint64 { return s.Uint64() % n }

// Pick returns an unbiased value in [0, n) for 1 <= n <= 256, consuming one
// byte per attempt (rejection sampling).
func (s *Stream) Pick(n int) int {
	limit := 256 - 256%n
	for {
		if b := int(s.Byte()); b < limit {
			return b % n
		}
	}
}

// Permute maps x in [0, a*b) to another value in [0, a*b). For a fixed key and
// namespace the mapping is a bijection, so distinct inputs never collide. It
// depends on the namespace only, not on the seeded value. Both a and b must be
// in [1, 2^32].
func (s *Stream) Permute(x, a, b uint64) uint64 {
	l, r := x/b, x%b
	for i := 0; i < rounds; i++ {
		if i%2 == 0 {
			l = (l + s.round(i, a, b, r)%a) % a
		} else {
			r = (r + s.round(i, a, b, l)%b) % b
		}
	}
	return l*b + r
}

func (s *Stream) round(i int, a, b, half uint64) uint64 {
	s.buf = append(s.buf[:s.nsLen], sepPermute, byte(i))
	s.buf = binary.BigEndian.AppendUint64(s.buf, a)
	s.buf = binary.BigEndian.AppendUint64(s.buf, b)
	s.buf = binary.BigEndian.AppendUint64(s.buf, half)
	var out [blockSize]byte
	s.hmac(out[:])
	return binary.BigEndian.Uint64(out[:8])
}
