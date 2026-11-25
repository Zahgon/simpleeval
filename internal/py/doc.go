// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

// Package py is the Python an expression evaluator reaches: CPython 3.12's
// parser and its values, operators, builtins and modules, with CPython's
// results and exception messages.
//
// # Parsing
//
// ParseModule is ast.parse: it accepts and rejects what CPython 3.12's parser
// does, raising its SyntaxError or IndentationError, and issues its
// SyntaxWarnings through Warn. Node is the read-only view of the tree:
// Kind is the node's class name, the other methods its fields. NodeObject
// gives a node as the Python object ast.parse returns, OperatorObject the
// shared instance of an operator class and NodeClass the class itself.
//
// # Values
//
// Value is a Python value. The constructors are None, Bool, Int, BigInt,
// Float, Complex, String, Bytes, List, Tuple, Set (or a frozenset), NewDict
// and Func (a Python function running Go code); Builtin is a name of the
// builtins module and Module a standard-library module (math, operator, os
// and posixpath, as far as an evaluator needs them). DefineClass and
// NewObject describe classes and instances of a library such as
// simpleeval's ModuleWrapper.
//
// The operations are Python's: Repr, Str, TypeName, TypeOf, Truth, Len,
// HasAttr, GetAttr, GetItem, SetItem, Contains, SameObject (is), IsCallable,
// IsModule, Call, CallMethod, Iterate and Iter, BinaryOp and Compare. A set
// iterates and prints in the order of CPython's set table, a dict in
// insertion order, and every number, string and container method computes
// what CPython computes.
//
// # Exceptions and warnings
//
// Every error the exported functions return for a Python exception is an
// *Error: Class is type(e).__name__, Message is str(e), and IsInstance tests
// its class. Raise makes the exception class(*args) of a builtin or library
// exception class. Warn issues a warning; CatchWarnings records the warnings
// a function issues, as warnings.catch_warnings(record=True) does.
package py
