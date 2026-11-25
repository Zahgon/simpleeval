// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The attributes and methods of the builtin values, reached through x.name, with
// CPython 3.12's argument handling and error messages.

package py

import (
	"math"
	"math/big"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/pyexc"
	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// builtinAttr is getattr(v, name) for the builtin value types: a bound
// method (a *callable with typ "builtin_function_or_method", name = the
// method name, self = v), a data attribute, or one of the special methods
// slotWrapper provides. ok=false means Python raises AttributeError for it.
func builtinAttr(v Value, name string) (Value, bool) {
	switch x := v.(type) {
	case strV:
		if name == "maketrans" {
			return strMaketrans(), true
		}
		if m, ok := methodsStr[name]; ok {
			s := string(x)
			if name == "format" {
				// The replacement fields format the arguments themselves.
				return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) { return m("str", s, args, kw) }), true
			}
			return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) { return m("str", s, args, kw) }), true
		}
	case *listV:
		if m, ok := methodsList[name]; ok {
			return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) { return m(x, args, kw) }), true
		}
	case *tupleV:
		if m, ok := methodsTuple[name]; ok {
			return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) { return m(x.pyType(), x.items, args, kw) }), true
		}
	case *dictV:
		if m, ok := methodsDict[name]; ok {
			return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) { return m(x, args, kw) }), true
		}
	case intV, boolV:
		if v, ok := methodsIntAttr(v, name); ok {
			return v, true
		}
	case floatV:
		if v, ok := methodsFloatAttr(x, name); ok {
			return v, true
		}
	case rangeV:
		if v, ok := methodsRangeAttr(x, name); ok {
			return v, true
		}
	}
	return slotWrapper(v, name)
}

// methodsBound returns the bound method name of self running fn.
func methodsBound(self Value, name string, fn func(args []Value, kw *dictV) (Value, error)) *callable {
	return &callable{
		name: name,
		typ:  "builtin_function_or_method",
		self: self,
		fn:   func(args []Value, kw *dictV) (Value, error) { return fn(args, kw) },
	}
}

// ---------------------------------------------------------------- argument parsing

// The helpers below reproduce the argument checks of the C calling
// conventions CPython 3.12's builtin methods use; qual is the qualified name
// ("str.center"), name the bare one ("center").

func methodsHasKw(kw *dictV) bool { return kw != nil && kw.Len() > 0 }

// methodsArgsNone checks a METH_NOARGS call.
func methodsArgsNone(qual string, args []Value, kw *dictV) error {
	if methodsHasKw(kw) {
		return typeErr("%s() takes no keyword arguments", qual)
	}
	if len(args) > 0 {
		return typeErr("%s() takes no arguments (%d given)", qual, len(args))
	}
	return nil
}

// methodsArgsOne checks a METH_O call.
func methodsArgsOne(qual string, args []Value, kw *dictV) error {
	if methodsHasKw(kw) {
		return typeErr("%s() takes no keyword arguments", qual)
	}
	if len(args) != 1 {
		return typeErr("%s() takes exactly one argument (%d given)", qual, len(args))
	}
	return nil
}

// methodsArgsPos checks an Argument Clinic call with positional-only parameters
// (_PyArg_NoKwnames, then _PyArg_CheckPositional).
func methodsArgsPos(qual string, min, max int, args []Value, kw *dictV) error {
	if methodsHasKw(kw) {
		return typeErr("%s() takes no keyword arguments", qual)
	}
	return methodsCheckPositional(qual[strings.IndexByte(qual, '.')+1:], len(args), min, max)
}

// methodsCheckPositional is _PyArg_CheckPositional.
func methodsCheckPositional(name string, n, min, max int) error {
	switch {
	case n < min:
		which := "at least "
		if min == max {
			which = ""
		}
		return typeErr("%s expected %s%d argument%s, got %d", name, which, min, pyexc.Plural(min), n)
	case n > max:
		which := "at most "
		if min == max {
			which = ""
		}
		return typeErr("%s expected %s%d argument%s, got %d", name, which, max, pyexc.Plural(max), n)
	}
	return nil
}

// methodsArgsTuple checks a METH_VARARGS call parsed by PyArg_ParseTuple.
func methodsArgsTuple(name string, min, max int, args []Value, kw *dictV) error {
	if methodsHasKw(kw) {
		return typeErr("%s() takes no keyword arguments", name)
	}
	if n := len(args); n < min {
		return typeErr("%s() takes at least %d argument%s (%d given)", name, min, pyexc.Plural(min), n)
	} else if n > max {
		return typeErr("%s() takes at most %d argument%s (%d given)", name, max, pyexc.Plural(max), n)
	}
	return nil
}

