// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Interfaces the value layer dispatches through, str and int subclasses, the
// builtins beyond the common table, and
// compare helpers.

package py

import (
	"os"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/textutil"
)

type getitemer interface {
	getItem(key Value) (Value, error)
}
type setitemer interface {
	setItem(key, value Value) error
}
type delitemer interface {
	delItem(key Value) error
}
type attrGetter interface {
	getAttr(name string) (Value, error)
}
type attrSetter interface {
	setAttr(name string, value Value) error
}
type attrDeleter interface {
	delAttr(name string) error
}
type truther interface {
	truth() (bool, error)
}
type callableObj interface {
	pyCall(args []Value, kw *dictV) (Value, error)
}
type callableChecker interface {
	isCallable() bool
}

// commonAttr holds the attributes every object answers to.
func commonAttr(obj Value, name string) (Value, bool) {
	switch name {
	case "__class__":
		if _, ok := obj.(*classV); ok {
			return classes["type"], true
		}
		return classOf(obj), true
	case "__name__", "__qualname__":
		if k, ok := obj.(*classV); ok {
			return strV(k.name), true
		}
		if c, ok := obj.(*callable); ok {
			return strV(c.name), true
		}
	case "__getitem__":
		if hasGetitem(obj) {
			return &callable{name: "__getitem__", typ: "builtin_function_or_method", self: obj,
				fn: func(args []Value, kw *dictV) (Value, error) {
					if err := posOnly("__getitem__", 1, 1, args, kw); err != nil {
						return nil, err
					}
					return pyGetitem(obj, args[0])
				}}, true
		}
	}
	return nil, false
}

// hasGetitem tells whether obj has a __getitem__ attribute.
func hasGetitem(obj Value) bool {
	switch x := obj.(type) {
	case *listV, *tupleV, *dictV, strV, bytesV, *bytearrayV, rangeV, *classV:
		return true
	case *Object:
		return x.GetItem != nil
	}
	return false
}

// allPythonBuiltins is dir(builtins) under CPython 3.12.
var allPythonBuiltins = strings.Fields(`ArithmeticError AssertionError AttributeError BaseException
BaseExceptionGroup BlockingIOError BrokenPipeError BufferError BytesWarning ChildProcessError
ConnectionAbortedError ConnectionError ConnectionRefusedError ConnectionResetError DeprecationWarning EOFError
Ellipsis EncodingWarning EnvironmentError Exception ExceptionGroup False FileExistsError FileNotFoundError
FloatingPointError FutureWarning GeneratorExit IOError ImportError ImportWarning IndentationError IndexError
InterruptedError IsADirectoryError KeyError KeyboardInterrupt LookupError MemoryError ModuleNotFoundError
NameError None NotADirectoryError NotImplemented NotImplementedError OSError OverflowError
PendingDeprecationWarning PermissionError ProcessLookupError RecursionError ReferenceError ResourceWarning
RuntimeError RuntimeWarning StopAsyncIteration StopIteration SyntaxError SyntaxWarning SystemError SystemExit
TabError TimeoutError True TypeError UnboundLocalError UnicodeDecodeError UnicodeEncodeError UnicodeError
UnicodeTranslateError UnicodeWarning UserWarning ValueError Warning ZeroDivisionError __build_class__ __debug__
__doc__ __import__ __loader__ __name__ __package__ __spec__ abs aiter all anext any ascii bin bool breakpoint
bytearray bytes callable chr classmethod compile complex copyright credits delattr dict dir divmod enumerate
eval exec exit filter float format frozenset getattr globals hasattr hash help hex id input int isinstance
issubclass iter len license list locals map max memoryview min next object oct open ord pow print property quit
range repr reversed round set setattr slice sorted staticmethod str sum super tuple type vars zip`)

