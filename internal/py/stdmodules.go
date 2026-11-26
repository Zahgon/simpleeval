// Ported from CPython 3.12 (Modules/_operator.c, Lib/operator.py, Lib/os.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The operator module, and the part of the os module an expression
// evaluator meets: the process functions it refuses, and the path
// constants.

package py

import "strings"

func init() {
	registerModule("operator", operatorAttrs)
	registerModule("os", osAttrs)
	registerModule("posixpath", osPathAttrs)
}

// operatorAttrs are the operator module's functions and classes.
func operatorAttrs() []moduleAttr {
	binary := map[string]string{"add": "Add", "sub": "Sub", "mul": "Mult", "truediv": "Div",
		"floordiv": "FloorDiv", "mod": "Mod", "pow": "Pow", "lshift": "LShift", "rshift": "RShift",
		"and_": "BitAnd", "or_": "BitOr", "xor": "BitXor", "matmul": "MatMult"}
	compare := map[string]string{"eq": "Eq", "ne": "NotEq", "lt": "Lt", "le": "LtE", "gt": "Gt", "ge": "GtE",
		"is_": "Is", "is_not": "IsNot"}
	unary := map[string]string{"neg": "USub", "pos": "UAdd", "invert": "Invert", "inv": "Invert", "not_": "Not"}
	fns := map[string]func(args []Value, kw *dictV) (Value, error){
		"truth": func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("truth", args, kw); err != nil {
				return nil, err
			}
			t, err := truthy(args[0])
			return pyBool(t), err
		},
		"abs": oneArg("abs", builtinAbs),
		"index": oneArg("index", func(v Value) (Value, error) {
			n, err := methodsIndex(v)
			if err != nil {
				return nil, err
			}
			return mkBig(n), nil
		}),
		"contains": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("contains", 2, 2, args, kw); err != nil {
				return nil, err
			}
			in, err := pyContains(args[0], args[1])
			return pyBool(in), err
		},
		"getitem": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("getitem", 2, 2, args, kw); err != nil {
				return nil, err
			}
			return pyGetitem(args[0], args[1])
		},
		"concat": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("concat", 2, 2, args, kw); err != nil {
				return nil, err
			}
			if !hasGetitem(args[0]) {
				return nil, typeErr("'%s' object can't be concatenated", typeName(args[0]))
			}
			return binop("Add", args[0], args[1])
		},
		"countOf": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("countOf", 2, 2, args, kw); err != nil {
				return nil, err
			}
			items, err := toSlice(args[0])
			if err != nil {
				return nil, err
			}
			return methodsSeqCount(items, args[1])
		},
		"indexOf": func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("indexOf", 2, 2, args, kw); err != nil {
				return nil, err
			}
			items, err := toSlice(args[0])
			if err != nil {
				return nil, err
			}
			for i, it := range items {
				eq := sameObject(it, args[1])
				if !eq {
					if eq, err = pyEq(it, args[1]); err != nil {
						return nil, err
					}
				}
				if eq {
					return mkInt(int64(i)), nil
				}
			}
			return nil, valueErr("sequence.index(x): x not in sequence")
		},
		"call": func(args []Value, kw *dictV) (Value, error) {
			if len(args) == 0 {
				return nil, typeErr("call() missing 1 required positional argument: 'obj'")
			}
			return callValue(args[0], args[1:], kw)
		},
	}
	for name, op := range binary {
		op := op
		fns[name] = func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 2, 2, args, kw); err != nil {
				return nil, err
			}
			return binop(op, args[0], args[1])
		}
	}
	for name, op := range compare {
		name, op := name, op
		fns[name] = func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly(strings.TrimSuffix(name, "_"), 2, 2, args, kw); err != nil {
				return nil, err
			}
			return compareOp(op, args[0], args[1])
		}
	}
	for name, op := range unary {
		name, op := name, op
		fns[name] = func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(strings.TrimSuffix(name, "_"), args, kw); err != nil {
				return nil, err
			}
			return unop(op, args[0])
		}
	}
	out := make([]moduleAttr, 0, len(fns)+3)
	for _, name := range operatorNames {
		switch name {
		case "attrgetter", "itemgetter", "methodcaller":
			out = append(out, moduleAttr{name, operatorClass(name)})
		default:
			out = append(out, moduleAttr{name, newBuiltin(name, fns[name])})
		}
	}
	return out
}