// methodsArgsKw is _PyArg_UnpackKeywords for parameters that may all be passed by
// keyword (names), of which the first maxpos may also be positional and none
// is required. It returns one slot per name, nil where nothing was passed.
func methodsArgsKw(name string, names []string, maxpos int, args []Value, kw *dictV) ([]Value, error) {
	return methodsArgsKwRequired(name, names, 0, maxpos, args, kw)
}

// methodsArgsKwRequired is methodsArgsKw whose first `required` parameters
// must be given; a missing one is reported before an unexpected keyword, as
// _PyArg_UnpackKeywords does.
func methodsArgsKwRequired(name string, names []string, required, maxpos int, args []Value, kw *dictV) ([]Value, error) {
	buf := make([]Value, len(names))
	nargs, nkw := len(args), 0
	if kw != nil {
		nkw = kw.Len()
	}
	if nargs+nkw > len(names) {
		which := ""
		if nargs == 0 {
			which = "keyword "
		}
		return nil, typeErr("%s() takes at most %d %sargument%s (%d given)", name, len(names), which, pyexc.Plural(len(names)), nargs+nkw)
	}
	if nargs > maxpos {
		if maxpos == 0 {
			return nil, typeErr("%s() takes no positional arguments", name)
		}
		return nil, typeErr("%s() takes at most %d positional argument%s (%d given)", name, maxpos, pyexc.Plural(maxpos), nargs)
	}
	copy(buf, args)
	for i := nargs; i < len(names) && nkw > 0; i++ {
		if v, ok := kw.getStr(names[i]); ok {
			buf[i] = v
			nkw--
		}
	}
	for i := 0; i < required; i++ {
		if buf[i] == nil {
			return nil, typeErr("%s() missing required argument '%s' (pos %d)", name, names[i], i+1)
		}
	}
	if nkw == 0 {
		return buf, nil
	}
	for i := 0; i < nargs; i++ {
		if _, ok := kw.getStr(names[i]); ok {
			return nil, typeErr("argument for %s() given by name ('%s') and position (%d)", name, names[i], i+1)
		}
	}
	for _, k := range kw.keys {
		ks, _ := isStr(k)
		if !methodsContains(names, ks) {
			return nil, typeErr("'%s' is an invalid keyword argument for %s()", ks, name)
		}
	}
	return nil, typeErr("invalid keyword argument for %s()", name)
}

func methodsContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- argument conversion

// methodsIndex is operator.index for the int-like values.
func methodsIndex(v Value) (*big.Int, error) {
	if n, ok := asInt(v); ok {
		return n, nil
	}
	return nil, typeErr("'%s' object cannot be interpreted as an integer", v.pyType())
}

// methodsSsize converts an argument to a C Py_ssize_t (PyNumber_AsSsize_t with
// OverflowError).
func methodsSsize(v Value) (int, error) {
	n, err := methodsIndex(v)
	if err != nil {
		return 0, err
	}
	i, ok := pyBigToInt(n)
	if !ok {
		return 0, newPyErr("OverflowError", "Python int too large to convert to C ssize_t")
	}
	return i, nil
}

// methodsCInt converts an argument to a C int.
func methodsCInt(v Value) (int, error) {
	n, err := methodsIndex(v)
	if err != nil {
		return 0, err
	}
	if !n.IsInt64() || n.Int64() < math.MinInt32 || n.Int64() > math.MaxInt32 {
		return 0, newPyErr("OverflowError", "Python int too large to convert to C int")
	}
	return int(n.Int64()), nil
}

// methodsOptSliceIndex converts a str search bound (None or an int-like value).
func methodsOptSliceIndex(v Value) (*int, error) {
	i, isNone, err := pySliceIndex(v)
	if err != nil || isNone {
		return nil, err
	}
	return &i, nil
}

// methodsSliceIndexNotNone is _PyEval_SliceIndexNotNone (list and tuple index).
func methodsSliceIndexNotNone(v Value) (int, error) {
	if _, ok := v.(noneV); ok {
		return 0, typeErr("slice indices must be integers or have an __index__ method")
	}
	if _, ok := asInt(v); !ok {
		return 0, typeErr("slice indices must be integers or have an __index__ method")
	}
	i, _, err := pySliceIndex(v)
	return i, err
}

// methodsBadArgType is the type name _PyArg_BadArgument reports.
func methodsBadArgType(v Value) string {
	if _, ok := v.(noneV); ok {
		return "None"
	}
	return typeName(v)
}

