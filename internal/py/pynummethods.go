// Ported from CPython 3.12 (Objects/longobject.c, Objects/floatobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The methods of int and float that convert to and from bytes, hexadecimal
// text and integer ratios.

package py

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// intToBytes is int.to_bytes(length=1, byteorder='big', *, signed=False).
func intToBytes(n *big.Int, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("to_bytes", []string{"length", "byteorder", "signed"}, 2, args, kw)
	if err != nil {
		return nil, err
	}
	length := int64(1)
	if buf[0] != nil {
		l, err := methodsIndex(buf[0])
		if err != nil {
			return nil, err
		}
		if l.Sign() < 0 {
			return nil, valueErr("length argument must be non-negative")
		}
		if !l.IsInt64() {
			return nil, newPyErr("OverflowError", "Python int too large to convert to C ssize_t")
		}
		length = l.Int64()
	}
	little, err := byteOrder("to_bytes", buf[1])
	if err != nil {
		return nil, err
	}
	signed := false
	if buf[2] != nil {
		if signed, err = truthy(buf[2]); err != nil {
			return nil, err
		}
	}
	if n.Sign() < 0 && !signed {
		return nil, newPyErr("OverflowError", "can't convert negative int to unsigned")
	}
	// The value in two's complement on length bytes, if it fits.
	v := new(big.Int).Set(n)
	limit := new(big.Int).Lsh(big.NewInt(1), uint(8*length))
	if signed {
		half := new(big.Int).Rsh(limit, 1)
		if v.Cmp(half) >= 0 || v.Cmp(new(big.Int).Neg(half)) < 0 {
			return nil, newPyErr("OverflowError", "int too big to convert")
		}
		if v.Sign() < 0 {
			v.Add(v, limit)
		}
	} else if v.Cmp(limit) >= 0 {
		return nil, newPyErr("OverflowError", "int too big to convert")
	}
	out := v.FillBytes(make([]byte, length))
	if little {
		reverseBytes(out)
	}
	return bytesV(out), nil
}

// intFromBytes is the classmethod int.from_bytes(bytes, byteorder='big', *,
// signed=False), bound to cls: an int, or for bool cls(int).
func intFromBytes(cls *classV) Value {
	return &callable{name: "from_bytes", typ: "builtin_function_or_method", self: cls,
		fn: func(args []Value, kw *dictV) (Value, error) {
			buf, err := acArgs("from_bytes", []string{"bytes", "byteorder", "signed"}, 1, 2, args, kw)
			if err != nil {
				return nil, err
			}
			data, err := bytesFromObject(buf[0])
			if err != nil {
				return nil, err
			}
			little, err := byteOrder("from_bytes", buf[1])
			if err != nil {
				return nil, err
			}
			signed := false
			if buf[2] != nil {
				if signed, err = truthy(buf[2]); err != nil {
					return nil, err
				}
			}
			b := []byte(data)
			if little {
				b = append([]byte(nil), b...)
				reverseBytes(b)
			}
			n := new(big.Int).SetBytes(b)
			if signed && len(b) > 0 && b[0]&0x80 != 0 {
				n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(8*len(b))))
			}
			if cls.name == "bool" {
				return pyBool(n.Sign() != 0), nil
			}
			return mkBig(n), nil
		}}
}

// byteOrder reads the byteorder argument: true for "little".
func byteOrder(fn string, v Value) (bool, error) {
	if v == nil {
		return false, nil
	}
	s, ok := v.(strV)
	if !ok {
		return false, typeErr("%s() argument 'byteorder' must be str, not %s", fn, typeName(v))
	}
	switch s {
	case "little":
		return true, nil
	case "big":
		return false, nil
	}
	return false, valueErr("byteorder must be either 'little' or 'big'")
}

// bytesFromObject is PyObject_Bytes as int.from_bytes uses it: the bytes of a
// bytes-like object, or of an iterable of ints in range(256).
func bytesFromObject(v Value) (string, error) {
	switch x := v.(type) {
	case bytesV:
		return string(x), nil
	case *bytearrayV:
		return string(x.b), nil
	case strV, intV, boolV, floatV, complexV, noneV:
		return "", typeErr("cannot convert '%s' object to bytes", typeName(v))
	}
	items, err := toSlice(v)
	if err != nil {
		if isExc(err, "TypeError") {
			return "", typeErr("cannot convert '%s' object to bytes", typeName(v))
		}
		return "", err
	}
	out := make([]byte, len(items))
	for i, it := range items {
		n, err := methodsIndex(it)
		if err != nil {
			return "", err
		}
		if n.Sign() < 0 || n.Cmp(big.NewInt(255)) > 0 {
			return "", valueErr("bytes must be in range(0, 256)")
		}
		out[i] = byte(n.Int64())
	}
	return string(out), nil
}

func reverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

// floatHex is float.hex(): the exact value in hexadecimal, normalised as
// 0x1.<13 hex digits>p<exponent>, or 0x0.<digits>p-1022 for a subnormal.
func floatHex(x float64) string {
	switch {
	case math.IsNaN(x):
		return "nan"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(x) {
		sign = "-"
		x = -x
	}
	if x == 0 {
		return sign + "0x0.0p+0"
	}
	bits := math.Float64bits(x)
	exp := int((bits>>52)&0x7ff) - 1023
	mant := bits & (1<<52 - 1)
	lead := "1"
	if exp == -1023 {
		lead, exp = "0", -1022
	}
	digits := strings.TrimLeft(big.NewInt(int64(mant)+1<<52).Text(16), "1")
	esign := "+"
	if exp < 0 {
		esign, exp = "-", -exp
	}
	return sign + "0x" + lead + "." + digits + "p" + esign + strconv.Itoa(exp)
}

// floatFromhex is the classmethod float.fromhex(s), bound to the float class.
func floatFromhex(cls *classV) Value {
	return &callable{name: "fromhex", typ: "builtin_function_or_method", self: cls,
		fn: func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("fromhex", args, kw); err != nil {
				return nil, err
			}
			s, ok := args[0].(strV)
			if !ok {
				return nil, typeErr("bad argument type for built-in operation")
			}
			f, err := parseHexFloat(string(s))
			if err != nil {
				return nil, err
			}
			return floatV(f), nil
		}}
}

// parseHexFloat is float_fromhex: [sign] ['0x'] hexdigits ['.' hexdigits]
// ['p' [sign] decimal digits], or inf, infinity or nan, with surrounding
// whitespace; rounded half to even.
func parseHexFloat(text string) (float64, error) {
	invalid := valueErr("invalid hexadecimal floating-point string")
	s := pyStrip(text, nil)
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	switch strings.ToLower(s) {
	case "inf", "infinity":
		return math.Copysign(math.Inf(1), boolSign(neg)), nil
	case "nan":
		return math.NaN(), nil
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	mant := new(big.Int)
	digits, fracDigits := 0, 0
	seenDot := false
	i := 0
	for ; i < len(s); i++ {
		ch := s[i]
		if ch == '.' && !seenDot {
			seenDot = true
			continue
		}
		d := hexValue(ch)
		if d < 0 {
			break
		}
		mant.Lsh(mant, 4).Or(mant, big.NewInt(int64(d)))
		digits++
		if seenDot {
			fracDigits++
		}
	}
	if digits == 0 {
		return 0, invalid
	}
	exp := int64(0)
	if i < len(s) && (s[i] == 'p' || s[i] == 'P') {
		i++
		start := i
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i == len(s) || s[i] < '0' || s[i] > '9' {
			return 0, invalid
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		e, ok := new(big.Int).SetString(s[start:i], 10)
		if !ok {
			return 0, invalid
		}
		switch {
		case e.Cmp(big.NewInt(1<<40)) > 0:
			exp = 1 << 40
		case e.Cmp(big.NewInt(-(1 << 40))) < 0:
			exp = -(1 << 40)
		default:
			exp = e.Int64()
		}
	}
	if i != len(s) {
		return 0, invalid
	}
	exp -= int64(4 * fracDigits)
	if mant.Sign() == 0 {
		return math.Copysign(0, boolSign(neg)), nil
	}
	top := int64(mant.BitLen()) + exp
	if top > 1024 {
		return 0, newPyErr("OverflowError", "hexadecimal value too large to represent as a float")
	}
	if top < -1080 {
		return math.Copysign(0, boolSign(neg)), nil
	}
	f, _ := new(big.Float).SetMantExp(new(big.Float).SetInt(mant), int(exp)).Float64()
	if math.IsInf(f, 0) {
		return 0, newPyErr("OverflowError", "hexadecimal value too large to represent as a float")
	}
	return math.Copysign(f, boolSign(neg)), nil
}

func boolSign(neg bool) float64 {
	if neg {
		return -1
	}
	return 1
}

func hexValue(ch byte) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10
	}
	return -1
}

