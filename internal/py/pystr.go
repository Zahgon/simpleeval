// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// CPython 3.12's str methods. Every function works on a Go string holding UTF-8 with code-point
// semantics: lengths, widths, indices and positions count code points, as Python does. A string is
// assumed to be valid UTF-8; an invalid byte reads as U+FFFD. Arguments arrive already converted from
// Values (that is the engine's job); each doc comment says what Python accepts for them, so the engine
// can raise Python's errors first. Character classes and case mappings come from CPython 3.12's
// database (Unicode 15.0.0) through package textutil.

package py

import (
	"slices"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// ---------------------------------------------------------------- per rune

// ---------------------------------------------------------------- case mapping

// pyMapRunes builds a string by appending each rune of s through f.
func pyMapRunes(s string, f func([]byte, rune) []byte) string {
	b := make([]byte, 0, len(s))
	for _, r := range textutil.Runes(s) {
		b = f(b, r)
	}
	return string(b)
}

// pyUpper is str.upper(): the full uppercase mapping, so 'ß' becomes "SS".
func pyUpper(s string) string {
	if textutil.IsASCII(s) {
		return strings.ToUpper(s)
	}
	return pyMapRunes(s, textutil.AppendUpper)
}

// pyCasefold is str.casefold(): the full case folding, without the final
// sigma rule.
func pyCasefold(s string) string {
	if textutil.IsASCII(s) {
		return strings.ToLower(s)
	}
	return pyMapRunes(s, textutil.AppendFold)
}

// pyTitle is str.title(): a character is title-cased after a character
// that is not cased and lower-cased after one that is, so "they're" becomes
// "They'Re".
func pyTitle(s string) string {
	rs := textutil.Runes(s)
	b := make([]byte, 0, len(s))
	prevCased := false
	for i, r := range rs {
		if prevCased {
			b = textutil.AppendLowerAt(b, rs, i)
		} else {
			b = textutil.AppendTitle(b, r)
		}
		prevCased = textutil.IsCased(r)
	}
	return string(b)
}

// pyCapitalize is str.capitalize() as of Python 3.8: the first character
// title-cased (so 'ǆ' becomes 'ǅ', 'ß' "Ss"), the rest lower-cased.
func pyCapitalize(s string) string {
	rs := textutil.Runes(s)
	b := make([]byte, 0, len(s))
	for i, r := range rs {
		if i == 0 {
			b = textutil.AppendTitle(b, r)
		} else {
			b = textutil.AppendLowerAt(b, rs, i)
		}
	}
	return string(b)
}

// pySwapcase is str.swapcase(): uppercase characters lowered (with the final
// sigma rule), lowercase ones uppered, the rest (titlecase 'ǅ' included)
// left alone.
func pySwapcase(s string) string {
	rs := textutil.Runes(s)
	b := make([]byte, 0, len(s))
	for i, r := range rs {
		switch {
		case textutil.IsUpper(r):
			b = textutil.AppendLowerAt(b, rs, i)
		case textutil.IsLower(r):
			b = textutil.AppendUpper(b, r)
		default:
			b = textutil.AppendRune(b, r)
		}
	}
	return string(b)
}

// ---------------------------------------------------------------- predicates

// pyAll reports whether s is not empty and every character satisfies f.
func pyAll(s string, f func(rune) bool) bool {
	if s == "" {
		return false
	}
	for _, r := range textutil.Runes(s) {
		if !f(r) {
			return false
		}
	}
	return true
}

// pyIsUpper is str.isupper(): no lowercase or titlecase character and at
// least one uppercase one.
func pyIsUpper(s string) bool {
	if r, n := textutil.DecodeRune(s); n == len(s) && n > 0 {
		return textutil.IsUpper(r)
	}
	cased := false
	for _, r := range textutil.Runes(s) {
		if textutil.IsLower(r) || textutil.IsTitle(r) {
			return false
		}
		cased = cased || textutil.IsUpper(r)
	}
	return cased
}

// pyIsLower is str.islower(): no uppercase or titlecase character and at
// least one lowercase one.
func pyIsLower(s string) bool {
	if r, n := textutil.DecodeRune(s); n == len(s) && n > 0 {
		return textutil.IsLower(r)
	}
	cased := false
	for _, r := range textutil.Runes(s) {
		if textutil.IsUpper(r) || textutil.IsTitle(r) {
			return false
		}
		cased = cased || textutil.IsLower(r)
	}
	return cased
}

// pyIsTitle is str.istitle(): uppercase and titlecase characters only after
// uncased ones, lowercase ones only after cased ones, and at least one cased.
func pyIsTitle(s string) bool {
	if r, n := textutil.DecodeRune(s); n == len(s) && n > 0 {
		return textutil.IsTitle(r) || textutil.IsUpper(r)
	}
	cased, prevCased := false, false
	for _, r := range textutil.Runes(s) {
		switch {
		case textutil.IsUpper(r) || textutil.IsTitle(r):
			if prevCased {
				return false
			}
			prevCased, cased = true, true
		case textutil.IsLower(r):
			if !prevCased {
				return false
			}
			prevCased, cased = true, true
		default:
			prevCased = false
		}
	}
	return cased
}

// pyIsAlpha is str.isalpha(); false for "".
func pyIsAlpha(s string) bool { return pyAll(s, textutil.IsAlpha) }

// pyIsAlnum is str.isalnum(); false for "".
func pyIsAlnum(s string) bool { return pyAll(s, textutil.IsAlnum) }

// pyIsDigit is str.isdigit(); false for "".
func pyIsDigit(s string) bool { return pyAll(s, textutil.IsDigit) }

// pyIsDecimal is str.isdecimal(); false for "".
func pyIsDecimal(s string) bool { return pyAll(s, textutil.IsDecimal) }

// pyIsNumeric is str.isnumeric(); false for "".
func pyIsNumeric(s string) bool { return pyAll(s, textutil.IsNumeric) }

// pyIsSpace is str.isspace(); false for "".
func pyIsSpace(s string) bool { return pyAll(s, textutil.IsSpace) }

// pyIsPrintable is str.isprintable(); true for "".
func pyIsPrintable(s string) bool { return s == "" || pyAll(s, textutil.IsPrintable) }

// ---------------------------------------------------------------- whitespace, splitting

// pyStrip is str.strip(chars): chars nil (Python's None) strips whitespace
// as str.isspace() defines it, otherwise every code point in *chars. Python
// takes chars as a str or None.
func pyStrip(s string, chars *string) string {
	if chars == nil {
		return strings.TrimFunc(s, textutil.IsSpace)
	}
	return strings.Trim(s, *chars)
}

// pyLStrip is str.lstrip(chars); see pyStrip.
func pyLStrip(s string, chars *string) string {
	if chars == nil {
		return strings.TrimLeftFunc(s, textutil.IsSpace)
	}
	return strings.TrimLeft(s, *chars)
}

// pyRStrip is str.rstrip(chars); see pyStrip.
func pyRStrip(s string, chars *string) string {
	if chars == nil {
		return strings.TrimRightFunc(s, textutil.IsSpace)
	}
	return strings.TrimRight(s, *chars)
}

// pySplit is str.split(sep, maxsplit). sep nil (None) splits on runs of
// whitespace and drops empty strings; otherwise an empty *sep raises
// ValueError "empty separator". maxsplit < 0 means no limit. Python takes
// sep as a str or None and maxsplit as an int that fits a C ssize_t
// (OverflowError "Python int too large to convert to C ssize_t" otherwise).
func pySplit(s string, sep *string, maxsplit int) ([]string, error) {
	if sep == nil {
		return textutil.SplitWhitespace(s, maxsplit), nil
	}
	if *sep == "" {
		return nil, valueErr("empty separator")
	}
	if maxsplit < 0 || maxsplit >= len(s) {
		return strings.Split(s, *sep), nil
	}
	return strings.SplitN(s, *sep, maxsplit+1), nil
}

// pyRSplit is str.rsplit(sep, maxsplit): pySplit working from the right.
func pyRSplit(s string, sep *string, maxsplit int) ([]string, error) {
	if sep == nil {
		return textutil.RSplitWhitespace(s, maxsplit), nil
	}
	if *sep == "" {
		return nil, valueErr("empty separator")
	}
	var out []string
	for ; maxsplit != 0; maxsplit-- {
		i := strings.LastIndex(s, *sep)
		if i < 0 {
			break
		}
		out = append(out, s[i+len(*sep):])
		s = s[:i]
	}
	out = append(out, s)
	slices.Reverse(out)
	return out, nil
}

// pyExpandtabs is str.expandtabs(tabsize): each tab becomes spaces up to the
// next multiple of tabsize, the column restarting after "\n" and "\r" only;
// tabsize <= 0 removes tabs. Python takes tabsize as an int that fits a C
// int (OverflowError "Python int too large to convert to C int" otherwise).
func pyExpandtabs(s string, tabsize int) string {
	var b strings.Builder
	col := 0
	for _, r := range textutil.Runes(s) {
		switch {
		case r == '\t':
			if tabsize > 0 {
				n := tabsize - col%tabsize
				col += n
				b.WriteString(strings.Repeat(" ", n))
			}
		default:
			b.Write(textutil.AppendRune(nil, r))
			col++
			if r == '\n' || r == '\r' {
				col = 0
			}
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- search

// pyAdjustIndices is CPython's ADJUST_INDICES for a string of n code points:
// a nil bound is the default, a negative one counts from the end, end is
// clamped to [0, n] and start only below at 0. The engine converts a bound
// as a slice index (None, or an int or bool with big ints clamped to the Go
// int range); anything else is TypeError "slice indices must be integers or
// None or have an __index__ method".
func pyAdjustIndices(start, end *int, n int) (int, int) {
	st, en := 0, n
	if end != nil {
		en = *end
		if en > n {
			en = n
		} else if en < 0 {
			en = max(en+n, 0)
		}
	}
	if start != nil {
		st = *start
		if st < 0 {
			st = max(st+n, 0)
		}
	}
	return st, en
}

// pyByteOffset returns the byte offset of code point i of s (len(s) past
// the end).
func pyByteOffset(s string, i int) int {
	off := 0
	for ; i > 0 && off < len(s); i-- {
		_, n := textutil.DecodeRune(s[off:])
		off += n
	}
	return off
}

// pyWindow returns s[st:en] in code points with the byte offset it starts
// at, for 0 <= st <= en <= len(s) in code points.
func pyWindow(s string, st, en int) (string, int) {
	bs := pyByteOffset(s, st)
	be := bs + pyByteOffset(s[bs:], en-st)
	return s[bs:be], bs
}

// pySearch runs find (last false) or rfind (last true) on s[start:end].
func pySearch(s, sub string, start, end *int, last bool) int {
	st, en := pyAdjustIndices(start, end, textutil.RuneCount(s))
	if en-st < textutil.RuneCount(sub) {
		return -1
	}
	w, _ := pyWindow(s, st, en)
	var i int
	if last {
		i = strings.LastIndex(w, sub)
	} else {
		i = strings.Index(w, sub)
	}
	if i < 0 {
		return -1
	}
	return st + textutil.RuneCount(w[:i])
}

// pyFind is str.find(sub, start, end): the lowest index of sub within
// s[start:end], or -1. An empty sub is found at start unless start is past
// the clamped end.
func pyFind(s, sub string, start, end *int) int { return pySearch(s, sub, start, end, false) }

// pyRFind is str.rfind(sub, start, end): the highest index, or -1.
func pyRFind(s, sub string, start, end *int) int { return pySearch(s, sub, start, end, true) }

// pyIndex is str.index(): pyFind raising ValueError "substring not found".
func pyIndex(s, sub string, start, end *int) (int, error) {
	if i := pyFind(s, sub, start, end); i >= 0 {
		return i, nil
	}
	return 0, valueErr("substring not found")
}

// pyRIndex is str.rindex(): pyRFind raising ValueError "substring not found".
func pyRIndex(s, sub string, start, end *int) (int, error) {
	if i := pyRFind(s, sub, start, end); i >= 0 {
		return i, nil
	}
	return 0, valueErr("substring not found")
}

// pyCount is str.count(sub, start, end): non-overlapping occurrences in
// s[start:end]; an empty sub counts end-start+1 positions.
func pyCount(s, sub string, start, end *int) int {
	st, en := pyAdjustIndices(start, end, textutil.RuneCount(s))
	if en-st < textutil.RuneCount(sub) {
		return 0
	}
	w, _ := pyWindow(s, st, en)
	return strings.Count(w, sub)
}

// pyTailMatch is CPython's tailmatch: whether s[start:end] starts (suffix
// false) or ends (suffix true) with sub.
func pyTailMatch(s, sub string, start, end *int, suffix bool) bool {
	st, en := pyAdjustIndices(start, end, textutil.RuneCount(s))
	m := textutil.RuneCount(sub)
	en -= m
	if en < st {
		return false
	}
	if m == 0 {
		return true
	}
	at := st
	if suffix {
		at = en
	}
	return strings.HasPrefix(s[pyByteOffset(s, at):], sub)
}

// pyStartsWith is str.startswith(prefix, start, end) for one prefix; for a
// tuple the engine calls it per item. Python takes prefix as a str or a
// tuple of str (TypeError "startswith first arg must be str or a tuple of
// str, not X" otherwise).
func pyStartsWith(s, prefix string, start, end *int) bool {
	return pyTailMatch(s, prefix, start, end, false)
}

// pyEndsWith is str.endswith(suffix, start, end); see pyStartsWith.
func pyEndsWith(s, suffix string, start, end *int) bool {
	return pyTailMatch(s, suffix, start, end, true)
}

// pyReplace is str.replace(old, new, count): count < 0 replaces every
// occurrence; an empty old inserts new before every character and at the
// end, count times at most. Python takes count as an int that fits a C
// ssize_t.
func pyReplace(s, old, new string, count int) string {
	return strings.Replace(s, old, new, count)
}

// pyPartition is str.partition(sep): (before, sep, after) at the first sep,
// or (s, "", "") when it is absent. An empty sep is ValueError "empty
// separator".
func pyPartition(s, sep string) ([3]string, error) {
	if sep == "" {
		return [3]string{}, valueErr("empty separator")
	}
	if i := strings.Index(s, sep); i >= 0 {
		return [3]string{s[:i], sep, s[i+len(sep):]}, nil
	}
	return [3]string{s, "", ""}, nil
}

// pyRPartition is str.rpartition(sep): split at the last sep, or
// ("", "", s) when it is absent.
func pyRPartition(s, sep string) ([3]string, error) {
	if sep == "" {
		return [3]string{}, valueErr("empty separator")
	}
	if i := strings.LastIndex(s, sep); i >= 0 {
		return [3]string{s[:i], sep, s[i+len(sep):]}, nil
	}
	return [3]string{"", "", s}, nil
}

// pyRemovePrefix is str.removeprefix(prefix).
func pyRemovePrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

// pyRemoveSuffix is str.removesuffix(suffix).
func pyRemoveSuffix(s, suffix string) string { return strings.TrimSuffix(s, suffix) }

// ---------------------------------------------------------------- padding

// pyPad surrounds s with left and right copies of fill.
func pyPad(s string, left, right int, fill rune) string {
	f := string(fill)
	return strings.Repeat(f, left) + s + strings.Repeat(f, right)
}

// pyCenter is str.center(width, fillchar). When the margin is odd the extra
// fill goes on the left only if width is odd too: 'ab'.center(5) is
// "  ab " but 'abc'.center(6) is " abc  ". Python takes width as an int
// that fits a C ssize_t and fillchar as a str of exactly one character
// (TypeError "The fill character must be exactly one character long").
func pyCenter(s string, width int, fill rune) string {
	n := textutil.RuneCount(s)
	if n >= width {
		return s
	}
	marg := width - n
	left := marg/2 + (marg & width & 1)
	return pyPad(s, left, marg-left, fill)
}

// pyLJust is str.ljust(width, fillchar); see pyCenter for the arguments.
func pyLJust(s string, width int, fill rune) string {
	if n := textutil.RuneCount(s); n < width {
		return pyPad(s, 0, width-n, fill)
	}
	return s
}

// pyRJust is str.rjust(width, fillchar); see pyCenter for the arguments.
func pyRJust(s string, width int, fill rune) string {
	if n := textutil.RuneCount(s); n < width {
		return pyPad(s, width-n, 0, fill)
	}
	return s
}

// pyZfill is str.zfill(width): zeros on the left, after a leading '+' or '-'.
func pyZfill(s string, width int) string {
	n := textutil.RuneCount(s)
	if n >= width {
		return s
	}
	zeros := strings.Repeat("0", width-n)
	if s != "" && (s[0] == '+' || s[0] == '-') {
		return s[:1] + zeros + s[1:]
	}
	return zeros + s
}
