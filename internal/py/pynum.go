// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import "github.com/danthedeckie/simpleeval/internal/textutil"

// pynumReprPrec is "%.200R": repr() cut to its first 200 characters; the
// error is the one repr() raised.
func pynumReprPrec(v Value) (string, error) {
	s, err := pyRepr(v)
	return textutil.Prefix(s, 200), err
}

// pyParseIntOf is int(s, base) for the str object obj with text s (see
// textutil.ParseInt); the errors are Python's ValueErrors, word for word,
// showing repr(obj).
func pyParseIntOf(s string, base int, obj Value) (Value, error) {
	if base != 0 && base < 2 || base > 36 {
		return nil, valueErr("int() base must be >= 2 and <= 36, or 0")
	}
	v, ok, err := textutil.ParseInt(s, base)
	if err != nil {
		return nil, fromLeaf(err)
	}
	if !ok {
		r, err := pynumReprPrec(obj)
		if err != nil {
			return nil, err
		}
		return nil, valueErr("invalid literal for int() with base %d: %s", base, r)
	}
	return mkBig(v), nil
}

// pyParseFloatOf is float(s) for the str object obj with text s (see
// textutil.ParseFloat), whose repr() the error shows.
func pyParseFloatOf(s string, obj Value) (float64, error) {
	f, ok := textutil.ParseFloat(s)
	if !ok {
		r, err := pyRepr(obj)
		if err != nil {
			return 0, err
		}
		return 0, valueErr("could not convert string to float: %s", r)
	}
	return f, nil
}
