// Ported from CPython 3.12 (Objects/unicodeobject.c, Objects/unicodectype.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package textutil

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// runeRange is an inclusive range of code points.
type runeRange struct{ lo, hi rune }

func inRanges(table []runeRange, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	return i < len(table) && table[i].lo <= r
}

// IsDecimal is str.isdecimal() for one character; it is also what \d matches
// in a Python str pattern and what int() accepts as a digit.
func IsDecimal(r rune) bool { return inRanges(decimalRanges, r) }

// DecimalValue is unicodedata.decimal(r) for a character IsDecimal accepts.
func DecimalValue(r rune) int {
	i := sort.Search(len(decimalZeros), func(i int) bool { return decimalZeros[i] > r })
	return int(r - decimalZeros[i-1])
}

// IsDigit is str.isdigit() for one character.
func IsDigit(r rune) bool { return inRanges(digitRanges, r) }

// IsAlnum is str.isalnum() for one character.
func IsAlnum(r rune) bool { return inRanges(alnumRanges, r) }

// IsSpace is str.isspace() for one character; it is also what \s matches in a
// Python str pattern and what str.strip() removes by default.
func IsSpace(r rune) bool { return inRanges(spaceRanges, r) }

// IsPrintable is str.isprintable() for one character.
func IsPrintable(r rune) bool { return inRanges(printableRanges, r) }

// IsNumeric is str.isnumeric() for one character: any character with a
// numeric value, CJK numerals such as '一' included.
func IsNumeric(r rune) bool { return inRanges(numericRanges, r) }

// IsAlpha is str.isalpha() for one character (Lu, Ll, Lt, Lm, Lo).
func IsAlpha(r rune) bool { return inRanges(alphaRanges, r) }

// IsUpper is Py_UNICODE_ISUPPER: the Uppercase property.
func IsUpper(r rune) bool { return inRanges(upperRanges, r) }

// IsLower is Py_UNICODE_ISLOWER: the Lowercase property.
func IsLower(r rune) bool { return inRanges(lowerRanges, r) }

// IsTitle is Py_UNICODE_ISTITLE: general category Lt, such as 'ǅ'.
func IsTitle(r rune) bool { return inRanges(titleRanges, r) }

// IsCased is the Cased property, which drives str.title() and the final
// sigma rule.
func IsCased(r rune) bool { return inRanges(casedRanges, r) }

// IsCaseIgnorable is the Case_Ignorable property used by the final sigma
// rule.
func IsCaseIgnorable(r rune) bool { return inRanges(caseIgnorableRanges, r) }

// IsXIDStart is XID_Start; str.isidentifier() also accepts '_' first.
func IsXIDStart(r rune) bool { return inRanges(xidStartRanges, r) }

// IsXIDContinue is XID_Continue, for every character of an identifier after
// the first.
func IsXIDContinue(r rune) bool { return inRanges(xidContinueRanges, r) }

// IsASCII is str.isascii(); true for "".
func IsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// IsIdentifier is str.isidentifier(): XID_Start or '_' first, XID_Continue
// after, with no normalisation and no keyword check.
func IsIdentifier(s string) bool {
	r, n := DecodeRune(s)
	if n == 0 || r != '_' && !IsXIDStart(r) {
		return false
	}
	for _, r := range Runes(s[n:]) {
		if !IsXIDContinue(r) {
			return false
		}
	}
	return true
}

// caseRun maps the code points lo, lo+stride, ..., hi to themselves plus
// delta; the runs of a mapping are sorted and never overlap.
type caseRun struct{ lo, hi, stride, delta rune }

// appendMapped appends the full case mapping of r given by one mapping's
// runs and multi-character exceptions; r maps to itself when neither has it.
func appendMapped(b []byte, r rune, runs []caseRun, special map[rune]string) []byte {
	if m, ok := special[r]; ok {
		return append(b, m...)
	}
	i := sort.Search(len(runs), func(i int) bool { return runs[i].hi >= r })
	if i < len(runs) && runs[i].lo <= r && (r-runs[i].lo)%runs[i].stride == 0 {
		r += runs[i].delta
	}
	return AppendRune(b, r)
}

// AppendUpper appends str.upper() of r: the full mapping, so 'ß' is "SS".
func AppendUpper(b []byte, r rune) []byte { return appendMapped(b, r, upperRuns, upperSpecial) }

// AppendLower appends str.lower() of r alone; AppendLowerAt also applies the
// final sigma rule.
func AppendLower(b []byte, r rune) []byte { return appendMapped(b, r, lowerRuns, lowerSpecial) }

// AppendTitle appends the full titlecase mapping of r, so 'ǆ' is 'ǅ'.
func AppendTitle(b []byte, r rune) []byte { return appendMapped(b, r, titleRuns, titleSpecial) }

// AppendFold appends str.casefold() of r.
func AppendFold(b []byte, r rune) []byte { return appendMapped(b, r, foldRuns, foldSpecial) }

