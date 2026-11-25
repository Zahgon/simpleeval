// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The attributes of sets, bytes, exceptions, slices, dicts, and the unbound methods of the
// builtin classes.

package py

import (
	"encoding/hex"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/codecs"
)

// excMethod is an exception's attribute other than args: one set on the
// instance, or one BaseException defines.
func excMethod(x *excV, name string) (Value, bool) {
	if x.attrs != nil {
		if v, ok := x.attrs.getStr(name); ok {
			return v, true
		}
	}
	return excBuiltinAttr(x, name)
}

// dictMethod is an attribute of a dict.
func dictMethod(obj Value, d *dictV, name string, isGroup bool) (Value, bool) {
	if m, ok := methodsDict[name]; ok {
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) { return m(d, args, kw) }), true
	}
	return slotWrapper(obj, name)
}

// setMethod is an attribute of a set or frozenset.
func setMethod(s *setV, name string) (Value, bool) {
	typ := s.pyType()
	bound := func(fn func(args []Value, kw *dictV) (Value, error)) (Value, bool) {
		return methodsBound(s, name, fn), true
	}
	switch name {
	case "copy":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+".copy", args, kw); err != nil {
				return nil, err
			}
			if s.frozen {
				return s, nil
			}
			return setCopy(s, false), nil
		})
	case "union", "intersection", "difference":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw(typ+"."+name, kw); err != nil {
				return nil, err
			}
			switch name {
			case "union":
				return setUnionMulti(s, args)
			case "intersection":
				return setIntersectionMulti(s, args)
			}
			return setDifferenceMulti(s, args)
		})
	case "symmetric_difference":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(typ+".symmetric_difference", args, kw); err != nil {
				return nil, err
			}
			return setSymmetricDifference(s, args[0])
		})
	case "isdisjoint", "issubset", "issuperset":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(typ+"."+name, args, kw); err != nil {
				return nil, err
			}
			switch name {
			case "isdisjoint":
				r, err := s.isDisjoint(args[0])
				return pyBool(r), err
			case "issuperset":
				r, err := s.isSuperset(args[0])
				return pyBool(r), err
			}
			o, err := toSet(args[0])
			if err != nil {
				return nil, err
			}
			r, err := s.subsetOf(o)
			return pyBool(r), err
		})
	}
	if s.frozen {
		return slotWrapper(s, name)
	}
	switch name {
	case "add", "remove", "discard":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("set."+name, args, kw); err != nil {
				return nil, err
			}
			if name == "add" {
				return none, s.add(args[0])
			}
			found, err := s.discard(args[0])
			if err != nil {
				return nil, err
			}
			if !found && name == "remove" {
				return nil, newExc("KeyError", []Value{args[0]})
			}
			return none, nil
		})
	case "pop":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("set.pop", args, kw); err != nil {
				return nil, err
			}
			return s.pop()
		})
	case "clear":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("set.clear", args, kw); err != nil {
				return nil, err
			}
			s.clear()
			return none, nil
		})
	case "update":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("set.update", kw); err != nil {
				return nil, err
			}
			for _, a := range args {
				if err := s.update(a); err != nil {
					return nil, err
				}
			}
			return none, nil
		})
	case "intersection_update", "difference_update":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("set."+name, kw); err != nil {
				return nil, err
			}
			if name == "difference_update" {
				for _, o := range args {
					if err := s.differenceUpdate(o); err != nil {
						return nil, err
					}
				}
				return none, nil
			}
			r, err := setIntersectionMulti(s, args)
			if err != nil {
				return nil, err
			}
			if r != s {
				s.table, s.fill, s.used, s.finger = r.table, r.fill, r.used, r.finger
			}
			return none, nil
		})
	case "symmetric_difference_update":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("set.symmetric_difference_update", args, kw); err != nil {
				return nil, err
			}
			return none, s.symmetricDifferenceUpdate(args[0])
		})
	}
	return slotWrapper(s, name)
}

func sliceIndicesMethod(s *sliceV) Value {
	return methodsBound(s, "indices", func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("slice.indices", args, kw); err != nil {
			return nil, err
		}
		n, err := methodsIndex(args[0])
		if err != nil {
			return nil, err
		}
		if n.Sign() < 0 {
			return nil, valueErr("length should not be negative")
		}
		start, stop, step, err := pySliceIndices(int(n.Int64()), s.start, s.stop, s.step)
		if err != nil {
			return nil, err
		}
		return newTuple([]Value{mkInt(int64(start)), mkInt(int64(stop)), mkInt(int64(step))}), nil
	})
}

