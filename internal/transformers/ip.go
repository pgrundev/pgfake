package transformers

import (
	"net/netip"
	"strconv"
	"strings"
)

// IPAddr returns an address of the same family in a range that is never
// routed on the public internet:
//
//	IPv4 -> 10.0.0.0/8      (RFC 1918)
//	IPv6 -> 2001:db8::/32   (RFC 3849, documentation)
//
// A "/prefix" suffix is kept; for the cidr type the host bits are cleared so
// the result stays a valid network. Unparseable input yields an IPv4 address.
func IPAddr(in *Input) (string, error) {
	host, suffix := strings.TrimSpace(in.Text), ""
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host, suffix = host[:i], host[i+1:]
	}
	src, err := netip.ParseAddr(host)

	var out netip.Addr
	if err == nil && src.Is6() && !src.Is4In6() {
		b := [16]byte{0x20, 0x01, 0x0d, 0xb8}
		in.S.Fill(b[4:])
		out = netip.AddrFrom16(b)
	} else {
		b := [4]byte{10}
		in.S.Fill(b[1:])
		out = netip.AddrFrom4(b)
	}
	if err != nil {
		return out.String(), nil
	}

	bits := out.BitLen()
	if suffix != "" {
		n, perr := strconv.Atoi(suffix)
		if perr != nil || n < 0 || n > bits {
			return out.String(), nil
		}
		bits = n
	}
	if in.CIDR {
		out = netip.PrefixFrom(out, bits).Masked().Addr()
	}
	if suffix == "" {
		return out.String(), nil
	}
	return out.String() + "/" + strconv.Itoa(bits), nil
}