// AppendLowerAt appends the lower mapping of rs[i], lowering U+03A3 to the
// final sigma U+03C2 in the Final_Sigma context as CPython's lower_ucs4 does.
func AppendLowerAt(b []byte, rs []rune, i int) []byte {
	switch {
	case rs[i] != 'Σ':
		return AppendLower(b, rs[i])
	case isFinalSigma(rs, i):
		return AppendRune(b, 'ς')
	default:
		return AppendRune(b, 'σ')
	}
}

// isFinalSigma reports whether rs[i] is in the Final_Sigma context: cased
// case-ignorable* Σ, not followed by case-ignorable* cased.
func isFinalSigma(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && IsCaseIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !IsCased(rs[j]) {
		return false
	}
	j = i + 1
	for j < len(rs) && IsCaseIgnorable(rs[j]) {
		j++
	}
	return j == len(rs) || !IsCased(rs[j])
}

// Lower is str.lower(): the full lowercase mapping, so 'İ' becomes "i̇", with
// Σ lowered to ς at the end of a word.
func Lower(s string) string {
	if IsASCII(s) {
		return strings.ToLower(s)
	}
	rs := Runes(s)
	b := make([]byte, 0, len(s))
	for i := range rs {
		b = AppendLowerAt(b, rs, i)
	}
	return string(b)
}

// Python strings may hold lone surrogates (chr(0xD800) is a valid str). Such
// a code point is kept in a Go string as its generalized UTF-8 encoding, the
// three bytes ED A0..BF 80..BF, which the functions below read and write.

// DecodeRune is utf8.DecodeRuneInString that also decodes a lone surrogate.
func DecodeRune(s string) (rune, int) {
	if isSurrogateAt(s, 0) {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// isSurrogateAt reports whether s[i:] starts with an encoded lone surrogate.
func isSurrogateAt(s string, i int) bool {
	return i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF
}

// IndexSurrogate returns the byte offset of the first lone surrogate in s,
// or -1.
func IndexSurrogate(s string) int {
	for i := 0; i < len(s); i++ {
		if isSurrogateAt(s, i) {
			return i
		}
	}
	return -1
}

// AppendRune is utf8.AppendRune that also encodes a lone surrogate.
func AppendRune(b []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(b, 0xED, byte(0x80|(r>>6)&0x3F), byte(0x80|r&0x3F))
	}
	return utf8.AppendRune(b, r)
}

// Prefix is s[:n] for a str: s cut to its first n code points, as the
// precision of "%.200R" cuts a repr.
func Prefix(s string, n int) string {
	for i := 0; i < len(s); n-- {
		if n == 0 {
			return s[:i]
		}
		_, size := DecodeRune(s[i:])
		i += size
	}
	return s
}

// RuneCount is len(s) for a str: its number of code points, lone surrogates
// included.
func RuneCount(s string) int {
	n := 0
	for i := 0; i < len(s); n++ {
		_, size := DecodeRune(s[i:])
		i += size
	}
	return n
}

// Runes is list(s): the code points of s, lone surrogates included.
func Runes(s string) []rune {
	out := make([]rune, 0, len(s))
	for len(s) > 0 {
		r, n := DecodeRune(s)
		out = append(out, r)
		s = s[n:]
	}
	return out
}

// FromRunes joins code points into a string, the inverse of Runes.
func FromRunes(rs []rune) string {
	b := make([]byte, 0, len(rs))
	for _, r := range rs {
		b = AppendRune(b, r)
	}
	return string(b)
}

// Repr is repr(s) for a str.
func Repr(s string) string {
	quote := reprQuote(s)
	b := []byte{quote}
	for len(s) > 0 {
		r, n := DecodeRune(s)
		s = s[n:]
		switch {
		case isReprSpecial(r, quote):
			b = appendReprSpecial(b, r)
		case r >= 0x20 && r < 0x7F:
			b = append(b, byte(r))
		case r > 0x7F && IsPrintable(r):
			b = AppendRune(b, r)
		default:
			b = AppendBackslashEscape(b, r)
		}
	}
	return string(append(b, quote))
}

// BytesRepr is repr(b) for a bytes object holding the bytes of s.
func BytesRepr(s string) string {
	quote := reprQuote(s)
	b := []byte{'b', quote}
	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case isReprSpecial(c, quote):
			b = appendReprSpecial(b, c)
		case c >= 0x20 && c < 0x7F:
			b = append(b, byte(c))
		default:
			b = AppendBackslashEscape(b, c)
		}
	}
	return string(append(b, quote))
}

// reprQuote is the quote repr() puts around s: a double quote when s holds a
// single quote and no double quote, else a single quote.
func reprQuote(s string) byte {
	if strings.IndexByte(s, '\'') >= 0 && strings.IndexByte(s, '"') < 0 {
		return '"'
	}
	return '\''
}

// isReprSpecial reports whether repr() writes r as a backslash and a letter
// (or the character itself) inside the given quotes.
func isReprSpecial(r rune, quote byte) bool {
	return r == rune(quote) || r == '\\' || r == '\t' || r == '\n' || r == '\r'
}