// methodsMaxLen bounds the length of a string a method builds; past it the
// method raises MemoryError rather than exhausting memory.
const methodsMaxLen = 1 << 31

func methodsCheckLen(n int) error {
	if n > methodsMaxLen {
		return newPyErr("MemoryError", "")
	}
	return nil
}

// methodsIterAll drains an iterator.
func methodsIterAll(it iterator) ([]Value, error) {
	var out []Value
	for {
		v, ok, err := it.next()
		if err != nil || !ok {
			return out, err
		}
		out = append(out, v)
	}
}

// ---------------------------------------------------------------- str

// methodsStrFn is a str method on the text s of a value of type typ ("str",
// named in some of the errors).
type methodsStrFn func(typ, s string, args []Value, kw *dictV) (Value, error)

var methodsStr map[string]methodsStrFn

func init() {
	methodsStr = map[string]methodsStrFn{
		"encode": func(typ, s string, args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("encode", []string{"encoding", "errors"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			enc, err := optStrArg(buf[0], "utf-8", "encode", "encoding")
			if err != nil {
				return nil, err
			}
			errs, err := optStrArg(buf[1], "strict", "encode", "errors")
			if err != nil {
				return nil, err
			}
			b, err := encodeStr(s, enc, errs)
			if err != nil {
				return nil, err
			}
			return bytesV(b), nil
		},
		"capitalize":   methodsStrMap("capitalize", pyCapitalize),
		"casefold":     methodsStrMap("casefold", pyCasefold),
		"lower":        methodsStrMap("lower", textutil.Lower),
		"upper":        methodsStrMap("upper", pyUpper),
		"swapcase":     methodsStrMap("swapcase", pySwapcase),
		"title":        methodsStrMap("title", pyTitle),
		"isalnum":      methodsStrPred("isalnum", pyIsAlnum),
		"isalpha":      methodsStrPred("isalpha", pyIsAlpha),
		"isascii":      methodsStrPred("isascii", textutil.IsASCII),
		"isdecimal":    methodsStrPred("isdecimal", pyIsDecimal),
		"isdigit":      methodsStrPred("isdigit", pyIsDigit),
		"isidentifier": methodsStrPred("isidentifier", textutil.IsIdentifier),
		"islower":      methodsStrPred("islower", pyIsLower),
		"isnumeric":    methodsStrPred("isnumeric", pyIsNumeric),
		"isprintable":  methodsStrPred("isprintable", pyIsPrintable),
		"isspace":      methodsStrPred("isspace", pyIsSpace),
		"istitle":      methodsStrPred("istitle", pyIsTitle),
		"isupper":      methodsStrPred("isupper", pyIsUpper),
		"center":       methodsStrPad("center", pyCenter),
		"ljust":        methodsStrPad("ljust", pyLJust),
		"rjust":        methodsStrPad("rjust", pyRJust),
		"zfill":        methodsStrZfill,
		"translate":    strTranslate,
		"count":        methodsStrSearch("count", func(s, sub string, st, en *int) (Value, error) { return mkInt(int64(pyCount(s, sub, st, en))), nil }),
		"find":         methodsStrSearch("find", func(s, sub string, st, en *int) (Value, error) { return mkInt(int64(pyFind(s, sub, st, en))), nil }),
		"rfind":        methodsStrSearch("rfind", func(s, sub string, st, en *int) (Value, error) { return mkInt(int64(pyRFind(s, sub, st, en))), nil }),
		"index":        methodsStrSearch("index", func(s, sub string, st, en *int) (Value, error) { return methodsIntResult(pyIndex(s, sub, st, en)) }),
		"rindex":       methodsStrSearch("rindex", func(s, sub string, st, en *int) (Value, error) { return methodsIntResult(pyRIndex(s, sub, st, en)) }),
		"startswith":   methodsStrTailMatch("startswith", pyStartsWith),
		"endswith":     methodsStrTailMatch("endswith", pyEndsWith),
		"expandtabs":   methodsStrExpandtabs,
		"join":         methodsStrJoin,
		"strip":        methodsStrStrip("strip", pyStrip),
		"lstrip":       methodsStrStrip("lstrip", pyLStrip),
		"rstrip":       methodsStrStrip("rstrip", pyRStrip),
		"partition":    methodsStrPartition("partition", pyPartition),
		"rpartition":   methodsStrPartition("rpartition", pyRPartition),
		"removeprefix": methodsStrRemove("removeprefix", pyRemovePrefix),
		"removesuffix": methodsStrRemove("removesuffix", pyRemoveSuffix),
		"replace":      methodsStrReplace,
		"split":        methodsStrSplit("split", pySplit),
		"rsplit":       methodsStrSplit("rsplit", pyRSplit),
		"splitlines":   methodsStrSplitlines,
		"format": func(typ, s string, args []Value, kw *dictV) (Value, error) {
			if kw == nil {
				kw = newDict()
			}
			r, err := strFormat(s, args, kw, fmtHooks{getattr: pyGetattr, getitem: pyGetitem})
			return strV(r), err
		},
		"format_map": func(typ, s string, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("format_map", 1, 1, args, kw); err != nil {
				return nil, err
			}
			r, err := strFormat(s, nil, newDict(), fmtHooks{getattr: pyGetattr, getitem: pyGetitem, mapping: args[0]})
			return strV(r), err
		},
	}
}

func methodsIntResult(i int, err error) (Value, error) {
	if err != nil {
		return nil, err
	}
	return mkInt(int64(i)), nil
}

func methodsStrMap(name string, f func(string) string) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsNone(typ+"."+name, args, kw); err != nil {
			return nil, err
		}
		return strV(f(s)), nil
	}
}

