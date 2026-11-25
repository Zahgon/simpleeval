// Ported from CPython 3.12 (Objects/genericaliasobject.c, Objects/unionobject.c, Objects/typeobject.c) to
// Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The values typing syntax makes of builtin classes (list[int], int | None)
// and the special methods of builtin values reached as attributes.

package py

import (
	"strings"

	"github.com/danthedeckie/simpleeval/internal/pyexc"
)

// genericClasses are the builtin classes that can be subscripted.
var genericClasses = map[string]bool{"list": true, "dict": true, "set": true, "frozenset": true, "tuple": true,
	"type": true, "enumerate": true, "BaseExceptionGroup": true, "ExceptionGroup": true}

// genericAliasV is a types.GenericAlias: origin[args].
type genericAliasV struct {
	origin *classV
	args   []Value
	// unpacked marks the *alias iterating an alias gives.
	unpacked bool
}

func (*genericAliasV) pyType() string { return "types.GenericAlias" }

var genericAliasClass = &classV{name: "GenericAlias", module: "types", base: objectClass}
var unionClass = &classV{name: "UnionType", module: "types", base: objectClass}

// classGetitem is cls[key] for a builtin class.
func classGetitem(k *classV, key Value) (Value, error) {
	if !genericClasses[k.name] {
		return nil, typeErr("type '%s' is not subscriptable", k.name)
	}
	args := []Value{key}
	if t, ok := key.(*tupleV); ok {
		args = append([]Value(nil), t.items...)
	}
	return &genericAliasV{origin: k, args: args}, nil
}

// typeReprItem is ga_repr_item: the name of a class (with its module unless
// it is builtins), ... for Ellipsis, repr() of anything else.
func typeReprItem(v Value, st *reprState) (string, error) {
	switch x := v.(type) {
	case ellipsisV:
		return "...", nil
	case *genericAliasV, *unionV:
		return reprWith(v, st)
	case *classV:
		if mod := x.moduleName(); mod != "builtins" {
			return mod + "." + x.name, nil
		}
		return x.name, nil
	case *callable:
		if x.self == nil && x.typ == "builtin_function_or_method" {
			return x.name, nil
		}
	}
	return reprWith(v, st)
}

func (g *genericAliasV) repr(st *reprState) (string, error) {
	head, err := typeReprItem(g.origin, st)
	if err != nil {
		return "", err
	}
	if g.unpacked {
		head = "*" + head
	}
	if len(g.args) == 0 {
		return head + "[()]", nil
	}
	parts := make([]string, len(g.args))
	for i, a := range g.args {
		if parts[i], err = typeReprItem(a, st); err != nil {
			return "", err
		}
	}
	return head + "[" + strings.Join(parts, ", ") + "]", nil
}

func (g *genericAliasV) getAttr(name string) (Value, error) {
	switch name {
	case "__origin__":
		return g.origin, nil
	case "__args__":
		return newTuple(append([]Value(nil), g.args...)), nil
	case "__parameters__":
		return newTuple(nil), nil
	case "__class__":
		return genericAliasClass, nil
	}
	return pyGetattr(g.origin, name)
}

func (g *genericAliasV) iter() (iterator, error) {
	return &sliceIter{items: []Value{&genericAliasV{origin: g.origin, args: g.args, unpacked: true}}}, nil
}

func (g *genericAliasV) pyCall(args []Value, kw *dictV) (Value, error) {
	return callValue(g.origin, args, kw)
}

func (g *genericAliasV) getItem(Value) (Value, error) {
	s, err := g.repr(&reprState{})
	if err != nil {
		return nil, err
	}
	return nil, typeErr("%s is not a generic class", s)
}

func (g *genericAliasV) eq(other Value) (bool, error) {
	o, ok := other.(*genericAliasV)
	if !ok || o.origin != g.origin {
		return false, nil
	}
	return seqEq(g.args, o.args)
}

// unionV is a types.UnionType: a | b.
type unionV struct {
	args []Value
}

func (*unionV) pyType() string { return "types.UnionType" }

func (u *unionV) repr(st *reprState) (string, error) {
	parts := make([]string, len(u.args))
	for i, a := range u.args {
		if isNoneV(a) {
			parts[i] = "None"
			continue
		}
		s, err := typeReprItem(a, st)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, " | "), nil
}

func (u *unionV) getAttr(name string) (Value, error) {
	switch name {
	case "__args__":
		return newTuple(append([]Value(nil), u.args...)), nil
	case "__class__":
		return unionClass, nil
	}
	return nil, nil
}

func (u *unionV) getItem(Value) (Value, error) {
	s, err := u.repr(&reprState{})
	if err != nil {
		return nil, err
	}
	return nil, typeErr("%s is not a generic class", s)
}

