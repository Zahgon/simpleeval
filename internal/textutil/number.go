// Ported from CPython 3.12 (Objects/longobject.c, Objects/floatobject.c, Objects/unicodeobject.c,
// Python/pystrtod.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package textutil

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/pyexc"
)

// Decimal is Py_UNICODE_TODECIMAL: the value of a decimal digit of any
// script, or -1.
func Decimal(r rune) int {
	if !IsDecimal(r) {
		return -1
	}
	return DecimalValue(r)
}

// IsASCIIDigit reports whether r is one of the ASCII digits 0-9.
func IsASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// IsCSpace is C's isspace in the "C" locale (Py_ISSPACE): space, \t, \n,
// \v, \f and \r.
func IsCSpace(r rune) bool { return r == ' ' || r >= '\t' && r <= '\r' }

// numberText is CPython's _PyUnicode_TransformDecimalAndSpaceToASCII, the
// text int() and float() parse: characters below U+007F are kept, other
// whitespace becomes a space, other decimal digits become ASCII digits, and
// the first other character becomes '?', where the result ends.
func numberText(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x7f:
			out = append(out, byte(r))
		case IsSpace(r):
			out = append(out, ' ')
		default:
			d := Decimal(r)
			if d < 0 {
				return append(out, '?')
			}
			out = append(out, byte('0'+d))
		}
	}
	return out
}

// digitValue is CPython's _PyLong_DigitValue: 0-35 for 0-9, a-z and A-Z,
// else 37.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 37
}

// ParseInt is int(s, base) for a str s and a base int() accepts (0 or 2 to
// 36): surrounding whitespace, a sign, underscores between digits, an
// optional base prefix (required to pick the base when base is 0), and
// decimal digits of any script. ok is false for a malformed string, for which
// int() raises "invalid literal for int() with base ..."; the error is the
// ValueError of a decimal string over the digit limit.
func ParseInt(s string, base int) (v *big.Int, ok bool, err error) {
	buf := numberText(s)
	// The C code reads a NUL-terminated buffer.
	full := true
	if i := strings.IndexByte(string(buf), 0); i >= 0 {
		buf, full = buf[:i], false
	}
	v, err = parseIntASCII(buf, base)
	if err != nil {
		return nil, false, err
	}
	return v, v != nil && full, nil
}

// parseIntASCII is CPython's PyLong_FromString on an ASCII buffer. It returns
// nil, nil for a malformed string and an error only for the digit limit.
func parseIntASCII(b []byte, base int) (*big.Int, error) {
	at := func(i int) byte {
		if i < len(b) {
			return b[i]
		}
		return 0
	}
	p := 0
	for p < len(b) && IsCSpace(rune(b[p])) {
		p++
	}
	neg := false
	switch at(p) {
	case '+':
		p++
	case '-':
		p++
		neg = true
	}
	errorIfNonzero := false
	if base == 0 {
		switch {
		case at(p) != '0':
			base = 10
		case at(p+1) == 'x' || at(p+1) == 'X':
			base = 16
		case at(p+1) == 'o' || at(p+1) == 'O':
			base = 8
		case at(p+1) == 'b' || at(p+1) == 'B':
			base = 2
		default:
			// An old-style octal literal is invalid unless it is zero.
			errorIfNonzero = true
			base = 10
		}
	}
	if at(p) == '0' {
		c := at(p + 1)
		if base == 16 && (c == 'x' || c == 'X') || base == 8 && (c == 'o' || c == 'O') ||
			base == 2 && (c == 'b' || c == 'B') {
			p += 2
			if at(p) == '_' {
				p++
			}
		}
	}
	if at(p) == '_' {
		return nil, nil
	}
	start := p
	var prev byte
	digits := 0
	for digitValue(at(p)) < base || at(p) == '_' {
		if at(p) == '_' {
			if prev == '_' {
				return nil, nil
			}
		} else {
			digits++
		}
		prev = at(p)
		p++
	}
	if prev == '_' || p == start {
		return nil, nil
	}
	end := p
	for p < len(b) && IsCSpace(rune(b[p])) {
		p++
	}
	if p != len(b) {
		return nil, nil
	}
	if base&(base-1) != 0 && digits > pyexc.MaxStrDigits {
		return nil, pyexc.DigitLimit(digits)
	}
	text := strings.ReplaceAll(string(b[start:end]), "_", "")
	v, ok := new(big.Int).SetString(text, base)
	if !ok {
		return nil, nil
	}
	if errorIfNonzero && v.Sign() != 0 {
		return nil, nil
	}
	if neg {
		v.Neg(v)
	}
	return v, nil
}

// ParseFloat is float(s) for a str s: surrounding whitespace, a sign, inf,
// infinity and nan in any case, underscores between digits, and decimal
// digits of any script, correctly rounded. ok is false where float() raises
// "could not convert string to float".
func ParseFloat(s string) (float64, bool) {
	b := numberText(s)
	if strings.IndexByte(string(b), 0) >= 0 {
		return 0, false
	}
	// _Py_string_to_number_with_underscores: underscores are only allowed
	// between digits.
	if strings.IndexByte(string(b), '_') >= 0 {
		isDigit := func(c byte) bool { return IsASCIIDigit(rune(c)) }
		var prev byte
		clean := make([]byte, 0, len(b))
		for _, c := range b {
			if c == '_' {
				if !isDigit(prev) {
					return 0, false
				}
			} else {
				clean = append(clean, c)
				if prev == '_' && !isDigit(c) {
					return 0, false
				}
			}
			prev = c
		}
		if prev == '_' {
			return 0, false
		}
		b = clean
	}
	i, j := 0, len(b)
	for i < j && IsCSpace(rune(b[i])) {
		i++
	}
	for j > i && IsCSpace(rune(b[j-1])) {
		j--
	}
	return Strtod(string(b[i:j]))
}

// Strtod is PyOS_string_to_double on the whole of s: an optionally signed
// decimal number, or inf, infinity or nan in any case.
func Strtod(s string) (float64, bool) {
	body := s
	neg := false
	if body != "" && (body[0] == '+' || body[0] == '-') {
		neg = body[0] == '-'
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		if neg {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case "nan":
		if neg {
			return math.Copysign(math.NaN(), -1), true
		}
		return math.NaN(), true
	}
	if !isDecimalFloat(body) {
		return 0, false
	}
	// The syntax is checked, so the only possible error is ErrRange, and the
	// value it comes with (±Inf or ±0) is the one Python gives.
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil || errors.Is(err, strconv.ErrRange)
}

// isDecimalFloat reports whether s is digits with an optional point (at
// least one digit in all) and an optional exponent.
func isDecimalFloat(s string) bool {
	i := 0
	digits := func() int {
		n := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			n++
		}
		return n
	}
	n := digits()
	if i < len(s) && s[i] == '.' {
		i++
		n += digits()
	}
	if n == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(s)
}

// FloatToInt is int(f) for a float: truncated toward zero, with the
// OverflowError and ValueError of the infinities and NaN.
func FloatToInt(f float64) (*big.Int, error) {
	switch {
	case math.IsInf(f, 0):
		return nil, pyexc.New("OverflowError", "cannot convert float infinity to integer")
	case math.IsNaN(f):
		return nil, pyexc.New("ValueError", "cannot convert float NaN to integer")
	}
	i, _ := big.NewFloat(math.Trunc(f)).Int(nil)
	return i, nil
}