func methodsStrPred(name string, f func(string) bool) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsNone(typ+"."+name, args, kw); err != nil {
			return nil, err
		}
		return pyBool(f(s)), nil
	}
}

// methodsFillChar converts the fillchar of center, ljust and rjust.
func methodsFillChar(v Value) (rune, error) {
	s, ok := isStr(v)
	if !ok {
		return 0, typeErr("The fill character must be a unicode character, not %s", v.pyType())
	}
	r, n := textutil.DecodeRune(s)
	if n == 0 || n != len(s) {
		return 0, typeErr("The fill character must be exactly one character long")
	}
	return r, nil
}

func methodsStrPad(name string, f func(string, int, rune) string) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsPos(typ+"."+name, 1, 2, args, kw); err != nil {
			return nil, err
		}
		width, err := methodsSsize(args[0])
		if err != nil {
			return nil, err
		}
		fill := ' '
		if len(args) > 1 {
			if fill, err = methodsFillChar(args[1]); err != nil {
				return nil, err
			}
		}
		if err := methodsCheckLen(width); err != nil {
			return nil, err
		}
		return strV(f(s, width, fill)), nil
	}
}

func methodsStrZfill(typ, s string, args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne(typ+".zfill", args, kw); err != nil {
		return nil, err
	}
	width, err := methodsSsize(args[0])
	if err != nil {
		return nil, err
	}
	if err := methodsCheckLen(width); err != nil {
		return nil, err
	}
	return strV(pyZfill(s, width)), nil
}

// methodsSearchBounds converts the optional start and end of the search methods.
func methodsSearchBounds(args []Value) (st, en *int, err error) {
	if len(args) > 0 {
		if st, err = methodsOptSliceIndex(args[0]); err != nil {
			return nil, nil, err
		}
	}
	if len(args) > 1 {
		if en, err = methodsOptSliceIndex(args[1]); err != nil {
			return nil, nil, err
		}
	}
	return st, en, nil
}

func methodsStrSearch(name string, f func(s, sub string, st, en *int) (Value, error)) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsTuple(name, 1, 3, args, kw); err != nil {
			return nil, err
		}
		st, en, err := methodsSearchBounds(args[1:])
		if err != nil {
			return nil, err
		}
		sub, ok := isStr(args[0])
		if !ok {
			return nil, typeErr("must be str, not %s", args[0].pyType())
		}
		return f(s, sub, st, en)
	}
}

func methodsStrTailMatch(name string, f func(s, sub string, st, en *int) bool) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsTuple(name, 1, 3, args, kw); err != nil {
			return nil, err
		}
		st, en, err := methodsSearchBounds(args[1:])
		if err != nil {
			return nil, err
		}
		if t, ok := args[0].(*tupleV); ok {
			for _, it := range t.items {
				sub, ok := isStr(it)
				if !ok {
					return nil, typeErr("tuple for %s must only contain str, not %s", name, it.pyType())
				}
				if f(s, sub, st, en) {
					return trueV, nil
				}
			}
			return falseV, nil
		}
		sub, ok := isStr(args[0])
		if !ok {
			return nil, typeErr("%s first arg must be str or a tuple of str, not %s", name, args[0].pyType())
		}
		return pyBool(f(s, sub, st, en)), nil
	}
}

func methodsStrExpandtabs(_, s string, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("expandtabs", []string{"tabsize"}, 1, args, kw)
	if err != nil {
		return nil, err
	}
	tabsize := 8
	if buf[0] != nil {
		if tabsize, err = methodsCInt(buf[0]); err != nil {
			return nil, err
		}
	}
	if tabsize > 1 {
		if err := methodsCheckLen(strings.Count(s, "\t") * tabsize); err != nil {
			return nil, err
		}
	}
	return strV(pyExpandtabs(s, tabsize)), nil
}

