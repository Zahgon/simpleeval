// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// String and bytes literal escapes as the Python 3.12 parser decodes them.

package py

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/danthedeckie/simpleeval/internal/codecs"
	"github.com/danthedeckie/simpleeval/internal/pyexc"
	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// decodeStrBody decodes the escapes of the body of a str or bytes literal
// (the unicode_escape and escape decoders). It also returns the first
// invalid escape sequence, for the SyntaxWarning the parser issues: "\\d"
// for an unknown escape, or the three digits of an octal escape above 0o377.
// A backslash before a non-ASCII character or at the end is kept as written.
func decodeStrBody(body string, isBytes bool) (string, string, error) {
	var b strings.Builder
	invalid := ""
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		e := body[i]
		// escape reports the error of the escape starting at the backslash,
		// which ends before body[end].
		escape := func(msg string, end int) error {
			return escapeErr(body, isBytes, msg, i-1, end)
		}
		switch e {
		case '\n':
		case '\\', '\'', '"':
			b.WriteByte(e)
		case 'a':
			b.WriteByte(7)
		case 'b':
			b.WriteByte(8)
		case 'f':
			b.WriteByte(12)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte(11)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := 0
			j := i
			for j < len(body) && j < i+3 && body[j] >= '0' && body[j] <= '7' {
				v = v*8 + int(body[j]-'0')
				j++
			}
			if v > 0o377 && invalid == "" {
				invalid = body[i:j]
			}
			i = j - 1
			if isBytes {
				b.WriteByte(byte(v))
			} else {
				b.Write(textutil.AppendRune(nil, rune(v)))
			}
		case 'x':
			v, n := hexDigits(body, i+1, 2)
			if n < 2 {
				return "", "", escape("truncated \\xXX escape", i+1+n)
			}
			i += 2
			if isBytes {
				b.WriteByte(byte(v))
			} else {
				b.Write(textutil.AppendRune(nil, rune(v)))
			}
		case 'u', 'U', 'N':
			if isBytes {
				// Not escapes in bytes: kept as written.
				if invalid == "" {
					invalid = body[i : i+1]
				}
				b.WriteByte('\\')
				b.WriteByte(e)
				continue
			}
			switch e {
			case 'u', 'U':
				n := 4
				if e == 'U' {
					n = 8
				}
				v, valid := hexDigits(body, i+1, n)
				if valid < n {
					return "", "", escape("truncated \\"+string(e)+strings.Repeat("X", n)+" escape", i+1+valid)
				}
				if v > 0x10ffff {
					return "", "", escape("illegal Unicode character", i+1+n)
				}
				i += n
				b.Write(textutil.AppendRune(nil, rune(v)))
			case 'N':
				if i+1 >= len(body) || body[i+1] != '{' {
					return "", "", escape("malformed \\N character escape", i+1)
				}
				end := strings.IndexByte(body[i+1:], '}')
				switch {
				case end < 0:
					return "", "", escape("malformed \\N character escape", len(body))
				case end == 1:
					return "", "", escape("malformed \\N character escape", i+2)
				}
				r, ok := textutil.LookupName(body[i+2 : i+1+end])
				if !ok {
					return "", "", escape("unknown Unicode character name", i+2+end)
				}
				b.Write(textutil.AppendRune(nil, r))
				i += 1 + end
			}
		default:
			// An unknown escape keeps its backslash.
			if e < 0x80 && invalid == "" {
				invalid = body[i : i+1]
			}
			b.WriteByte('\\')
			b.WriteByte(e)
		}
	}
	return b.String(), invalid, nil
}

// escapeErr is the error of the escape body[start:end] (start at its
// backslash) of a literal. For a bytes literal it is the ValueError of the
// escape decoder. For a str literal it is the UnicodeDecodeError of the
// unicode_escape decoder, on the literal as CPython hands it to the decoder:
// a non-ASCII character there is a \UXXXXXXXX escape, and a backslash before
// one or at the end is \u005c.
func escapeErr(body string, isBytes bool, msg string, start, end int) error {
	if isBytes {
		return pyexc.New("ValueError", fmt.Sprintf("invalid \\x escape at position %d", start))
	}
	var buf []byte
	from, to := -1, -1
	for j := 0; j <= len(body); {
		if j == start {
			from = len(buf)
		}
		if j == end {
			to = len(buf)
		}
		if j == len(body) {
			break
		}
		r, size := utf8.DecodeRuneInString(body[j:])
		switch {
		case r >= 0x80:
			buf = fmt.Appendf(buf, "\\U%08x", r)
		case r == '\\' && (j+1 == len(body) || body[j+1] >= 0x80):
			buf = append(buf, `\u005c`...)
		default:
			buf = append(buf, body[j])
		}
		j += size
	}
	reason := "'unicodeescape' codec can't decode bytes in position " + fmt.Sprintf("%d-%d: %s", from, to-1, msg)
	if to == from+1 {
		reason = fmt.Sprintf("'unicodeescape' codec can't decode byte 0x%02x in position %d: %s", buf[from], from, msg)
	}
	return &codecs.Error{Exception: pyexc.Exception{Class: "UnicodeDecodeError", Msg: reason},
		Encoding: "unicodeescape", Object: buf, Start: from, End: to, Reason: msg}
}

// hexDigits reads up to n hex digits of s from at: their value and how many
// there were before the first other character or the end.
func hexDigits(s string, at, n int) (v, valid int) {
	for ; valid < n && at+valid < len(s); valid++ {
		c := s[at+valid]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return v, valid
		}
		v = v*16 + d
	}
	return v, valid
}
