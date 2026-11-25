// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The exported constructors of Python values and the Python operations the
// evaluator and its callers apply to them.

package py

import (
	"errors"
	"math/big"
)

// None returns Python's None.
func None() Value { return none }

// Bool returns True or False.
func Bool(b bool) Value { return pyBool(b) }

// Int returns an int.
func Int(i int64) Value { return mkInt(i) }

// BigInt returns an int of any size. The argument is copied.
func BigInt(i *big.Int) Value { return mkBig(new(big.Int).Set(i)) }

// Float returns a float.
func Float(f float64) Value { return floatV(f) }

// Complex returns a complex number.
func Complex(re, im float64) Value { return complexV(complex(re, im)) }

// String returns a str holding s, which must be valid UTF-8.
func String(s string) Value { return strV(s) }

// Bytes returns a bytes object. The argument is copied.
func Bytes(b []byte) Value { return bytesV(b) }

// List returns a new list holding the items.
func List(items ...Value) Value { return newList(append([]Value(nil), items...)) }

// Tuple returns a tuple holding the items.
func Tuple(items ...Value) Value { return newTuple(append([]Value(nil), items...)) }

// Set returns a new set (a frozenset when frozen is set) holding the items,
// added in order, or the TypeError Python raises for an unhashable item.
func Set(frozen bool, items ...Value) (Value, error) {
	s := newSet(frozen)
	for _, it := range items {
		if err := s.add(it); err != nil {
			return nil, toError(err)
		}
	}
	return s, nil
}

// NewDict returns a new empty dict.
func NewDict() Value { return newDict() }

// Repr is repr(v), or the exception repr() raises.
func Repr(v Value) (string, error) {
	s, err := pyRepr(v)
	return s, toError(err)
}

// Str is str(v), or the exception str() raises.
func Str(v Value) (string, error) {
	s, err := pyStr(v)
	return s, toError(err)
}

// TypeName is type(v).__name__.
func TypeName(v Value) string { return typeName(v) }

// TypeOf is type(v).
func TypeOf(v Value) Value { return classOf(v) }

// AsString returns the text of a str, and whether v is one.
func AsString(v Value) (string, bool) {
	s, ok := v.(strV)
	return string(s), ok
}

// Truth is bool(v).
func Truth(v Value) (bool, error) {
	t, err := truthy(v)
	return t, toError(err)
}

// Len is len(v).
func Len(v Value) (int, error) {
	n, err := pyLen(v)
	return n, toError(err)
}

// HasAttr is hasattr(v, name).
func HasAttr(v Value, name string) (bool, error) {
	a, err := getattrOpt(v, name)
	return a != nil, toError(err)
}

// GetAttr is getattr(v, name).
func GetAttr(v Value, name string) (Value, error) {
	a, err := pyGetattr(v, name)
	return a, toError(err)
}

// GetItem is v[key].
func GetItem(v, key Value) (Value, error) {
	r, err := pyGetitem(v, key)
	return r, toError(err)
}

// SetItem is v[key] = value.
func SetItem(v, key, value Value) error { return toError(pySetitem(v, key, value)) }

// Contains is `item in container`.
func Contains(container, item Value) (bool, error) {
	in, err := pyContains(container, item)
	return in, toError(err)
}

// SameObject is `a is b`.
func SameObject(a, b Value) bool { return sameObject(a, b) }

// IsCallable is callable(v).
func IsCallable(v Value) bool { return isCallable(v) }

// IsModule is isinstance(v, types.ModuleType).
func IsModule(v Value) bool {
	_, ok := v.(*moduleV)
	return ok
}

// Keyword is one keyword argument of a call.
type Keyword struct {
	Name  string
	Value Value
}

// Call calls fn with the positional and keyword arguments.
func Call(fn Value, args []Value, kwargs []Keyword) (Value, error) {
	kw := newDict()
	for _, k := range kwargs {
		kw.setStr(k.Name, k.Value)
	}
	r, err := callValue(fn, append([]Value(nil), args...), kw)
	return r, toError(err)
}

// CallMethod is v.name(*args).
func CallMethod(v Value, name string, args ...Value) (Value, error) {
	m, err := pyGetattr(v, name)
	if err != nil {
		return nil, toError(err)
	}
	r, err := callValue(m, append([]Value(nil), args...), nil)
	return r, toError(err)
}

// Iterate calls fn with each item of iter(v) in turn, stopping at the first
// error fn returns or iteration raises.
func Iterate(v Value, fn func(item Value) error) error {
	it, err := pyIter(v)
	if err != nil {
		return toError(err)
	}
	for {
		item, more, err := it.next()
		if err != nil {
			return toError(err)
		}
		if !more {
			return nil
		}
		if err := fn(item); err != nil {
			return err
		}
	}
}

