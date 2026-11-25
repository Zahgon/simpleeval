// Ported from CPython 3.12 (Objects/unicodeobject.c, Objects/stringlib/codecs.h) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package codecs

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// Byte orders of the utf-16 and utf-32 decoders: byteOrderDetect reads a byte
// order mark if there is one and otherwise decodes little-endian (the native
// order of the platforms CPython runs on here).
const (
	byteOrderLittle = -1
	byteOrderDetect = 0
	byteOrderBig    = 1
)

// decode runs the codec's decoder. With final false an incomplete sequence at
// the end of b is left undecoded, as the stateful decoders behind
// codecs.StreamReader do; consumed counts the bytes decoded. bo is the
// initial byte order for the utf-16 and utf-32 codecs.
func (c *Codec) decode(b []byte, errors string, final bool, bo int) (s string, consumed int, err error) {
	switch c.Module {
	case "utf_8":
		return decodeUTF8(b, errors, final)
	case "utf_8_sig":
		if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
			s, n, err := decodeUTF8(b[3:], errors, final)
			return s, n + 3, err
		}
		return decodeUTF8(b, errors, final)
	case "ascii":
		s, err := decodeASCII(b, errors)
		return s, len(b), err
	case "latin_1":
		return DecodeLatin1(b), len(b), nil
	case "utf_16", "utf_16_le", "utf_16_be":
		s, n, _, err := decodeUTF16(b, errors, final, c.byteOrder(bo))
		return s, n, err
	case "utf_32", "utf_32_le", "utf_32_be":
		s, n, _, err := decodeUTF32(b, errors, final, c.byteOrder(bo))
		return s, n, err
	}
	s, err = c.table.decodeBytes(b, errors)
	return s, len(b), err
}

// byteOrder is the starting byte order of a utf-16 or utf-32 codec.
func (c *Codec) byteOrder(bo int) int {
	switch {
	case strings.HasSuffix(c.Module, "_le"):
		return byteOrderLittle
	case strings.HasSuffix(c.Module, "_be"):
		return byteOrderBig
	}
	return bo
}

func decodeUTF8(b []byte, errors string, final bool) (string, int, error) {
	if utf8.Valid(b) {
		return string(b), len(b), nil
	}
	var sb strings.Builder
	i := 0
	for i < len(b) {
		if b[i] < 0x80 {
			sb.WriteByte(b[i])
			i++
			continue
		}
		n, reason := utf8Sequence(b[i:])
		switch {
		case reason == "":
			sb.Write(b[i : i+n])
			i += n
			continue
		case reason == "unexpected end of data" && !final:
			return sb.String(), i, nil
		}
		next, err := decodeFailure(&sb, errors, "utf-8", b, i, i+n, reason)
		if err != nil {
			return "", 0, err
		}
		i = next
	}
	return sb.String(), i, nil
}

// utf8Sequence examines the sequence starting with the non-ASCII byte b[0].
// It returns the sequence length and "" when it is valid, or the length of
// the bad prefix and the reason CPython reports.
func utf8Sequence(b []byte) (int, string) {
	c := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c >= 0xE0 && c <= 0xEF:
		need = 2
		if c == 0xE0 {
			lo = 0xA0
		} else if c == 0xED {
			hi = 0x9F
		}
	case c >= 0xF0 && c <= 0xF4:
		need = 3
		if c == 0xF0 {
			lo = 0x90
		} else if c == 0xF4 {
			hi = 0x8F
		}
	default:
		return 1, "invalid start byte"
	}
	for j := 1; j <= need; j++ {
		if j >= len(b) {
			return len(b), "unexpected end of data"
		}
		if b[j] < lo || b[j] > hi {
			return j, "invalid continuation byte"
		}
		lo, hi = 0x80, 0xBF
	}
	return need + 1, ""
}