// initExtraBuiltins adds the builtins beyond the classes and functions of
// pybuiltins.go.
func initExtraBuiltins() {
	classes["object"] = objectClass
	for _, singleton := range []struct {
		name, typ string
		v         Value
	}{{"ellipsis", "EllipsisType", ellipsisV{}}, {"NoneType", "NoneType", none},
		{"NotImplementedType", "NotImplementedType", notImplementedV{}}} {
		singleton := singleton
		cls(singleton.name, objectClass, func(args []Value, kw *dictV) (Value, error) {
			if len(args) > 0 || kw != nil && kw.Len() > 0 {
				return nil, typeErr("%s takes no arguments", singleton.typ)
			}
			return singleton.v, nil
		})
	}
	objectClass.construct = func(args []Value, kw *dictV) (Value, error) {
		if len(args) > 0 || kw != nil && kw.Len() > 0 {
			return nil, typeErr("object() takes no arguments")
		}
		return &Object{Class: "object"}, nil
	}
	typeC := classes["type"]
	typeC.construct = func(args []Value, kw *dictV) (Value, error) {
		if len(args) != 1 && len(args) != 3 {
			return nil, typeErr("type() takes 1 or 3 arguments")
		}
		if len(args) == 3 {
			return nil, typeErr("type() cannot create classes in this evaluator")
		}
		if err := noKw("type", kw); err != nil {
			return nil, err
		}
		return classOf(args[0]), nil
	}
	extra := map[string]func(args []Value, kw *dictV) (Value, error){
		"callable": oneArg("callable", func(v Value) (Value, error) { return pyBool(isCallable(v)), nil }),
		"ascii": oneArg("ascii", func(v Value) (Value, error) {
			s, err := fmtASCII(v)
			return strV(s), err
		}),
		"getattr": func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("getattr", kw); err != nil {
				return nil, err
			}
			if len(args) < 2 {
				return nil, typeErr("getattr expected at least 2 arguments, got %d", len(args))
			}
			if len(args) > 3 {
				return nil, typeErr("getattr expected at most 3 arguments, got %d", len(args))
			}
			name, ok := args[1].(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			v, err := pyGetattr(args[0], string(name))
			if err != nil && len(args) == 3 && isExc(err, "AttributeError") {
				return args[2], nil
			}
			return v, err
		},
		"hasattr": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("hasattr", 2, 2, args, kw); err != nil {
				return nil, err
			}
			name, ok := args[1].(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			_, err := pyGetattr(args[0], string(name))
			if err != nil {
				if isExc(err, "AttributeError") {
					return falseV, nil
				}
				return nil, err
			}
			return trueV, nil
		},
		"setattr": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("setattr", 3, 3, args, kw); err != nil {
				return nil, err
			}
			name, ok := args[1].(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			return none, pySetattr(args[0], string(name), args[2])
		},
		"delattr": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("delattr", 2, 2, args, kw); err != nil {
				return nil, err
			}
			name, ok := args[1].(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			return none, pyDelattr(args[0], string(name))
		},
		"issubclass": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("issubclass", 2, 2, args, kw); err != nil {
				return nil, err
			}
			k, ok := args[0].(*classV)
			if !ok {
				return nil, typeErr("issubclass() arg 1 must be a class")
			}
			match := func(o Value) (bool, error) {
				ok2, isCls := o.(*classV)
				if !isCls {
					return false, typeErr("issubclass() arg 2 must be a class, a tuple of classes, or a union")
				}
				return k.subclassOf(ok2) || ok2 == objectClass, nil
			}
			if t, ok := args[1].(*tupleV); ok {
				for _, o := range t.items {
					m, err := match(o)
					if err != nil || m {
						return pyBool(m), err
					}
				}
				return falseV, nil
			}
			m, err := match(args[1])
			return pyBool(m), err
		},
		"iter": func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("iter", kw); err != nil {
				return nil, err
			}
			switch len(args) {
			case 0:
				return nil, typeErr("iter expected at least 1 argument, got 0")
			case 1:
				return makeIterObject(args[0])
			case 2:
				return callableIter(args[0], args[1])
			}
			return nil, typeErr("iter expected at most 2 arguments, got %d", len(args))
		},
		"next": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("next", 1, 2, args, kw); err != nil {
				return nil, err
			}
			x, isIterObj := args[0].(*iterObj)
			if !isIterObj {
				return nil, typeErr("'%s' object is not an iterator", typeName(args[0]))
			}
			it, err := x.iter()
			if err != nil {
				return nil, err
			}
			v, more, err := it.next()
			if err != nil {
				return nil, err
			}
			if !more {
				if len(args) == 2 {
					return args[1], nil
				}
				return nil, newExc("StopIteration", nil)
			}
			return v, nil
		},
		"print": builtinPrint,
	}
	for name, fn := range extra {
		builtinsTable[name] = newBuiltin(name, fn)
	}
	builtinsTable["NotImplemented"] = notImplementedV{}
	builtinsTable["Ellipsis"] = ellipsisV{}
	builtinsTable["__name__"] = strV("builtins")
	builtinsTable["__package__"] = strV("")
	builtinsTable["__debug__"] = trueV
	// The builtins that run code, read the caller's namespaces or open files
	// exist only as values: simpleeval refuses each of them by identity
	// (DisallowFunctions) before an expression could call it, and calling one
	// directly is refused with the reason.
	for name, why := range map[string]string{
		"exec":    "this library evaluates expressions only through its own evaluator",
		"eval":    "this library evaluates expressions only through its own evaluator",
		"compile": "this library evaluates expressions only through its own evaluator",
		"globals": "an evaluated expression has no calling Python frame",
		"locals":  "an evaluated expression has no calling Python frame",
		"open":    "this library does not open files",
	} {
		if _, ok := builtinsTable[name]; !ok {
			builtinsTable[name] = newBuiltin(name, refusedBuiltin(name, why))
		}
	}
	builtinsTable["__doc__"] = strV(builtinsDoc)
	loader := &Object{Class: "type", Repr: func() (string, error) {
		return "<class '_frozen_importlib.BuiltinImporter'>", nil
	}}
	builtinsTable["__loader__"] = loader
	builtinsTable["__spec__"] = &Object{Class: "ModuleSpec", Repr: func() (string, error) {
		return "ModuleSpec(name='builtins', loader=<class '_frozen_importlib.BuiltinImporter'>, origin='built-in')", nil
	}}
	// The objects the site module adds keep their reprs; exit() and quit()
	// raise SystemExit, while the printers and help, which write to the
	// terminal, are not callable here.
	for name, text := range sitePrinters {
		o := &Object{Module: "_sitebuiltins", Class: "_Printer", Repr: func() (string, error) { return text, nil }}
		switch name {
		case "exit", "quit":
			o.Class = "Quitter"
			o.Call = func(args []Value, kwargs []Keyword) (Value, error) {
				code := Value(none)
				if len(args) > 0 {
					code = args[0]
				}
				return nil, newExc("SystemExit", []Value{code})
			}
		case "help":
			// The interactive help reads from a terminal; simpleeval refuses
			// help by identity, and a direct call is refused with the reason.
			o.Class = "_Helper"
			refuse := refusedBuiltin("help", "an evaluated expression has no interactive terminal")
			o.Call = func([]Value, []Keyword) (Value, error) { return refuse(nil, nil) }
		}
		builtinsTable[name] = o
	}

}