func (u *unionV) eq(other Value) (bool, error) {
	o, ok := other.(*unionV)
	if !ok || len(o.args) != len(u.args) {
		return false, nil
	}
	for _, a := range u.args {
		found := false
		for _, b := range o.args {
			if eq, err := pyEq(a, b); err != nil {
				return false, err
			} else if eq {
				found = true
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// isUnionable reports whether v can be an operand of a type union.
func isUnionable(v Value) bool {
	switch v.(type) {
	case noneV, *classV, *genericAliasV, *unionV:
		return true
	}
	return false
}

// typeUnion is a | b for classes, None, generic aliases and unions; ok is
// false when an operand cannot take part.
func typeUnion(a, b Value) (Value, bool, error) {
	if !isUnionable(a) || !isUnionable(b) {
		return nil, false, nil
	}
	var args []Value
	add := func(v Value) error {
		if u, ok := v.(*unionV); ok {
			for _, x := range u.args {
				if err := addUnique(&args, x); err != nil {
					return err
				}
			}
			return nil
		}
		return addUnique(&args, v)
	}
	if err := add(a); err != nil {
		return nil, true, err
	}
	if err := add(b); err != nil {
		return nil, true, err
	}
	if len(args) == 1 {
		return args[0], true, nil
	}
	return &unionV{args: args}, true, nil
}

func addUnique(args *[]Value, v Value) error {
	for _, x := range *args {
		if sameObject(x, v) {
			return nil
		}
		if eq, err := pyEq(x, v); err != nil {
			return err
		} else if eq {
			return nil
		}
	}
	*args = append(*args, v)
	return nil
}

// ---------------------------------------------------------------- special methods of builtin values

// slotWrapperNames are the special methods reached as attributes of builtin
// values, with the operation each one performs.
var slotWrapperNames = map[string]bool{"__len__": true, "__iter__": true, "__repr__": true, "__str__": true,
	"__hash__": true, "__eq__": true, "__ne__": true, "__lt__": true, "__le__": true, "__gt__": true,
	"__ge__": true, "__contains__": true, "__add__": true, "__mul__": true, "__mod__": true, "__bool__": true,
	"__getitem__": true, "__setitem__": true, "__delitem__": true}

// hasSlot reports whether the builtin value v's type defines the special
// method name.
func hasSlot(v Value, name string) bool {
	switch name {
	case "__repr__", "__str__", "__hash__", "__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__":
		return true
	case "__len__", "__iter__", "__contains__":
		switch v.(type) {
		case strV, bytesV, *bytearrayV, *listV, *tupleV, *dictV, *setV, rangeV, *dictViewV:
			return true
		}
	case "__getitem__":
		switch v.(type) {
		case strV, bytesV, *bytearrayV, *listV, *tupleV, *dictV, rangeV:
			return true
		}
	case "__setitem__", "__delitem__":
		switch v.(type) {
		case *bytearrayV, *listV, *dictV:
			return true
		}
	case "__add__", "__mul__":
		switch v.(type) {
		case strV, bytesV, *bytearrayV, *listV, *tupleV, intV, boolV, floatV, complexV:
			return true
		}
	case "__mod__":
		switch v.(type) {
		case strV, bytesV, *bytearrayV, intV, boolV, floatV:
			return true
		}
	case "__bool__":
		switch v.(type) {
		case intV, boolV, floatV, complexV, noneV:
			return true
		}
	}
	return false
}

// slotWrapper is v.name for a special method of a builtin value: a
// method-wrapper that performs the operation.
func slotWrapper(v Value, name string) (Value, bool) {
	if !slotWrapperNames[name] || !hasSlot(v, name) {
		return nil, false
	}
	nargs := 1
	switch name {
	case "__len__", "__iter__", "__repr__", "__str__", "__hash__", "__bool__":
		nargs = 0
	case "__setitem__":
		nargs = 2
	}
	return &callable{name: name, typ: "method-wrapper", self: v,
		fn: func(args []Value, kw *dictV) (Value, error) {
			if kw != nil && kw.Len() > 0 {
				return nil, typeErr("wrapper %s() takes no keyword arguments", name)
			}
			if len(args) != nargs {
				return nil, typeErr("expected %d argument%s, got %d", nargs, pyexc.Plural(nargs), len(args))
			}
			switch name {
			case "__len__":
				n, err := pyLen(v)
				return mkInt(int64(n)), err
			case "__iter__":
				return makeIterObject(v)
			case "__repr__":
				s, err := pyRepr(v)
				return strV(s), err
			case "__str__":
				s, err := pyStr(v)
				return strV(s), err
			case "__hash__":
				h, err := pyHash(v)
				return mkInt(h), err
			case "__bool__":
				t, err := truthy(v)
				return pyBool(t), err
			case "__contains__":
				in, err := pyContains(v, args[0])
				return pyBool(in), err
			case "__getitem__":
				return pyGetitem(v, args[0])
			case "__setitem__":
				return none, pySetitem(v, args[0], args[1])
			case "__delitem__":
				return none, pyDelitem(v, args[0])
			}
			if op, ok := map[string]string{"__eq__": "==", "__ne__": "!=", "__lt__": "<", "__le__": "<=",
				"__gt__": ">", "__ge__": ">="}[name]; ok {
				return builtinRich(op, v, args[0])
			}
			op := map[string]string{"__add__": "Add", "__mul__": "Mult", "__mod__": "Mod"}[name]
			r, err := builtinBinop(op, v, args[0])
			if err != nil && isExc(err, "TypeError") {
				return notImplementedV{}, nil
			}
			return r, err
		}}, true
}

// builtinRich is a builtin type's rich comparison slot called directly
// (x.__lt__(y)): NotImplemented where the operator raises TypeError.
func builtinRich(op string, a, b Value) (Value, error) {
	if op == "==" || op == "!=" {
		eq, err := pyEq(a, b)
		if err != nil {
			return nil, err
		}
		return pyBool(eq == (op == "==")), nil
	}
	r, err := pyCompare(op, a, b)
	if err != nil {
		if isExc(err, "TypeError") {
			return notImplementedV{}, nil
		}
		return nil, err
	}
	return pyBool(r), nil
}