// operatorNames are the operator module's public names this port provides,
// in dir() order.
var operatorNames = []string{"abs", "add", "and_", "attrgetter", "call", "concat", "contains", "countOf", "eq",
	"floordiv", "ge", "getitem", "gt", "index", "indexOf", "inv", "invert", "is_", "is_not", "itemgetter", "le",
	"lshift", "lt", "matmul", "methodcaller", "mod", "mul", "ne", "neg", "not_", "or_", "pos", "pow", "rshift",
	"sub", "truediv", "truth", "xor"}

// operatorClass is operator.attrgetter, operator.itemgetter or
// operator.methodcaller: a class whose instances are callables fetching
// attributes, fetching items, or calling a method.
func operatorClass(name string) *classV {
	k := defineClass("operator", name, "")
	k.construct = func(args []Value, kw *dictV) (Value, error) {
		switch name {
		case "attrgetter", "itemgetter":
			if err := noKw(name, kw); err != nil {
				return nil, err
			}
			if len(args) == 0 {
				return nil, typeErr("%s expected 1 argument, got 0", name)
			}
			keys := append([]Value(nil), args...)
			if name == "attrgetter" {
				for _, a := range keys {
					if _, ok := a.(strV); !ok {
						return nil, typeErr("attribute name must be a string")
					}
				}
			}
			return operatorGetter(name, keys), nil
		}
		if len(args) == 0 {
			return nil, typeErr("methodcaller needs at least one argument, the method name")
		}
		method, ok := args[0].(strV)
		if !ok {
			return nil, typeErr("method name must be a string")
		}
		rest := append([]Value(nil), args[1:]...)
		return &Object{Module: "operator", Class: name, Call: func(cargs []Value, _ []Keyword) (Value, error) {
			if len(cargs) != 1 {
				return nil, typeErr("methodcaller expected 1 argument, got %d", len(cargs))
			}
			m, err := pyGetattr(cargs[0], string(method))
			if err != nil {
				return nil, err
			}
			return callValue(m, rest, kw)
		}}, nil
	}
	return k
}

// operatorGetter is an attrgetter or itemgetter instance for the keys.
func operatorGetter(name string, keys []Value) Value {
	fetch := func(obj, key Value) (Value, error) {
		if name == "itemgetter" {
			return pyGetitem(obj, key)
		}
		v := obj
		for _, part := range strings.Split(string(key.(strV)), ".") {
			var err error
			if v, err = pyGetattr(v, part); err != nil {
				return nil, err
			}
		}
		return v, nil
	}
	return &Object{Module: "operator", Class: name, Call: func(args []Value, _ []Keyword) (Value, error) {
		if len(args) != 1 {
			return nil, typeErr("%s expected 1 argument, got %d", name, len(args))
		}
		if len(keys) == 1 {
			return fetch(args[0], keys[0])
		}
		out := make([]Value, len(keys))
		for i, k := range keys {
			v, err := fetch(args[0], k)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return newTuple(out), nil
	}}
}

// osAttrs are the attributes of the os module this port provides: the
// functions that run other programs, as values only (an expression
// evaluator refuses them), and the path constants of a POSIX system.
func osAttrs() []moduleAttr {
	out := []moduleAttr{{"name", strV("posix")}, {"path", module("posixpath")}}
	for _, a := range osPathAttrs() {
		switch a.name {
		case "sep", "altsep", "curdir", "pardir", "extsep", "pathsep", "defpath", "devnull":
			out = append(out, a)
		}
	}
	out = append(out, moduleAttr{"linesep", strV("\n")})
	// posixmodule.c defines system, execv, execve, posix_spawn and
	// posix_spawnp; os.py defines popen and the other exec and spawn
	// functions in Python.
	for _, name := range []string{"system", "execv", "execve", "posix_spawn", "posix_spawnp"} {
		out = append(out, moduleAttr{name, newBuiltin(name, refusedBuiltin(name, "this library does not start other programs"))})
	}
	for _, name := range []string{"popen", "execl", "execle", "execlp", "execlpe", "execvp", "execvpe", "spawnl",
		"spawnle", "spawnlp", "spawnlpe", "spawnv", "spawnve", "spawnvp", "spawnvpe"} {
		out = append(out, moduleAttr{name, &callable{name: name, typ: "function", qual: name,
			fn: refusedBuiltin(name, "this library does not start other programs")}})
	}
	return out
}

// osPathAttrs are the constants of posixpath, the os.path of a POSIX system.
func osPathAttrs() []moduleAttr {
	return []moduleAttr{{"sep", strV("/")}, {"altsep", none}, {"curdir", strV(".")}, {"pardir", strV("..")},
		{"extsep", strV(".")}, {"pathsep", strV(":")}, {"defpath", strV("/bin:/usr/bin")},
		{"devnull", strV("/dev/null")}}
}