// callableIter is iter(callable, sentinel).
func callableIter(fn, sentinel Value) (Value, error) {
	if !isCallable(fn) {
		return nil, typeErr("iter(v, w): v must be callable")
	}
	done := false
	return &iterObj{typ: "callable_iterator", it: funcIter(func() (Value, bool, error) {
		if done {
			return nil, false, nil
		}
		v, err := callValue(fn, nil, nil)
		if err != nil {
			if isExc(err, "StopIteration") {
				done = true
				return nil, false, nil
			}
			return nil, false, err
		}
		eq, err := pyEq(v, sentinel)
		if err != nil {
			return nil, false, err
		}
		if eq {
			done = true
			return nil, false, nil
		}
		return v, true, nil
	})}, nil
}

// builtinsDoc is builtins.__doc__.
const builtinsDoc = "Built-in functions, types, exceptions, and other objects.\n\nThis module provides direct " +
	"access to all 'built-in'\nidentifiers of Python; for example, builtins.len is\nthe full name for the " +
	"built-in function len().\n\nThis module is not normally accessed explicitly by most\napplications, but " +
	"can be useful in modules that provide\nobjects with the same name as a built-in value, but in\nwhich the " +
	"built-in of that name is also needed."

// sitePrinters are the reprs of the builtins the site module adds.
var sitePrinters = map[string]string{
	"exit": "Use exit() or Ctrl-D (i.e. EOF) to exit",
	"quit": "Use quit() or Ctrl-D (i.e. EOF) to exit",
	"help": "Type help() for interactive help, or help(object) for help about object.",
	"copyright": "Copyright (c) 2001-2023 Python Software Foundation.\nAll Rights Reserved.\n\nCopyright (c) 2000 " +
		"BeOpen.com.\nAll Rights Reserved.\n\nCopyright (c) 1995-2001 Corporation for National Research " +
		"Initiatives.\nAll Rights Reserved.\n\nCopyright (c) 1991-1995 Stichting Mathematisch Centrum, " +
		"Amsterdam.\nAll Rights Reserved.",
	"credits": "    Thanks to CWI, CNRI, BeOpen, Zope Corporation, the Python Software\n    Foundation, and a cast " +
		"of thousands for supporting Python\n    development.  See www.python.org for more information.",
	"license": "Type license() to see the full license text",
}