// bytesMethod is an attribute of bytes or bytearray.
func bytesMethod(obj Value, name string) (Value, bool) {
	b, _ := bytesOf(obj)
	typ := obj.pyType()
	wrap := func(s string) Value {
		if typ == "bytearray" {
			return &bytearrayV{b: []byte(s)}
		}
		return bytesV(s)
	}
	switch name {
	case "decode":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("decode", []string{"encoding", "errors"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			return decodeBytes(b, buf[0], buf[1])
		}), true
	case "hex":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+".hex", args, kw); err != nil {
				return nil, err
			}
			return strV(hex.EncodeToString([]byte(b))), nil
		}), true
	case "upper", "lower":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+"."+name, args, kw); err != nil {
				return nil, err
			}
			out := []byte(b)
			for i, c := range out {
				if name == "upper" && c >= 'a' && c <= 'z' {
					out[i] = c - 32
				} else if name == "lower" && c >= 'A' && c <= 'Z' {
					out[i] = c + 32
				}
			}
			return wrap(string(out)), nil
		}), true
	case "startswith", "endswith", "count", "find":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos(typ+"."+name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			sub, ok := bytesOf(args[0])
			if !ok {
				return nil, typeErr("a bytes-like object is required, not '%s'", args[0].pyType())
			}
			switch name {
			case "startswith":
				return pyBool(strings.HasPrefix(b, sub)), nil
			case "endswith":
				return pyBool(strings.HasSuffix(b, sub)), nil
			case "count":
				if sub == "" {
					return mkInt(int64(len(b) + 1)), nil
				}
				return mkInt(int64(strings.Count(b, sub))), nil
			}
			return mkInt(int64(strings.Index(b, sub))), nil
		}), true
	case "join":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(typ+".join", args, kw); err != nil {
				return nil, err
			}
			items, err := toSliceAs(args[0], typeErr("can only join an iterable"))
			if err != nil {
				return nil, err
			}
			parts := make([]string, len(items))
			for i, it := range items {
				p, ok := bytesOf(it)
				if !ok {
					return nil, typeErr("sequence item %d: expected a bytes-like object, %s found", i, typeName(it))
				}
				parts[i] = p
			}
			return wrap(strings.Join(parts, b)), nil
		}), true
	case "strip", "lstrip", "rstrip", "split", "replace":
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
			bytesArg := func(v Value) (string, error) {
				p, ok := bytesOf(v)
				if !ok {
					return "", typeErr("a bytes-like object is required, not '%s'", typeName(v))
				}
				return p, nil
			}
			switch name {
			case "replace":
				if err := methodsArgsPos(typ+".replace", 2, 2, args, kw); err != nil {
					return nil, err
				}
				old, err := bytesArg(args[0])
				if err != nil {
					return nil, err
				}
				repl, err := bytesArg(args[1])
				if err != nil {
					return nil, err
				}
				return wrap(strings.ReplaceAll(b, old, repl)), nil
			case "split":
				if err := methodsArgsPos(typ+".split", 0, 1, args, kw); err != nil {
					return nil, err
				}
				var parts []string
				if len(args) == 0 || isNoneV(args[0]) {
					parts = strings.FieldsFunc(b, func(r rune) bool { return strings.ContainsRune(asciiSpace, r) })
				} else {
					sep, err := bytesArg(args[0])
					if err != nil {
						return nil, err
					}
					if sep == "" {
						return nil, valueErr("empty separator")
					}
					parts = strings.Split(b, sep)
				}
				items := make([]Value, len(parts))
				for i, p := range parts {
					items[i] = wrap(p)
				}
				return newList(items), nil
			}
			if err := methodsArgsPos(typ+"."+name, 0, 1, args, kw); err != nil {
				return nil, err
			}
			cut := asciiSpace
			if len(args) == 1 && !isNoneV(args[0]) {
				var err error
				if cut, err = bytesArg(args[0]); err != nil {
					return nil, err
				}
			}
			switch name {
			case "lstrip":
				return wrap(strings.TrimLeft(b, cut)), nil
			case "rstrip":
				return wrap(strings.TrimRight(b, cut)), nil
			}
			return wrap(strings.Trim(b, cut)), nil
		}), true
	}
	if m, ok := bytesViaStr(obj, b, name, wrap); ok {
		return m, true
	}
	return slotWrapper(obj, name)
}

// unlatin1 is the inverse of codecs.DecodeLatin1.
func unlatin1(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return string(out)
}

// bytesStrMethods are the bytes methods whose result is the str method's
// on the bytes read as latin-1 code points: they only compare and move
// bytes around.
var bytesStrMethods = map[string]bool{"index": true, "rindex": true, "rfind": true, "partition": true,
	"rpartition": true, "center": true, "ljust": true, "rjust": true, "zfill": true, "removeprefix": true,
	"removesuffix": true, "expandtabs": true}

