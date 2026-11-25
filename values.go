// Copyright (c) 2013-2026 Daniel Fairhead
// SPDX-License-Identifier: MIT
//
// Ported from Python (simpleeval.py) to Go, 2026-11-25.
//
// The Python values a program hands to an evaluator and gets back from it.

package simpleeval

import (
	"math/big"

	"github.com/danthedeckie/simpleeval/internal/py"
)

// Value is a Python value. A nil Value given for an evaluator's operators,
// functions or names stands for Python's None: the default.
type Value = py.Value

// Keyword is one keyword argument of a call.
type Keyword = py.Keyword

// Error is a Python exception: Class is type(e).__name__ and Message is
// str(e). Every error the package returns for something Python would raise
// is an *Error.
type Error = py.Error

// Warning is one warning issued while evaluating: the name of its category
// (AssignmentAttempted, MultipleExpressions, SyntaxWarning,
// DeprecationWarning, ...) and its message.
type Warning = py.Warning

// None is Python's None.
func None() Value { return py.None() }

// Bool is True or False.
func Bool(b bool) Value { return py.Bool(b) }

// Int is an int.
func Int(i int64) Value { return py.Int(i) }

// BigInt is an int of any size.
func BigInt(i *big.Int) Value { return py.BigInt(i) }

// Float is a float.
func Float(f float64) Value { return py.Float(f) }

// Complex is a complex number.
func Complex(real, imag float64) Value { return py.Complex(real, imag) }

// String is a str holding s, which must be valid UTF-8.
func String(s string) Value { return py.String(s) }

// Bytes is a bytes object.
func Bytes(b []byte) Value { return py.Bytes(b) }

// List is a new list of the items.
func List(items ...Value) Value { return py.List(items...) }

// Tuple is a tuple of the items.
func Tuple(items ...Value) Value { return py.Tuple(items...) }

// Set is a new set of the items, added in order, or the TypeError for an
// unhashable one.
func Set(items ...Value) (Value, error) { return py.Set(false, items...) }

// FrozenSet is a frozenset of the items, added in order, or the TypeError for
// an unhashable one.
func FrozenSet(items ...Value) (Value, error) { return py.Set(true, items...) }

// Dict is a new dict of the keys and values given alternately (key, value,
// key, value, ...), set in order, or the TypeError for an unhashable key.
func Dict(pairs ...Value) (Value, error) {
	d := py.NewDict()
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := py.SetItem(d, pairs[i], pairs[i+1]); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Builtin is what a name of CPython 3.12's builtins module stands for (len,
// int, type, eval, ...), or nil when the name is not one.
func Builtin(name string) Value { return py.Builtin(name) }

// Module is the standard-library module of the given name ("math",
// "operator", "os", "posixpath"), or nil.
func Module(name string) Value { return py.Module(name) }

// Func is a Python function named name that runs fn. An *Error that fn
// returns raises that exception; any other error raises a RuntimeError
// holding its text.
func Func(name string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return py.Func(name, fn)
}

// Repr is repr(v), or the exception repr() raises.
func Repr(v Value) (string, error) { return py.Repr(v) }

// Str is str(v), or the exception str() raises.
func Str(v Value) (string, error) { return py.Str(v) }

// GetAttr is getattr(v, name).
func GetAttr(v Value, name string) (Value, error) { return py.GetAttr(v, name) }

// GetItem is v[key].
func GetItem(v, key Value) (Value, error) { return py.GetItem(v, key) }

// SetItem is v[key] = value.
func SetItem(v, key, value Value) error { return py.SetItem(v, key, value) }

// Call calls fn with the positional and keyword arguments.
func Call(fn Value, args []Value, kwargs []Keyword) (Value, error) { return py.Call(fn, args, kwargs) }

// Raise is the exception class(*args), for a function or a names handler to
// return. class names a builtin exception class or one of this package's.
func Raise(class string, args ...Value) *Error { return py.Raise(class, args...) }

// CatchWarnings runs fn and returns every warning issued while it ran, in
// order. Outside it, warnings are written to standard error.
func CatchWarnings(fn func()) []Warning { return py.CatchWarnings(fn) }
