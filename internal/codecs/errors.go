// Ported from CPython 3.12 (Objects/exceptions.c, Python/codecs.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package codecs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/pyexc"
	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// Error is a Python exception raised by a codec: UnicodeDecodeError,
// UnicodeEncodeError, UnicodeError, LookupError or ValueError.
type Error struct {
	pyexc.Exception

	// The attributes of UnicodeDecodeError and UnicodeEncodeError, whose args
	// are (Encoding, object, Start, End, Reason). The object is Object (bytes)
	// for a decode error and Text (str) for an encode error; Start and End
	// count bytes or code points respectively.
	Encoding   string
	Object     []byte
	Text       string
	Start, End int
	Reason     string
}

func lookupError(msg string) *Error {
	return &Error{Exception: pyexc.Exception{Class: "LookupError", Msg: msg}}
}

// newDecodeError builds UnicodeDecodeError(encoding, object, start, end, reason).
func newDecodeError(encoding string, object []byte, start, end int, reason string) *Error {
	var msg string
	if start < len(object) && end == start+1 {
		msg = fmt.Sprintf("'%s' codec can't decode byte 0x%02x in position %d: %s", encoding, object[start], start, reason)
	} else {
		msg = fmt.Sprintf("'%s' codec can't decode bytes in position %d-%d: %s", encoding, start, end-1, reason)
	}
	obj := append([]byte(nil), object...)
	return &Error{Exception: pyexc.Exception{Class: "UnicodeDecodeError", Msg: msg}, Encoding: encoding, Object: obj, Start: start, End: end, Reason: reason}
}

// newEncodeError builds UnicodeEncodeError(encoding, text, start, end, reason)
// for text given as code points.
func newEncodeError(encoding string, text []rune, start, end int, reason string) *Error {
	var msg string
	if start < len(text) && end == start+1 {
		ch := textutil.AppendBackslashEscape(nil, text[start])
		msg = fmt.Sprintf("'%s' codec can't encode character '%s' in position %d: %s", encoding, ch, start, reason)
	} else {
		msg = fmt.Sprintf("'%s' codec can't encode characters in position %d-%d: %s", encoding, start, end-1, reason)
	}
	return &Error{Exception: pyexc.Exception{Class: "UnicodeEncodeError", Msg: msg}, Encoding: encoding, Text: textutil.FromRunes(text), Start: start, End: end, Reason: reason}
}

func unknownHandler(errors string) *Error {
	return lookupError("unknown error handler name '" + errors + "'")
}

// decodeFailure applies the error handler errors to the undecodable bytes
// object[start:end], writing the replacement to out. It returns the position
// where decoding resumes (end, unless surrogatepass decoded a surrogate of
// another length) or the exception to raise.
func decodeFailure(out *strings.Builder, errors, encoding string, object []byte, start, end int, reason string) (int, error) {
	switch errors {
	case "strict":
		return 0, newDecodeError(encoding, object, start, end, reason)
	case "ignore":
	case "replace":
		out.WriteRune(0xFFFD)
	case "backslashreplace":
		for _, b := range object[start:end] {
			out.Write(textutil.AppendBackslashEscape(nil, rune(b)))
		}
	case "surrogateescape":
		// Each byte 0x80-0xff becomes the lone surrogate U+DC80-U+DCFF; an
		// ASCII byte cannot be escaped.
		for _, b := range object[start:end] {
			if b < 0x80 {
				return 0, newDecodeError(encoding, object, start, end, reason)
			}
		}
		for _, b := range object[start:end] {
			out.Write(textutil.AppendRune(nil, 0xDC00|rune(b)))
		}
	case "surrogatepass":
		r, n, ok := encodedSurrogate(encoding, object[start:])
		if !ok {
			return 0, newDecodeError(encoding, object, start, end, reason)
		}
		out.Write(textutil.AppendRune(nil, r))
		return start + n, nil
	default:
		return 0, unknownHandler(errors)
	}
	return end, nil
}

// encodedSurrogate reads the lone surrogate that b starts with in the UTF
// codec named encoding, for the surrogatepass handler: its code point and
// length in bytes. ok is false for another codec or another character.
func encodedSurrogate(encoding string, b []byte) (r rune, n int, ok bool) {
	switch encoding {
	case "utf-8":
		if len(b) >= 3 && b[0] == 0xED && b[1]&0xE0 == 0xA0 && b[2]&0xC0 == 0x80 {
			r, n = 0xD000|rune(b[1]&0x3F)<<6|rune(b[2]&0x3F), 3
		}
	case "utf-16-le", "utf-16-be":
		if len(b) >= 2 {
			r, n = rune(b[1])<<8|rune(b[0]), 2
			if encoding == "utf-16-be" {
				r = rune(b[0])<<8 | rune(b[1])
			}
		}
	case "utf-32-le", "utf-32-be":
		if len(b) >= 4 {
			r, n = rune(b[3])<<24|rune(b[2])<<16|rune(b[1])<<8|rune(b[0]), 4
			if encoding == "utf-32-be" {
				r = rune(b[0])<<24 | rune(b[1])<<16 | rune(b[2])<<8 | rune(b[3])
			}
		}
	}
	return r, n, n > 0 && r >= 0xD800 && r <= 0xDFFF
}

// encodeReplacement returns what the error handler errors substitutes for the
// unencodable characters text[start:end], or the exception to raise. The
// replacement is text for the codec to encode, or bytes to write as they are
// when raw is set (surrogateescape).
func encodeReplacement(errors, encoding string, text []rune, start, end int, reason string) (rep string, raw bool, err error) {
	var sb strings.Builder
	switch errors {
	case "strict", "surrogatepass":
		// surrogatepass passes surrogates through the UTF codecs, which
		// handle it themselves; for the other codecs it fails as strict.
		return "", false, newEncodeError(encoding, text, start, end, reason)
	case "surrogateescape":
		// The lone surrogates U+DC80-U+DCFF become the bytes 0x80-0xff.
		for _, c := range text[start:end] {
			if c < 0xDC80 || c > 0xDCFF {
				return "", false, newEncodeError(encoding, text, start, end, reason)
			}
			sb.WriteByte(byte(c - 0xDC00))
		}
		return sb.String(), true, nil
	case "ignore":
	case "replace":
		sb.WriteString(strings.Repeat("?", end-start))
	case "xmlcharrefreplace":
		for _, c := range text[start:end] {
			sb.WriteString("&#" + strconv.Itoa(int(c)) + ";")
		}
	case "backslashreplace":
		var b []byte
		for _, c := range text[start:end] {
			b = textutil.AppendBackslashEscape(b, c)
		}
		sb.Write(b)
	case "namereplace":
		var b []byte
		for _, c := range text[start:end] {
			if name, ok := textutil.Name(c); ok {
				b = append(append(append(b, `\N{`...), name...), '}')
			} else {
				b = textutil.AppendBackslashEscape(b, c)
			}
		}
		sb.Write(b)
	default:
		return "", false, unknownHandler(errors)
	}
	return sb.String(), false, nil
}