// bytesViaStr runs a bytes method through the str method of the same
// name, converting bytes arguments to str and str results back to bytes.
func bytesViaStr(obj Value, b, name string, wrap func(string) Value) (Value, bool) {
	m, ok := methodsStr[name]
	if !ok || !bytesStrMethods[name] {
		return nil, false
	}
	typ := obj.pyType()
	return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) {
		conv := make([]Value, len(args))
		for i, a := range args {
			switch x := a.(type) {
			case bytesV, *bytearrayV:
				s, _ := bytesOf(x)
				conv[i] = strV(codecs.DecodeLatin1([]byte(s)))
			case strV:
				if name == "center" || name == "ljust" || name == "rjust" {
					return nil, typeErr("%s() argument 2 must be a byte string of length 1, not str", name)
				}
				return nil, typeErr("a bytes-like object is required, not '%s'", typeName(a))
			default:
				conv[i] = a
			}
		}
		r, err := m(typ, codecs.DecodeLatin1([]byte(b)), conv, kw)
		if err != nil {
			if e, ok := err.(*excV); ok && e.Message == "substring not found" {
				return nil, valueErr("subsection not found")
			}
			return nil, err
		}
		var back func(Value) Value
		back = func(v Value) Value {
			switch x := v.(type) {
			case strV:
				return wrap(unlatin1(string(x)))
			case *tupleV:
				items := make([]Value, len(x.items))
				for i, it := range x.items {
					items[i] = back(it)
				}
				return newTuple(items)
			}
			return v
		}
		return back(r), nil
	}), true
}

// asciiSpace is the whitespace of bytes methods.
const asciiSpace = " \t\n\r\x0b\x0c"

// classAttr is an attribute of a builtin class: its methods, unbound.
func classAttr(k *classV, name string) (Value, bool) {
	if m, ok := classMethodOf(k, name); ok {
		return m, true
	}
	sample := map[string]Value{"str": strV(""), "list": newList(nil), "dict": newDict(), "tuple": newTuple(nil),
		"set": newSet(false), "frozenset": newSet(true), "int": mkInt(0), "float": floatV(0), "bytes": bytesV(""),
		"bool": falseV, "range": rangeV{stop: 0, step: 1}, "slice": &sliceV{start: none, stop: none, step: none},
		"bytearray": &bytearrayV{}}[k.name]
	if sample == nil {
		return nil, false
	}
	if _, ok := attrOf(sample, name); !ok {
		return nil, false
	}
	if _, isCall := mustAttr(sample, name).(*callable); !isCall {
		return &dataDescV{name: name, owner: k.name}, true
	}
	desc := &callable{name: name, typ: "method_descriptor", qual: k.name + "." + name}
	desc.fn = func(args []Value, kw *dictV) (Value, error) {
		if len(args) == 0 {
			return nil, typeErr("unbound method %s.%s() needs an argument", k.name, name)
		}
		if !classOf(args[0]).subclassOf(k) {
			return nil, typeErr("descriptor '%s' for '%s' objects doesn't apply to a '%s' object", name, k.name, args[0].pyType())
		}
		m, ok := attrOf(args[0], name)
		if !ok {
			return nil, newPyErr("AttributeError", name)
		}
		return callValue(m, args[1:], kw)
	}
	return &methodDescV{desc}, true
}

// dictFromkeys is the classmethod dict.fromkeys bound to cls, dict or a
// subclass of it: cls() with each key set to the value (None by default).
func dictFromkeys(cls *classV) Value {
	return &callable{name: "fromkeys", typ: "builtin_function_or_method", self: cls,
		fn: func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("fromkeys", 1, 2, args, kw); err != nil {
				return nil, err
			}
			var val Value = none
			if len(args) == 2 {
				val = args[1]
			}
			keys, err := toSlice(args[0])
			if err != nil {
				return nil, err
			}
			d := newDict()
			for _, key := range keys {
				if err := pySetitem(d, key, val); err != nil {
					return nil, err
				}
			}
			return d, nil
		}}
}

func mustAttr(v Value, name string) Value {
	a, _ := attrOf(v, name)
	return a
}

// dataDescV is a data attribute of a builtin class, as the class holds it:
// a getset or member descriptor.
type dataDescV struct {
	name, owner string
}

func (d *dataDescV) pyType() string {
	if d.owner == "range" || d.owner == "slice" {
		return "member_descriptor"
	}
	return "getset_descriptor"
}

func (d *dataDescV) repr(*reprState) (string, error) {
	if d.pyType() == "member_descriptor" {
		return "<member '" + d.name + "' of '" + d.owner + "' objects>", nil
	}
	return "<attribute '" + d.name + "' of '" + d.owner + "' objects>", nil
}

// methodDescV is an unbound method of a builtin class.
type methodDescV struct{ *callable }

func (m *methodDescV) repr(*reprState) (string, error) {
	i := strings.IndexByte(m.qual, '.')
	if slotWrapperNames[m.name] {
		return "<slot wrapper '" + m.name + "' of '" + m.qual[:i] + "' objects>", nil
	}
	return "<method '" + m.name + "' of '" + m.qual[:i] + "' objects>", nil
}