func methodsStrJoin(typ, sep string, args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne(typ+".join", args, kw); err != nil {
		return nil, err
	}
	items, err := toSliceAs(args[0], typeErr("can only join an iterable"))
	if err != nil {
		return nil, err
	}
	parts := make([]string, len(items))
	for i, item := range items {
		s, ok := isStr(item)
		if !ok {
			return nil, typeErr("sequence item %d: expected str instance, %s found", i, item.pyType())
		}
		parts[i] = s
	}
	return strV(strings.Join(parts, sep)), nil
}

func methodsStrStrip(name string, f func(string, *string) string) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsPos(typ+"."+name, 0, 1, args, kw); err != nil {
			return nil, err
		}
		var chars *string
		if len(args) > 0 {
			if _, isNone := args[0].(noneV); !isNone {
				c, ok := isStr(args[0])
				if !ok {
					return nil, typeErr("%s arg must be None or str", name)
				}
				chars = &c
			}
		}
		return strV(f(s, chars)), nil
	}
}

func methodsStrPartition(name string, f func(s, sep string) ([3]string, error)) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne(typ+"."+name, args, kw); err != nil {
			return nil, err
		}
		sep, ok := isStr(args[0])
		if !ok {
			return nil, typeErr("must be str, not %s", args[0].pyType())
		}
		p, err := f(s, sep)
		if err != nil {
			return nil, err
		}
		// A separator that was found is returned as it is.
		mid := Value(strV(""))
		if p[1] != "" {
			mid = args[0]
		}
		return newTuple([]Value{strV(p[0]), mid, strV(p[2])}), nil
	}
}

func methodsStrRemove(name string, f func(s, affix string) string) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne(typ+"."+name, args, kw); err != nil {
			return nil, err
		}
		affix, ok := isStr(args[0])
		if !ok {
			return nil, typeErr("%s() argument must be str, not %s", name, methodsBadArgType(args[0]))
		}
		return strV(f(s, affix)), nil
	}
}

func methodsStrReplace(typ, s string, args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsPos(typ+".replace", 2, 3, args, kw); err != nil {
		return nil, err
	}
	old, ok := isStr(args[0])
	if !ok {
		return nil, typeErr("replace() argument 1 must be str, not %s", methodsBadArgType(args[0]))
	}
	repl, ok := isStr(args[1])
	if !ok {
		return nil, typeErr("replace() argument 2 must be str, not %s", methodsBadArgType(args[1]))
	}
	count := -1
	if len(args) > 2 {
		var err error
		if count, err = methodsSsize(args[2]); err != nil {
			return nil, err
		}
	}
	if old == "" && repl != "" {
		n := textutil.RuneCount(s) + 1
		if count >= 0 && count < n {
			n = count
		}
		if err := methodsCheckLen(n * textutil.RuneCount(repl)); err != nil {
			return nil, err
		}
	}
	return strV(pyReplace(s, old, repl, count)), nil
}

func methodsStrSplit(name string, f func(s string, sep *string, maxsplit int) ([]string, error)) methodsStrFn {
	return func(typ, s string, args []Value, kw *dictV) (Value, error) {
		buf, err := methodsArgsKw(name, []string{"sep", "maxsplit"}, 2, args, kw)
		if err != nil {
			return nil, err
		}
		maxsplit := -1
		if buf[1] != nil {
			if maxsplit, err = methodsSsize(buf[1]); err != nil {
				return nil, err
			}
		}
		var sep *string
		if buf[0] != nil {
			if _, isNone := buf[0].(noneV); !isNone {
				c, ok := isStr(buf[0])
				if !ok {
					return nil, typeErr("must be str or None, not %s", buf[0].pyType())
				}
				sep = &c
			}
		}
		parts, err := f(s, sep, maxsplit)
		if err != nil {
			return nil, err
		}
		return methodsStrList(parts), nil
	}
}

func methodsStrList(parts []string) *listV {
	items := make([]Value, len(parts))
	for i, p := range parts {
		items[i] = strV(p)
	}
	return newList(items)
}

func methodsStrSplitlines(_, s string, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("splitlines", []string{"keepends"}, 1, args, kw)
	if err != nil {
		return nil, err
	}
	keepends := false
	if buf[0] != nil {
		if keepends, err = truthy(buf[0]); err != nil {
			return nil, err
		}
	}
	return methodsStrList(textutil.SplitLines(s, keepends)), nil
}
