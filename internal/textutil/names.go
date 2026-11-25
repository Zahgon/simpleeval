// Ported from CPython 3.12 (Modules/unicodedata.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package textutil

import (
	_ "embed"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
)

// unicodeNamesData holds the character names (see gen_names.py for the
// format).
//
//go:embed unicode_names.bin
var unicodeNamesData []byte

// nameRange gives the code points lo..hi the names prefix + their hex code.
type nameRange struct {
	prefix   string
	lo, hi   rune
	accepted bool
}

// nameTable is unicodeNamesData decoded: the name of each code point, the
// code point of each name or alias \N{...} accepts, and the hex-named ranges.
type nameTable struct {
	names   map[rune]string
	codes   map[string]rune
	ranges  []nameRange
	unified [][2]rune
}

var (
	namesOnce sync.Once
	names     nameTable
)

// loadNames decodes unicodeNamesData, which gen_names.py wrote and which
// therefore holds well-formed varints and strings.
func loadNames() {
	b := unicodeNamesData
	num := func() int {
		v, n := binary.Uvarint(b)
		b = b[n:]
		return int(v)
	}
	str := func() string {
		n := num()
		s := string(b[:n])
		b = b[n:]
		return s
	}
	words := make([]string, num())
	for i := range words {
		words[i] = str()
	}
	names = nameTable{names: map[rune]string{}, codes: map[string]rune{}}
	r := rune(0)
	for n := num(); n > 0; n-- {
		r += rune(num())
		accepted := num() == 1
		parts := make([]string, num())
		for i := range parts {
			parts[i] = words[num()]
		}
		name := strings.Join(parts, " ")
		names.names[r] = name
		if accepted {
			names.codes[name] = r
		}
	}
	for n := num(); n > 0; n-- {
		prefix := str()
		lo, hi := rune(num()), rune(num())
		names.ranges = append(names.ranges, nameRange{prefix, lo, hi, num() == 1})
	}
	for n := num(); n > 0; n-- {
		r := rune(num())
		names.codes[str()] = r
	}
	for n := num(); n > 0; n-- {
		lo, hi := rune(num()), rune(num())
		names.unified = append(names.unified, [2]rune{lo, hi})
	}
}

// The jamo short names of the Hangul syllable names.
var (
	hangulLNames = []string{"G", "GG", "N", "D", "DD", "R", "M", "B", "BB", "S", "SS", "", "J", "JJ", "C", "K", "T", "P", "H"}
	hangulVNames = []string{"A", "AE", "YA", "YAE", "EO", "E", "YEO", "YE", "O", "WA", "WAE", "OE", "YO", "U", "WEO",
		"WE", "WI", "YU", "EU", "YI", "I"}
	hangulTNames = []string{"", "G", "GG", "GS", "N", "NJ", "NH", "D", "L", "LG", "LM", "LB", "LS", "LT", "LP", "LH", "M",
		"B", "BS", "S", "SS", "NG", "J", "C", "K", "T", "P", "H"}
)

// isUnifiedIdeograph reports whether r is a CJK unified ideograph, whose
// name is "CJK UNIFIED IDEOGRAPH-" and its hex code.
func isUnifiedIdeograph(r rune) bool {
	for _, rg := range names.unified {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

// hexCode is a code point in hex as the names write it: at least four
// digits, in capitals.
func hexCode(r rune) string {
	s := strings.ToUpper(strconv.FormatInt(int64(r), 16))
	return strings.Repeat("0", max(0, 4-len(s))) + s
}

// Name is unicodedata.name(r): the character's name, false when it has none.
func Name(r rune) (string, bool) {
	namesOnce.Do(loadNames)
	if r >= hangulSBase && r < hangulSBase+hangulSCount {
		i := r - hangulSBase
		return "HANGUL SYLLABLE " + hangulLNames[i/hangulNCount] + hangulVNames[i%hangulNCount/hangulTCount] +
			hangulTNames[i%hangulTCount], true
	}
	if isUnifiedIdeograph(r) {
		return "CJK UNIFIED IDEOGRAPH-" + hexCode(r), true
	}
	if n, ok := names.names[r]; ok {
		return n, true
	}
	for _, rg := range names.ranges {
		if r >= rg.lo && r <= rg.hi {
			return rg.prefix + hexCode(r), true
		}
	}
	return "", false
}

// LookupName is the character \N{name} stands for in a str literal: the
// character with that name or alias, in any case; the names of the Hangul
// syllables and the CJK unified ideographs are read in capitals only, the
// latter with four or five hex digits.
func LookupName(name string) (rune, bool) {
	namesOnce.Do(loadNames)
	if rest, ok := strings.CutPrefix(name, "HANGUL SYLLABLE "); ok {
		return lookupHangul(rest)
	}
	if hex, ok := strings.CutPrefix(name, "CJK UNIFIED IDEOGRAPH-"); ok {
		if len(hex) != 4 && len(hex) != 5 || strings.ToUpper(hex) != hex {
			return 0, false
		}
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil || !isUnifiedIdeograph(rune(v)) {
			return 0, false
		}
		return rune(v), true
	}
	upper := strings.ToUpper(name)
	if r, ok := names.codes[upper]; ok {
		return r, true
	}
	for _, rg := range names.ranges {
		if hex, ok := strings.CutPrefix(upper, rg.prefix); ok && rg.accepted {
			if v, err := strconv.ParseUint(hex, 16, 32); err == nil && rune(v) >= rg.lo && rune(v) <= rg.hi &&
				hexCode(rune(v)) == hex {
				return rune(v), true
			}
		}
	}
	return 0, false
}

// lookupHangul reads the jamo short names of a Hangul syllable name: the
// longest leading consonant, vowel and trailing consonant that match, which
// must use up the name.
func lookupHangul(s string) (rune, bool) {
	longest := func(names []string) int {
		best, size := -1, -1
		for i, n := range names {
			if len(n) > size && strings.HasPrefix(s, n) {
				best, size = i, len(n)
			}
		}
		if best >= 0 {
			s = s[size:]
		}
		return best
	}
	l := longest(hangulLNames)
	v := longest(hangulVNames)
	t := longest(hangulTNames)
	if l < 0 || v < 0 || t < 0 || s != "" {
		return 0, false
	}
	return hangulSBase + rune((l*hangulVCount+v)*hangulTCount+t), true
}