// Builtin is the object a name in the builtins module stands for (a
// function, a class or a constant), or nil.
func Builtin(name string) Value { return pyBuiltinName(name) }

// Func returns a Python function named name that runs fn. A *Error it
// returns raises that exception; any other error raises a RuntimeError
// holding its text.
func Func(name string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return &callable{name: name, typ: "function", qual: name, fn: wrapGoFunc(fn)}
}

// Error is a Python exception as a Go error. Class is type(e).__name__ and
// Message is str(e).
type Error struct {
	Class   string
	Message string
	exc     *excV
}

// Error returns Message.
func (e *Error) Error() string { return e.Message }

// IsInstance reports whether the exception is an instance of the builtin or
// library exception class with the given name.
func (e *Error) IsInstance(class string) bool { return excIsInstance(e.exc, class) }

// Attr is an attribute of the exception object, nil when it has none.
func (e *Error) Attr(name string) Value {
	v, err := e.exc.getAttr(name)
	if err != nil {
		return nil
	}
	return v
}

// ErrorIs reports whether err is a Python exception that is an instance of
// the class named class, as `except class:` tests it.
func ErrorIs(err error, class string) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.IsInstance(class)
}

// Raise returns the exception class(*args), class being the name of a
// builtin or library exception class.
func Raise(class string, args ...Value) *Error {
	k := namedExcClass(class)
	if k == nil {
		return toError(typeErr("exceptions must derive from BaseException")).(*Error)
	}
	e := newExc(k.name, append([]Value(nil), args...))
	if k.module != "" {
		e.cls = k
	}
	return toError(e).(*Error)
}

// SetAttr sets an attribute of the exception object, as an exception's
// __init__ does.
func (e *Error) SetAttr(name string, value Value) { e.exc.attrDict().setStr(name, value) }

// toError converts an exception raised inside the package into an *Error.
func toError(err error) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	x := asExc(err)
	text, terr := excText(x)
	if terr != nil {
		text = "<exception str() failed>"
	}
	return &Error{Class: x.Class, Message: text, exc: x}
}

// fromGoError turns an error returned by Go code called from Python into the
// exception it raises: an *Error raises its exception, any other error a
// RuntimeError holding its text.
func fromGoError(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) && e.exc != nil {
		return e.exc
	}
	if x, ok := err.(*excV); ok {
		return x
	}
	return newPyErr("RuntimeError", err.Error())
}

func wrapGoFunc(fn func(args []Value, kwargs []Keyword) (Value, error)) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		var kwargs []Keyword
		for i, k := range kw.keys {
			name, ok := k.(strV)
			if !ok {
				return nil, typeErr("keywords must be strings")
			}
			kwargs = append(kwargs, Keyword{Name: string(name), Value: kw.vals[i]})
		}
		v, err := fn(append([]Value(nil), args...), kwargs)
		if err != nil {
			return nil, fromGoError(err)
		}
		if v == nil {
			return none, nil
		}
		return v, nil
	}
}

// BinaryOp applies the binary operator named by its ast class (Add, Sub,
// Mult, Div, FloorDiv, Mod, Pow, LShift, RShift, BitOr, BitXor, BitAnd,
// MatMult).
func BinaryOp(op string, a, b Value) (Value, error) {
	r, err := binop(op, a, b)
	return r, toError(err)
}

// Compare applies the comparison operator named by its ast class (Eq,
// NotEq, Lt, LtE, Gt, GtE, Is, IsNot, In, NotIn).
func Compare(op string, a, b Value) (Value, error) {
	r, err := compareOp(op, a, b)
	return r, toError(err)
}

// Strip is str.strip() with no argument: s without the leading and trailing
// characters Python counts as whitespace.
func Strip(s string) string { return pyStrip(s, nil) }

// Iterator is a Python iterator, as iter() returns it.
type Iterator struct{ it iterator }

// Iter is iter(v).
func Iter(v Value) (*Iterator, error) {
	it, err := pyIter(v)
	if err != nil {
		return nil, toError(err)
	}
	return &Iterator{it}, nil
}

// Next is next(it): the next item, or false at the end.
func (it *Iterator) Next() (Value, bool, error) {
	v, more, err := it.it.next()
	return v, more, toError(err)
}

// BoundMethod is a builtin method named name of the object self, running fn
// (repr "<built-in method name of Class object at 0x...>").
func BoundMethod(self Value, name string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return &callable{name: name, typ: "builtin_function_or_method", self: self, fn: wrapGoFunc(fn)}
}