// notImplementedV is the NotImplemented singleton.
type notImplementedV struct{}

func (notImplementedV) pyType() string                  { return "NotImplementedType" }
func (notImplementedV) repr(*reprState) (string, error) { return "NotImplemented", nil }

// makeIterObject is iter(v) as a value.
func makeIterObject(v Value) (Value, error) {
	if _, ok := v.(*iterObj); ok {
		return v, nil
	}
	it, err := pyIter(v)
	if err != nil {
		return nil, err
	}
	typ := map[string]string{"list": "list_iterator", "tuple": "tuple_iterator", "str": "str_ascii_iterator",
		"dict": "dict_keyiterator", "range": "range_iterator", "bytes": "bytes_iterator", "set": "set_iterator",
		"frozenset": "set_iterator", "dict_keys": "dict_keyiterator", "dict_values": "dict_valueiterator",
		"dict_items": "dict_itemiterator"}[v.pyType()]
	if typ == "" {
		typ = "iterator"
	}
	if typ == "str_ascii_iterator" {
		if s, _ := v.(strV); !textutil.IsASCII(string(s)) || false {
			typ = "str_iterator"
		}
	}
	return &iterObj{typ: typ, it: it}, nil
}

// compareOp is one operator of a comparison.
func compareOp(op string, a, b Value) (Value, error) {
	switch op {
	case "Is":
		return pyBool(sameObject(a, b)), nil
	case "IsNot":
		return pyBool(!sameObject(a, b)), nil
	case "In", "NotIn":
		in, err := pyContains(b, a)
		if err != nil {
			return nil, err
		}
		return pyBool(in == (op == "In")), nil
	}
	r, err := pyCompare(comparisonOperators[op], a, b)
	return pyBool(r), err
}

func isNoneV(v Value) bool {
	_, ok := v.(noneV)
	return ok
}

// comparisonOperators maps a comparison's ast class name to its symbol.
var comparisonOperators = map[string]string{
	"Eq": "==", "NotEq": "!=", "Lt": "<", "LtE": "<=", "Gt": ">", "GtE": ">=", "Is": "is",
	"IsNot": "is not", "In": "in", "NotIn": "not in",
}

// refusedBuiltin is the body of a builtin this library keeps only so that an
// evaluator can refuse it by identity: calling it raises RuntimeError naming
// the builtin and why it is refused.
func refusedBuiltin(name, why string) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		return nil, newPyErr("RuntimeError", name+"() is refused: "+why)
	}
}

// builtinPrint is print(*objects, sep=' ', end='\n', file=None, flush=False):
// it writes str() of each object, separated by sep and followed by end, to
// standard output, or passes each piece to file.write when a file is given.
func builtinPrint(args []Value, kw *dictV) (Value, error) {
	sep, end := " ", "\n"
	file := none
	if kw != nil {
		for i, k := range kw.keys {
			ks, _ := k.(strV)
			v := kw.vals[i]
			switch ks {
			case "sep", "end":
				switch t := v.(type) {
				case noneV:
				case strV:
					if ks == "sep" {
						sep = string(t)
					} else {
						end = string(t)
					}
				default:
					return nil, typeErr("%s must be None or a string, not %s", ks, v.pyType())
				}
			case "file":
				file = v
			case "flush":
				if _, err := truthy(v); err != nil {
					return nil, err
				}
			default:
				return nil, typeErr("'%s' is an invalid keyword argument for print()", ks)
			}
		}
	}
	pieces := make([]string, 0, 2*len(args)+1)
	for i, a := range args {
		if i > 0 {
			pieces = append(pieces, sep)
		}
		s, err := pyStr(a)
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, s)
	}
	pieces = append(pieces, end)
	if _, ok := file.(noneV); ok {
		if _, err := os.Stdout.WriteString(strings.Join(pieces, "")); err != nil {
			return nil, newPyErr("OSError", err.Error())
		}
		return none, nil
	}
	write, err := pyGetattr(file, "write")
	if err != nil {
		return nil, err
	}
	for _, piece := range pieces {
		if _, err := callValue(write, []Value{strV(piece)}, nil); err != nil {
			return nil, err
		}
	}
	return none, nil
}
