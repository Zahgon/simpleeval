// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Python exceptions: the builtin classes, their hierarchy, and exception instances as Python
// values.

package py

import (
	"errors"
	"fmt"

	"github.com/danthedeckie/simpleeval/internal/codecs"
	"github.com/danthedeckie/simpleeval/internal/pyexc"
	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// excV is a Python exception instance. It is both a Value (it can be bound
// by `except ... as e`, printed and inspected) and a Go error (it is what a
// Python operation raises).
type excV struct {
	Class string
	// Message is the text of the exception for Go code: str() of it as it
	// was raised (see excStr). Python code gets str() from excText.
	Message string
	// ownStr is set when Message is the str() the exception's class
	// formats itself (a codec error), rather than one derived from args.
	ownStr bool
	args   []Value
	// attrs holds attributes set on the instance (nil until one is).
	attrs *dictV
	// cls is the class of an exception whose class is not a builtin one
	// (simpleeval's exception classes).
	cls *classV
}

// excText is str() of an exception: BaseException.__str__, or the __str__ of
// the builtin class it derives from.
func excText(e *excV) (string, error) {
	base := excClass(e).builtinBase().name
	if subclass(base, "SyntaxError") && e.attrs != nil {
		return syntaxErrorStr(e)
	}
	if hasOwnStr(base) && e.ownStr {
		return e.Message, nil
	}
	return excArgsStr(base, e.args)
}

// hasOwnStr reports whether a builtin exception class formats str() itself
// rather than from its args.
func hasOwnStr(class string) bool {
	return subclass(class, "OSError") || subclass(class, "UnicodeError") && class != "UnicodeError" ||
		subclass(class, "ImportError")
}

// excArgsStr is BaseException.__str__ (KeyError.__str__ for KeyError): the
// str of the single argument, or the repr of the args.
func excArgsStr(class string, args []Value) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		if subclass(class, "KeyError") {
			return pyRepr(args[0])
		}
		return pyStr(args[0])
	}
	return pyRepr(newTuple(args))
}

// syntaxErrorStr is SyntaxError.__str__: the msg attribute, with the base
// name of the file and the line number when they are set.
func syntaxErrorStr(e *excV) (string, error) {
	msgV, _ := e.attrs.getStr("msg")
	if msgV == nil {
		msgV = none
	}
	msg, err := pyStr(msgV)
	if err != nil {
		return "", err
	}
	fileV, _ := e.attrs.getStr("filename")
	lineV, _ := e.attrs.getStr("lineno")
	file, haveFile := fileV.(strV)
	line, haveLine := asInt(lineV)
	switch {
	case haveFile && haveLine:
		return msg + " (" + textutil.Basename(string(file)) + ", line " + line.String() + ")", nil
	case haveFile:
		return msg + " (" + textutil.Basename(string(file)) + ")", nil
	case haveLine:
		return msg + " (line " + line.String() + ")", nil
	}
	return msg, nil
}

func (e *excV) Error() string { return e.Class + ": " + e.Message }

// attrDict is the instance dictionary of the exception.
func (e *excV) attrDict() *dictV {
	if e.attrs == nil {
		e.attrs = newDict()
	}
	return e.attrs
}

// getAttr is attribute access on an exception: args, the instance
// dictionary, then BaseException's attributes.
func (e *excV) getAttr(name string) (Value, error) {
	if name == "args" {
		return newTuple(append([]Value(nil), e.args...)), nil
	}
	if e.attrs != nil {
		if v, ok := e.attrs.getStr(name); ok {
			return v, nil
		}
	}
	if v, ok := excMethod(e, name); ok {
		return v, nil
	}
	return nil, nil
}

// excBuiltinAttr is an attribute BaseException (or a builtin subclass)
// defines for its instances.
func excBuiltinAttr(e *excV, name string) (Value, bool) {
	switch name {
	case "__traceback__", "__cause__", "__context__":
		return none, true
	case "__suppress_context__":
		return falseV, true
	case "__class__":
		return excClass(e), true
	case "value":
		if subclass(excClass(e).builtinBase().name, "StopIteration") {
			if len(e.args) > 0 {
				return e.args[0], true
			}
			return none, true
		}
	case "with_traceback":
		return methodsBound(e, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("with_traceback", args, kw); err != nil {
				return nil, err
			}
			return e, nil
		}), true
	}
	return nil, false
}

