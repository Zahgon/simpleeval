// Ported from CPython 3.12 (Objects/unicodeobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package codecs

import (
	"unicode/utf16"

	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// Encode is codec.encode(s, errors)[0].
func (c *Codec) Encode(s string, errors string) ([]byte, error) {
	switch c.Module {
	case "utf_8", "utf_8_sig":
		var out []byte
		if c.Module == "utf_8_sig" {
			out = []byte{0xef, 0xbb, 0xbf}
		}
		if textutil.IndexSurrogate(s) < 0 {
			return append(out, s...), nil
		}
		return encodeUTF(textutil.Runes(s), errors, "utf-8", out, textutil.AppendRune)
	case "ascii":
		return encodeSingleByte(textutil.Runes(s), errors, "ascii", "ordinal not in range(128)", func(r rune) (byte, bool) {
			return byte(r), r < 0x80
		})
	case "latin_1":
		return encodeSingleByte(textutil.Runes(s), errors, "latin-1", "ordinal not in range(256)", func(r rune) (byte, bool) {
			return byte(r), r < 0x100
		})
	case "utf_16", "utf_16_le", "utf_16_be":
		var out []byte
		if c.Module == "utf_16" {
			out = append(out, 0xff, 0xfe)
		}
		bigEndian := c.Module == "utf_16_be"
		encoding := map[bool]string{false: "utf-16-le", true: "utf-16-be"}[bigEndian]
		if c.Module == "utf_16" {
			encoding = "utf-16"
		}
		return encodeUTF(textutil.Runes(s), errors, encoding, out, func(b []byte, r rune) []byte {
			units := []uint16{uint16(r)}
			if r >= 0x10000 {
				hi, lo := utf16.EncodeRune(r)
				units = []uint16{uint16(hi), uint16(lo)}
			}
			for _, u := range units {
				if bigEndian {
					b = append(b, byte(u>>8), byte(u))
				} else {
					b = append(b, byte(u), byte(u>>8))
				}
			}
			return b
		})
	case "utf_32", "utf_32_le", "utf_32_be":
		var out []byte
		if c.Module == "utf_32" {
			out = append(out, 0xff, 0xfe, 0, 0)
		}
		bigEndian := c.Module == "utf_32_be"
		encoding := map[bool]string{false: "utf-32-le", true: "utf-32-be"}[bigEndian]
		if c.Module == "utf_32" {
			encoding = "utf-32"
		}
		return encodeUTF(textutil.Runes(s), errors, encoding, out, func(b []byte, r rune) []byte {
			if bigEndian {
				return append(b, byte(r>>24), byte(r>>16), byte(r>>8), byte(r))
			}
			return append(b, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		})
	}
	return encodeSingleByte(textutil.Runes(s), errors, "charmap", "character maps to <undefined>", func(r rune) (byte, bool) {
		b, ok := c.table.encode[r]
		return b, ok
	})
}

func isSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// encodeUTF encodes text with a UTF codec whose put appends the encoding of
// one code point. Lone surrogates are the characters such a codec cannot
// encode: a run of them is one error ("surrogates not allowed") for the
// handler, except that surrogatepass encodes them as if they were not
// surrogates. A replacement holding a surrogate fails as strict.
func encodeUTF(text []rune, errors, encoding string, out []byte, put func([]byte, rune) []byte) ([]byte, error) {
	const reason = "surrogates not allowed"
	for i := 0; i < len(text); {
		if !isSurrogate(text[i]) {
			out = put(out, text[i])
			i++
			continue
		}
		end := i + 1
		for end < len(text) && isSurrogate(text[end]) {
			end++
		}
		if errors == "surrogatepass" {
			for _, r := range text[i:end] {
				out = put(out, r)
			}
			i = end
			continue
		}
		rep, raw, err := encodeReplacement(errors, encoding, text, i, end, reason)
		if err != nil {
			return nil, err
		}
		if raw {
			out = append(out, rep...)
		} else {
			for _, r := range textutil.Runes(rep) {
				if isSurrogate(r) {
					return nil, newEncodeError(encoding, text, i, end, reason)
				}
				out = put(out, r)
			}
		}
		i = end
	}
	return out, nil
}

// encodeSingleByte encodes text with a one-byte-per-character mapping. As in
// CPython, an error covers the whole run of consecutive unencodable
// characters, and the handler's replacement is itself encoded with the
// mapping (failing as strict if it cannot be), unless it is bytes.
func encodeSingleByte(text []rune, errors, encoding, reason string, mapping func(rune) (byte, bool)) ([]byte, error) {
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); {
		if b, ok := mapping(text[i]); ok {
			out = append(out, b)
			i++
			continue
		}
		end := i + 1
		for end < len(text) {
			if _, ok := mapping(text[end]); ok {
				break
			}
			end++
		}
		rep, raw, err := encodeReplacement(errors, encoding, text, i, end, reason)
		if err != nil {
			return nil, err
		}
		if raw {
			out = append(out, rep...)
			i = end
			continue
		}
		for _, r := range textutil.Runes(rep) {
			b, ok := mapping(r)
			if !ok {
				return nil, newEncodeError(encoding, text, i, end, reason)
			}
			out = append(out, b)
		}
		i = end
	}
	return out, nil
}