func appendReprSpecial(b []byte, r rune) []byte {
	switch r {
	case '\t':
		return append(b, '\\', 't')
	case '\n':
		return append(b, '\\', 'n')
	case '\r':
		return append(b, '\\', 'r')
	}
	return append(b, '\\', byte(r))
}

// AppendBackslashEscape appends the backslash escape of r that repr(),
// ascii() and the backslashreplace error handler write: \xhh up to U+00FF,
// \uhhhh up to U+FFFF and \Uhhhhhhhh above, in lowercase hex.
func AppendBackslashEscape(b []byte, r rune) []byte {
	letter, width := byte('x'), 2
	switch {
	case r > 0xFFFF:
		letter, width = 'U', 8
	case r > 0xFF:
		letter, width = 'u', 4
	}
	h := strconv.FormatInt(int64(r), 16)
	b = append(b, '\\', letter)
	for i := len(h); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, h...)
}

// FloatRepr is repr() of a float: the shortest digits that round-trip, in
// fixed notation for decimal exponents from -4 to 16 and in exponent notation
// otherwise.
func FloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64)
	sign := ""
	if e[0] == '-' {
		sign, e = "-", e[1:]
	}
	// e is d[.ddd]e±XX: the mantissa digits and a signed decimal exponent.
	mant, expText, _ := strings.Cut(e, "e")
	exp := 0
	for _, c := range expText[1:] {
		exp = exp*10 + int(c-'0')
	}
	if expText[0] == '-' {
		exp = -exp
	}
	digits := strings.Replace(mant, ".", "", 1)
	decpt := exp + 1
	if decpt > -4 && decpt <= 16 {
		switch {
		case decpt <= 0:
			return sign + "0." + strings.Repeat("0", -decpt) + digits
		case decpt >= len(digits):
			return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
		default:
			return sign + digits[:decpt] + "." + digits[decpt:]
		}
	}
	out := digits[:1]
	if len(digits) > 1 {
		out += "." + digits[1:]
	}
	expSign := "+"
	if exp < 0 {
		expSign, exp = "-", -exp
	}
	expDigits := strconv.Itoa(exp)
	if len(expDigits) < 2 {
		expDigits = "0" + expDigits
	}
	return sign + out + "e" + expSign + expDigits
}

// IsLinebreak reports whether str.splitlines() breaks at r: \n \v \f \r
// \x1c \x1d \x1e \x85 U+2028 U+2029.
func IsLinebreak(r rune) bool {
	switch r {
	case '\n', '\v', '\f', '\r', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// SplitLines is str.splitlines(keepends): it breaks at every character
// IsLinebreak accepts, reading "\r\n" as one break, and yields no empty last
// line for a trailing break.
func SplitLines(s string, keepends bool) []string {
	out := []string{}
	for i, j := 0, 0; i < len(s); j = i {
		var r rune
		n := 0
		for i < len(s) {
			r, n = utf8.DecodeRuneInString(s[i:])
			if IsLinebreak(r) {
				break
			}
			i += n
		}
		eol := i
		if i < len(s) {
			if r == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i += n
			}
			if keepends {
				eol = i
			}
		}
		out = append(out, s[j:eol])
	}
	return out
}

// skipSpace returns the offset of the first character at or after i whose
// str.isspace() equals want, or len(s).
func skipSpace(s string, i int, want bool) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if IsSpace(r) == want {
			break
		}
		i += n
	}
	return i
}

// skipSpaceBack is skipSpace from the right: it returns the end of the last
// character before end whose str.isspace() equals want, or 0.
func skipSpaceBack(s string, end int, want bool) int {
	for end > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:end])
		if IsSpace(r) == want {
			break
		}
		end -= n
	}
	return end
}

// SplitWhitespace is str.split(None, maxsplit) (CPython's split_whitespace):
// once maxsplit splits are made, the rest, with leading whitespace skipped,
// is the last item. A negative maxsplit splits everywhere.
func SplitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	i := 0
	for ; maxsplit != 0; maxsplit-- {
		i = skipSpace(s, i, false)
		if i == len(s) {
			break
		}
		j := i
		i = skipSpace(s, i, true)
		out = append(out, s[j:i])
	}
	if i < len(s) {
		if i = skipSpace(s, i, false); i < len(s) {
			out = append(out, s[i:])
		}
	}
	return out
}

// RSplitWhitespace is str.rsplit(None, maxsplit) (CPython's
// rsplit_whitespace).
func RSplitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	i := len(s)
	for ; maxsplit != 0; maxsplit-- {
		i = skipSpaceBack(s, i, false)
		if i == 0 {
			break
		}
		j := i
		i = skipSpaceBack(s, i, true)
		out = append(out, s[i:j])
	}
	if i > 0 {
		if i = skipSpaceBack(s, i, false); i > 0 {
			out = append(out, s[:i])
		}
	}
	slices.Reverse(out)
	return out
}