func decodeASCII(b []byte, errors string) (string, error) {
	var sb strings.Builder
	for i, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else if _, err := decodeFailure(&sb, errors, "ascii", b, i, i+1, "ordinal not in range(128)"); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// DecodeLatin1 is b.decode("latin-1"): each byte is the code point of the same
// value.
func DecodeLatin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

// decodeUTF16 decodes UTF-16 in byte order bo, reading a byte order mark
// first when bo is byteOrderDetect. It also returns the byte order in force
// at the end, which stays byteOrderDetect when fewer than two bytes were seen.
func decodeUTF16(b []byte, errors string, final bool, bo int) (string, int, int, error) {
	q := 0
	if bo == byteOrderDetect && len(b) >= 2 {
		switch {
		case b[0] == 0xff && b[1] == 0xfe:
			q, bo = 2, byteOrderLittle
		case b[0] == 0xfe && b[1] == 0xff:
			q, bo = 2, byteOrderBig
		}
	}
	encoding := "utf-16-le"
	if bo == byteOrderBig {
		encoding = "utf-16-be"
	}
	unit := func(i int) rune {
		if bo == byteOrderBig {
			return rune(b[i])<<8 | rune(b[i+1])
		}
		return rune(b[i+1])<<8 | rune(b[i])
	}
	var sb strings.Builder
	for q < len(b) {
		var start, end int
		var reason string
		switch {
		case len(b)-q < 2:
			if !final {
				return sb.String(), q, bo, nil
			}
			start, end, reason = q, len(b), "truncated data"
		case unit(q) < 0xD800 || unit(q) >= 0xE000:
			sb.WriteRune(unit(q))
			q += 2
			continue
		case unit(q) >= 0xDC00:
			start, end, reason = q, q+2, "illegal encoding"
		case len(b)-q < 4:
			if !final {
				return sb.String(), q, bo, nil
			}
			start, end, reason = q, len(b), "unexpected end of data"
		case unit(q+2) < 0xDC00 || unit(q+2) >= 0xE000:
			start, end, reason = q, q+2, "illegal UTF-16 surrogate"
		default:
			sb.WriteRune(0x10000 + (unit(q)-0xD800)<<10 + unit(q+2) - 0xDC00)
			q += 4
			continue
		}
		next, err := decodeFailure(&sb, errors, encoding, b, start, end, reason)
		if err != nil {
			return "", 0, bo, err
		}
		q = next
	}
	return sb.String(), q, bo, nil
}

// decodeUTF32 is decodeUTF16's counterpart for UTF-32.
func decodeUTF32(b []byte, errors string, final bool, bo int) (string, int, int, error) {
	q := 0
	if bo == byteOrderDetect && len(b) >= 4 {
		switch {
		case b[0] == 0xff && b[1] == 0xfe && b[2] == 0 && b[3] == 0:
			q, bo = 4, byteOrderLittle
		case b[0] == 0 && b[1] == 0 && b[2] == 0xfe && b[3] == 0xff:
			q, bo = 4, byteOrderBig
		}
	}
	encoding := "utf-32-le"
	if bo == byteOrderBig {
		encoding = "utf-32-be"
	}
	var sb strings.Builder
	for q < len(b) {
		var start, end int
		var reason string
		if len(b)-q < 4 {
			if !final {
				return sb.String(), q, bo, nil
			}
			start, end, reason = q, len(b), "truncated data"
		} else {
			var ch uint32
			if bo == byteOrderBig {
				ch = uint32(b[q])<<24 | uint32(b[q+1])<<16 | uint32(b[q+2])<<8 | uint32(b[q+3])
			} else {
				ch = uint32(b[q+3])<<24 | uint32(b[q+2])<<16 | uint32(b[q+1])<<8 | uint32(b[q])
			}
			switch {
			case ch >= 0xD800 && ch < 0xE000:
				start, end, reason = q, q+4, "code point in surrogate code point range(0xd800, 0xe000)"
			case ch >= 0x110000:
				start, end, reason = q, q+4, "code point not in range(0x110000)"
			default:
				sb.WriteRune(rune(ch))
				q += 4
				continue
			}
		}
		next, err := decodeFailure(&sb, errors, encoding, b, start, end, reason)
		if err != nil {
			return "", 0, bo, err
		}
		q = next
	}
	return sb.String(), q, bo, nil
}

// charmapSpec is the generated description of a charmap codec.
type charmapSpec struct {
	decode          string
	encodeOverrides map[rune]int
}

// charmap is a single-byte codec built from its charmapSpec.
type charmap struct {
	decode [256]rune
	encode map[rune]byte
}

const undefinedByte = 0xFFFE

var (
	charmapsMu sync.Mutex
	charmaps   = map[string]*charmap{}
)

func buildCharmap(module string) *charmap {
	charmapsMu.Lock()
	defer charmapsMu.Unlock()
	if m, ok := charmaps[module]; ok {
		return m
	}
	spec := charmapTables[module]
	m := &charmap{encode: map[rune]byte{}}
	copy(m.decode[:], textutil.Runes(spec.decode))
	for i, r := range m.decode {
		if r != undefinedByte {
			m.encode[r] = byte(i)
		}
	}
	for r, v := range spec.encodeOverrides {
		if v < 0 {
			delete(m.encode, r)
		} else {
			m.encode[r] = byte(v)
		}
	}
	charmaps[module] = m
	return m
}

func (m *charmap) decodeBytes(b []byte, errors string) (string, error) {
	var sb strings.Builder
	for i, c := range b {
		if r := m.decode[c]; r != undefinedByte {
			sb.WriteRune(r)
		} else if _, err := decodeFailure(&sb, errors, "charmap", b, i, i+1, "character maps to <undefined>"); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}