// newPyErr returns an exception raised with a single message argument.
func newPyErr(class, msg string) *excV {
	return &excV{Class: class, Message: msg, args: []Value{strV(msg)}}
}

// newExc returns an exception built as Class(*args) is in Python.
func newExc(class string, args []Value) *excV {
	e := &excV{Class: class, args: args}
	e.Message = excStr(class, args)
	return e
}

// excStr is the Message of an exception built from args: the str() of the
// args. Building an exception does not call str() in Python, so a str() that
// raises is not an error here; the Message is then the text CPython shows for
// such an exception, and excText raises the error when Python code asks for
// str().
func excStr(class string, args []Value) string {
	s, err := excArgsStr(class, args)
	if err != nil {
		return "<exception str() failed>"
	}
	return s
}

// excParents is the class hierarchy of the builtin exceptions.
var excParents = map[string]string{
	"BaseException":             "",
	"Exception":                 "BaseException",
	"KeyboardInterrupt":         "BaseException",
	"SystemExit":                "BaseException",
	"GeneratorExit":             "BaseException",
	"ArithmeticError":           "Exception",
	"FloatingPointError":        "ArithmeticError",
	"OverflowError":             "ArithmeticError",
	"ZeroDivisionError":         "ArithmeticError",
	"AssertionError":            "Exception",
	"AttributeError":            "Exception",
	"BufferError":               "Exception",
	"EOFError":                  "Exception",
	"ImportError":               "Exception",
	"ModuleNotFoundError":       "ImportError",
	"LookupError":               "Exception",
	"IndexError":                "LookupError",
	"KeyError":                  "LookupError",
	"MemoryError":               "Exception",
	"NameError":                 "Exception",
	"UnboundLocalError":         "NameError",
	"OSError":                   "Exception",
	"ReferenceError":            "Exception",
	"RuntimeError":              "Exception",
	"NotImplementedError":       "RuntimeError",
	"RecursionError":            "RuntimeError",
	"StopIteration":             "Exception",
	"SyntaxError":               "Exception",
	"IndentationError":          "SyntaxError",
	"TabError":                  "IndentationError",
	"SystemError":               "Exception",
	"TypeError":                 "Exception",
	"ValueError":                "Exception",
	"UnicodeError":              "ValueError",
	"UnicodeDecodeError":        "UnicodeError",
	"UnicodeEncodeError":        "UnicodeError",
	"UnicodeTranslateError":     "UnicodeError",
	"Warning":                   "Exception",
	"BytesWarning":              "Warning",
	"DeprecationWarning":        "Warning",
	"ImportWarning":             "Warning",
	"RuntimeWarning":            "Warning",
	"SyntaxWarning":             "Warning",
	"UnicodeWarning":            "Warning",
	"UserWarning":               "Warning",
	"FutureWarning":             "Warning",
	"PendingDeprecationWarning": "Warning",
	"ResourceWarning":           "Warning",
	"EncodingWarning":           "Warning",
	"StopAsyncIteration":        "Exception",
	"BaseExceptionGroup":        "BaseException",
	"ExceptionGroup":            "BaseExceptionGroup",
	"BlockingIOError":           "OSError",
	"ChildProcessError":         "OSError",
	"ConnectionError":           "OSError",
	"BrokenPipeError":           "ConnectionError",
	"ConnectionAbortedError":    "ConnectionError",
	"ConnectionRefusedError":    "ConnectionError",
	"ConnectionResetError":      "ConnectionError",
	"FileExistsError":           "OSError",
	"FileNotFoundError":         "OSError",
	"InterruptedError":          "OSError",
	"IsADirectoryError":         "OSError",
	"NotADirectoryError":        "OSError",
	"PermissionError":           "OSError",
	"ProcessLookupError":        "OSError",
	"TimeoutError":              "OSError",
}

