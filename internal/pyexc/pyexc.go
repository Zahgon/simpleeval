// Ported from CPython 3.12 (Objects/exceptions.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

// Package pyexc holds the error the internal packages return for a Python
// exception, so that every package above them turns it into an exception
// object the same way.
package pyexc

import (
	"errors"
	"strconv"
)

// Exception is a Python exception: Class is the name of its class (such as
// "ValueError") and Msg is str() of it, which Error also returns. Packages
// whose exceptions carry more attributes embed it.
type Exception struct {
	Class string
	Msg   string
}

// New is the exception class(msg).
func New(class, msg string) *Exception { return &Exception{Class: class, Msg: msg} }

func (e *Exception) Error() string { return e.Msg }

// PyException returns e. Promoted through embedding, it returns the
// Exception part of a richer error, which is how As finds it.
func (e *Exception) PyException() *Exception { return e }

// As returns the Python exception err stands for, when err (or an error it
// wraps) is an *Exception or embeds one.
func As(err error) (*Exception, bool) {
	var x interface{ PyException() *Exception }
	if errors.As(err, &x) {
		return x.PyException(), true
	}
	return nil, false
}

// NoAttribute is the AttributeError of reading the attribute name of an
// object whose type is named typ.
func NoAttribute(typ, name string) *Exception {
	return New("AttributeError", "'"+typ+"' object has no attribute '"+name+"'")
}

// MaxStrDigits is sys.get_int_max_str_digits(), the longest decimal string
// int() converts and the longest decimal string str() makes of an int.
const MaxStrDigits = 4300

// DigitLimit is the ValueError int() raises for a decimal string of digits
// digits, more than MaxStrDigits.
func DigitLimit(digits int) *Exception {
	return New("ValueError", "Exceeds the limit ("+strconv.Itoa(MaxStrDigits)+" digits) for integer string "+
		"conversion: value has "+strconv.Itoa(digits)+" digits; use sys.set_int_max_str_digits() to increase the limit")
}

// Plural is the "s" of a count in CPython's messages: "" for 1, else "s".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
