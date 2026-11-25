// Ported from CPython 3.12 (Lib/encodings/__init__.py, Python/codecs.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package codecs

import (
	"strings"

	"github.com/danthedeckie/simpleeval/internal/pyexc"
)

// Codec is the CodecInfo codecs.lookup returns.
type Codec struct {
	// Name is CodecInfo.name, for example "utf-8", "iso8859-1" or "cp1252".
	Name string
	// Module is the module of the encodings package that defines the codec,
	// for example "utf_8" or "shift_jis".
	Module string
	table  *charmap
}

// NormalizeEncoding is the normalisation codecs.lookup applies before the
// search: runs of characters other than ASCII letters, digits and '.' become
// one '_' (dropped at the start and end) and ASCII letters are lowercased.
func NormalizeEncoding(name string) string {
	var sb strings.Builder
	punct := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		isAlnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if isAlnum || c == '.' {
			if punct && sb.Len() > 0 {
				sb.WriteByte('_')
			}
			punct = false
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			sb.WriteByte(c)
		} else {
			punct = true
		}
	}
	return sb.String()
}

// ModuleOf is the name search of codecs.lookup(encoding) over the modules of
// CPython's encodings package: it resolves a name or alias to the module that
// defines the codec, such as "utf_8" for "UTF8" or "shift_jis" for "sjis". It
// deals in module names only and says nothing about which codecs Lookup
// has; an unknown name gives LookupError "unknown encoding: <encoding>".
func ModuleOf(encoding string) (string, error) {
	if strings.IndexByte(encoding, 0) >= 0 {
		return "", pyexc.New("ValueError", "embedded null character")
	}
	norm := NormalizeEncoding(encoding)
	candidates := []string{norm}
	alias, ok := aliases[norm]
	if !ok {
		alias, ok = aliases[strings.ReplaceAll(norm, ".", "_")]
	}
	if ok {
		candidates = []string{alias, norm}
	}
	for _, mod := range candidates {
		if mod == "" || strings.Contains(mod, ".") {
			continue
		}
		if _, ok := moduleNames[mod]; ok {
			return mod, nil
		}
	}
	return "", unknownEncoding(encoding)
}

func unknownEncoding(encoding string) *Error { return lookupError("unknown encoding: " + encoding) }

// Lookup is codecs.lookup(encoding) over the codecs of this package (see the
// package documentation); any other name gives LookupError "unknown
// encoding: <encoding>".
func Lookup(encoding string) (*Codec, error) {
	mod, err := ModuleOf(encoding)
	if err != nil {
		return nil, err
	}
	if !hasCodec(mod) {
		return nil, unknownEncoding(encoding)
	}
	c := &Codec{Name: moduleNames[mod], Module: mod}
	if _, ok := charmapTables[mod]; ok {
		c.table = buildCharmap(mod)
	}
	return c, nil
}

// hasCodec reports whether the package has a codec for the module: one of
// the Unicode codecs of decode.go and encode.go, or a charmap table.
func hasCodec(mod string) bool {
	switch mod {
	case "utf_8", "utf_8_sig", "ascii", "latin_1", "utf_16", "utf_16_le", "utf_16_be",
		"utf_32", "utf_32_le", "utf_32_be":
		return true
	}
	_, ok := charmapTables[mod]
	return ok
}

// Decode is bytes.decode(encoding, errors).
func Decode(b []byte, encoding, errors string) (string, error) {
	c, err := lookupText(encoding, "decode")
	if err != nil {
		return "", err
	}
	return c.Decode(b, errors)
}

// Encode is str.encode(encoding, errors).
func Encode(s, encoding, errors string) ([]byte, error) {
	c, err := lookupText(encoding, "encode")
	if err != nil {
		return nil, err
	}
	return c.Encode(s, errors)
}

// lookupText is the codec lookup of str.encode and bytes.decode, which only
// accept text encodings.
func lookupText(encoding, method string) (*Codec, error) {
	mod, err := ModuleOf(encoding)
	if err != nil {
		return nil, err
	}
	if nonTextModules[mod] {
		return nil, lookupError("'" + encoding + "' is not a text encoding; use codecs." + method +
			"() to handle arbitrary codecs")
	}
	return Lookup(encoding)
}

// Decode is codec.decode(b, errors)[0]: the whole input is decoded and an
// incomplete sequence at its end is an error.
func (c *Codec) Decode(b []byte, errors string) (string, error) {
	s, _, err := c.decode(b, errors, true, byteOrderDetect)
	return s, err
}