// excAliases are the builtin names bound to another exception class.
var excAliases = map[string]string{"EnvironmentError": "OSError", "IOError": "OSError"}

func subclass(class, of string) bool {
	for c := class; c != ""; c = excParents[c] {
		if c == of {
			return true
		}
	}
	return false
}

// isExc reports whether err is a Python exception of one of the classes.
func isExc(err error, classes ...string) bool {
	var e *excV
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range classes {
		if subclass(e.Class, c) {
			return true
		}
	}
	return false
}

// asExc returns err as an exception, wrapping a foreign Go error as
// SystemError (it would be a bug in this package).
func asExc(err error) *excV {
	var e *excV
	if errors.As(err, &e) {
		return e
	}
	return newPyErr("SystemError", err.Error())
}

func typeErr(format string, args ...any) *excV {
	return newPyErr("TypeError", fmt.Sprintf(format, args...))
}

func valueErr(format string, args ...any) *excV {
	return newPyErr("ValueError", fmt.Sprintf(format, args...))
}

// attrErr is the AttributeError of reading the attribute name of an object of
// type typ.
func attrErr(typ, name string) *excV {
	e := pyexc.NoAttribute(typ, name)
	return newPyErr(e.Class, e.Msg)
}

// typeAttrErr is the AttributeError of reading the attribute name of the
// class named typ.
func typeAttrErr(typ, name string) *excV {
	return newPyErr("AttributeError", "type object '"+typ+"' has no attribute '"+name+"'")
}

// fromLeaf turns the Python exception an internal package returned (see
// package pyexc) into an exception object; a codec failure keeps the
// attributes of UnicodeDecodeError and UnicodeEncodeError. Other errors pass
// through.
func fromLeaf(err error) error {
	if e, ok := err.(*codecs.Error); ok {
		switch e.Class {
		case "UnicodeDecodeError":
			x := newExc(e.Class, []Value{strV(e.Encoding), bytesV(e.Object), mkInt(int64(e.Start)),
				mkInt(int64(e.End)), strV(e.Reason)})
			x.Message, x.ownStr = e.Msg, true
			setCodecAttrs(x, e, bytesV(e.Object))
			return x
		case "UnicodeEncodeError":
			x := newExc(e.Class, []Value{strV(e.Encoding), strV(e.Text), mkInt(int64(e.Start)),
				mkInt(int64(e.End)), strV(e.Reason)})
			x.Message, x.ownStr = e.Msg, true
			setCodecAttrs(x, e, strV(e.Text))
			return x
		}
	}
	if e, ok := pyexc.As(err); ok {
		return newPyErr(e.Class, e.Msg)
	}
	return err
}

// excClass is type(e).
func excClass(e *excV) *classV {
	if e.cls != nil {
		return e.cls
	}
	return excClassOf(e.Class)
}

// excIsInstance reports whether e is an instance of the exception class
// named class: a builtin one, an alias of one, or a library one.
func excIsInstance(e *excV, class string) bool {
	k := namedExcClass(class)
	return k != nil && excClass(e).subclassOf(k)
}

// namedExcClass is the exception class of the given name, or nil.
func namedExcClass(name string) *classV {
	if k, ok := libraryClasses[name]; ok && isExcClass(k) {
		return k
	}
	if k, ok := classes[name]; ok && isExcClass(k) {
		return k
	}
	if alias, ok := excAliases[name]; ok {
		return classes[alias]
	}
	return nil
}

// excReprDefault is BaseException.__repr__.
func excReprDefault(x *excV, st *reprState) (string, error) {
	switch len(x.args) {
	case 0:
		return x.Class + "()", nil
	case 1:
		s, err := reprWith(x.args[0], st)
		if err != nil {
			return "", err
		}
		return x.Class + "(" + s + ")", nil
	}
	s, err := seqRepr("(", ")", x.args, true, st)
	if err != nil {
		return "", err
	}
	return x.Class + s, nil
}