// floatRatio is float.as_integer_ratio(): the exact value as a fraction in
// lowest terms with a positive denominator.
func floatRatio(x float64) (Value, error) {
	if math.IsInf(x, 0) {
		return nil, newPyErr("OverflowError", "cannot convert Infinity to integer ratio")
	}
	if math.IsNaN(x) {
		return nil, valueErr("cannot convert NaN to integer ratio")
	}
	r, _ := new(big.Float).SetFloat64(x).Rat(nil)
	return newTuple([]Value{mkBig(new(big.Int).Set(r.Num())), mkBig(new(big.Int).Set(r.Denom()))}), nil
}

// strTranslate is str.translate(table): each character's code point looked
// up in table; a LookupError keeps the character, None deletes it, an int is
// a code point and a str a replacement.
func strTranslate(typ, s string, args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne(typ+".translate", args, kw); err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, r := range s {
		v, err := pyGetitem(args[0], mkInt(int64(r)))
		if err != nil {
			if isExc(err, "LookupError") {
				b.WriteRune(r)
				continue
			}
			return nil, err
		}
		switch x := v.(type) {
		case noneV:
		case strV:
			b.WriteString(string(x))
		case intV, boolV:
			n, _ := asInt(x)
			if n.Sign() < 0 || n.Cmp(big.NewInt(0x110000)) >= 0 {
				return nil, valueErr("character mapping must be in range(0x110000)")
			}
			b.WriteRune(rune(n.Int64()))
		default:
			return nil, typeErr("character mapping must return integer, None or str")
		}
	}
	return strV(b.String()), nil
}

// strMaketrans is the static method str.maketrans(x[, y[, z]]), shown as
// bound to the str class.
func strMaketrans() Value {
	return &callable{name: "maketrans", typ: "builtin_function_or_method", self: classes["str"],
		fn: func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("maketrans", 1, 3, args, kw); err != nil {
				return nil, err
			}
			out := newDict()
			if len(args) == 1 {
				d, ok := args[0].(*dictV)
				if !ok {
					return nil, typeErr("if you give only one argument to maketrans it must be a dict")
				}
				for i, k := range d.keys {
					switch key := k.(type) {
					case strV:
						rs := []rune(string(key))
						if len(rs) != 1 {
							return nil, valueErr("string keys in translate table must be of length 1")
						}
						if err := out.set(mkInt(int64(rs[0])), d.vals[i]); err != nil {
							return nil, err
						}
					case intV, boolV:
						if err := out.set(k, d.vals[i]); err != nil {
							return nil, err
						}
					default:
						return nil, typeErr("keys in translate table must be strings or integers")
					}
				}
				return out, nil
			}
			x, ok := args[0].(strV)
			if !ok {
				return nil, typeErr("maketrans() argument 1 must be str, not %s", typeName(args[0]))
			}
			y, ok := args[1].(strV)
			if !ok {
				return nil, typeErr("maketrans() argument 2 must be str, not %s", typeName(args[1]))
			}
			xs, ys := []rune(string(x)), []rune(string(y))
			if len(xs) != len(ys) {
				return nil, valueErr("the first two maketrans arguments must have equal length")
			}
			for i := range xs {
				if err := out.set(mkInt(int64(xs[i])), mkInt(int64(ys[i]))); err != nil {
					return nil, err
				}
			}
			if len(args) == 3 {
				z, ok := args[2].(strV)
				if !ok {
					return nil, typeErr("maketrans() argument 3 must be str, not %s", typeName(args[2]))
				}
				for _, r := range string(z) {
					if err := out.set(mkInt(int64(r)), none); err != nil {
						return nil, err
					}
				}
			}
			return out, nil
		}}
}

// classMethodOf is the classmethod or static method name of the builtin class
// k, bound to the class as CPython shows it, when k defines one.
func classMethodOf(k *classV, name string) (Value, bool) {
	switch {
	case name == "from_bytes" && (k.name == "int" || k.name == "bool"):
		return intFromBytes(k), true
	case name == "fromhex" && k.name == "float":
		return floatFromhex(k), true
	case name == "maketrans" && k.name == "str":
		return strMaketrans(), true
	case name == "fromkeys" && k.name == "dict":
		return dictFromkeys(k), true
	}
	return nil, false
}
