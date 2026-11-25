// Ported from CPython 3.12 (Modules/mathmodule.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The math module: its functions with CPython 3.12's argument handling,
// domain and range errors, and results (the C library's functions are
// correctly rounded in practice, so these are too).

package py

import (
	"math"
	"math/big"
)

func init() {
	registerModule("math", mathAttrs)
}

// mathAttrs are the math module's attributes, in dir() order.
func mathAttrs() []moduleAttr {
	fns := map[string]func(args []Value, kw *dictV) (Value, error){
		"acos":      math1("acos", preciseAcos, false),
		"acosh":     math1("acosh", preciseAcosh, false),
		"asin":      math1("asin", preciseAsin, false),
		"asinh":     math1("asinh", preciseAsinh, false),
		"atan":      math1("atan", preciseAtan, false),
		"atanh":     math1("atanh", preciseAtanh, false),
		"cos":       math1("cos", preciseCos, false),
		"cosh":      math1("cosh", preciseCosh, true),
		"sin":       math1("sin", preciseSin, false),
		"sinh":      math1("sinh", preciseSinh, true),
		"tan":       math1("tan", preciseTan, false),
		"tanh":      math1("tanh", preciseTanh, false),
		"exp":       math1("exp", preciseExp, true),
		"expm1":     math1("expm1", preciseExpm1, true),
		"fabs":      math1("fabs", math.Abs, false),
		"sqrt":      math1("sqrt", math.Sqrt, false),
		"log1p":     math1("log1p", preciseLog1p, false),
		"degrees":   math1("degrees", func(x float64) float64 { return float64(x * (180.0 / math.Pi)) }, false),
		"radians":   math1("radians", func(x float64) float64 { return float64(x * (math.Pi / 180.0)) }, false),
		"atan2":     math2("atan2", preciseAtan2),
		"copysign":  math2("copysign", math.Copysign),
		"remainder": math2("remainder", math.Remainder),
		"fmod": math2("fmod", func(x, y float64) float64 {
			if math.IsInf(y, 0) && !math.IsInf(x, 0) && !math.IsNaN(x) {
				return x
			}
			return math.Mod(x, y)
		}),
		"ceil":      mathRound("ceil", math.Ceil),
		"floor":     mathRound("floor", math.Floor),
		"trunc":     mathTrunc,
		"factorial": mathFactorial,
		"frexp":     mathFrexp,
		"modf":      mathModf,
		"ldexp":     mathLdexp,
		"fsum":      mathFsum,
		"hypot":     mathHypot,
		"dist":      mathDist,
		"isinf":     mathPred("isinf", func(x float64) bool { return math.IsInf(x, 0) }),
		"isnan":     mathPred("isnan", math.IsNaN),
		"isfinite":  mathPred("isfinite", func(x float64) bool { return !math.IsInf(x, 0) && !math.IsNaN(x) }),
		"isclose":   mathIsclose,
		"log":       mathLog,
		"log2":      mathLogN("log2", preciseLog2),
		"log10":     mathLogN("log10", preciseLog10),
		"pow":       mathPow,
		"gcd":       mathGcdLcm("gcd"),
		"lcm":       mathGcdLcm("lcm"),
		"isqrt":     mathIsqrt,
		"comb":      mathCombPerm("comb"),
		"perm":      mathCombPerm("perm"),
		"prod":      mathProd,
	}
	consts := map[string]Value{"e": floatV(math.E), "pi": floatV(math.Pi), "tau": floatV(2 * math.Pi),
		"inf": floatV(math.Inf(1)), "nan": floatV(math.NaN())}
	out := make([]moduleAttr, 0, len(mathNames))
	for _, name := range mathNames {
		if v, ok := consts[name]; ok {
			out = append(out, moduleAttr{name, v})
		} else {
			out = append(out, moduleAttr{name, newBuiltin(name, fns[name])})
		}
	}
	return out
}

// mathNames are the public names of the math module this port provides, in
// dir() order.
var mathNames = []string{"acos", "acosh", "asin", "asinh", "atan", "atan2", "atanh", "ceil", "comb",
	"copysign", "cos", "cosh", "degrees", "dist", "e", "exp", "expm1", "fabs", "factorial", "floor", "fmod",
	"frexp", "fsum", "gcd", "hypot", "inf", "isclose", "isfinite", "isinf", "isnan", "isqrt", "lcm", "ldexp",
	"log", "log10", "log1p", "log2", "modf", "nan", "perm", "pi", "pow", "prod", "radians", "remainder", "sin",
	"sinh", "sqrt", "tan", "tanh", "tau", "trunc"}

func domainErr() error { return valueErr("math domain error") }
func rangeErr() error  { return newPyErr("OverflowError", "math range error") }

// mathFloat is PyFloat_AsDouble for a math function's argument.
func mathFloat(v Value) (float64, error) {
	switch v.(type) {
	case intV, boolV, floatV:
		return toFloat(v)
	}
	return 0, typeErr("must be real number, not %s", v.pyType())
}

// math1 is mathmodule's math_1: a function of one float, with its NaN and
// infinity checks.
func math1(name string, fn func(float64) float64, canOverflow bool) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("math."+name, args, kw); err != nil {
			return nil, err
		}
		x, err := mathFloat(args[0])
		if err != nil {
			return nil, err
		}
		r := fn(x)
		if math.IsNaN(r) && !math.IsNaN(x) {
			return nil, domainErr()
		}
		if math.IsInf(r, 0) && !math.IsInf(x, 0) && !math.IsNaN(x) {
			if canOverflow {
				return nil, rangeErr()
			}
			return nil, domainErr()
		}
		return floatV(r), nil
	}
}

// math2 is mathmodule's math_2.
func math2(name string, fn func(x, y float64) float64) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := posOnly(name, 2, 2, args, kw); err != nil {
			return nil, err
		}
		x, err := mathFloat(args[0])
		if err != nil {
			return nil, err
		}
		y, err := mathFloat(args[1])
		if err != nil {
			return nil, err
		}
		r := fn(x, y)
		finite := func(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }
		if math.IsNaN(r) {
			if !math.IsNaN(x) && !math.IsNaN(y) {
				return nil, domainErr()
			}
		} else if math.IsInf(r, 0) && finite(x) && finite(y) {
			return nil, rangeErr()
		}
		return floatV(r), nil
	}
}

func mathPred(name string, fn func(float64) bool) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("math."+name, args, kw); err != nil {
			return nil, err
		}
		x, err := mathFloat(args[0])
		if err != nil {
			return nil, err
		}
		return pyBool(fn(x)), nil
	}
}

// mathRound is math.ceil / math.floor: an int's own value, a float rounded
// and converted to int.
func mathRound(name string, fn func(float64) float64) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("math."+name, args, kw); err != nil {
			return nil, err
		}
		if i, ok := asInt(args[0]); ok {
			return mkBig(new(big.Int).Set(i)), nil
		}
		x, err := mathFloat(args[0])
		if err != nil {
			return nil, err
		}
		return floatToInt(fn(x))
	}
}

func floatToInt(f float64) (Value, error) {
	if math.IsInf(f, 0) {
		return nil, newPyErr("OverflowError", "cannot convert float infinity to integer")
	}
	if math.IsNaN(f) {
		return nil, valueErr("cannot convert float NaN to integer")
	}
	i, _ := big.NewFloat(f).Int(nil)
	return mkBig(i), nil
}

func mathTrunc(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.trunc", args, kw); err != nil {
		return nil, err
	}
	switch x := args[0].(type) {
	case intV, boolV:
		i, _ := asInt(x)
		return mkBig(new(big.Int).Set(i)), nil
	case floatV:
		return floatToInt(math.Trunc(float64(x)))
	}
	return nil, typeErr("type %s doesn't define __trunc__ method", args[0].pyType())
}

func mathFactorial(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.factorial", args, kw); err != nil {
		return nil, err
	}
	n, err := methodsIndex(args[0])
	if err != nil {
		return nil, err
	}
	if !n.IsInt64() {
		if n.Sign() < 0 {
			return nil, valueErr("factorial() not defined for negative values")
		}
		return nil, newPyErr("OverflowError", "factorial() argument should not exceed 9223372036854775807")
	}
	if n.Int64() < 0 {
		return nil, valueErr("factorial() not defined for negative values")
	}
	if n.Int64() > 20000 {
		return nil, newPyErr("MemoryError", "")
	}
	return mkBig(new(big.Int).MulRange(1, n.Int64())), nil
}

func mathFrexp(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.frexp", args, kw); err != nil {
		return nil, err
	}
	x, err := mathFloat(args[0])
	if err != nil {
		return nil, err
	}
	if math.IsInf(x, 0) || math.IsNaN(x) || x == 0 {
		return newTuple([]Value{floatV(x), mkInt(0)}), nil
	}
	m, e := math.Frexp(x)
	return newTuple([]Value{floatV(m), mkInt(int64(e))}), nil
}

func mathModf(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.modf", args, kw); err != nil {
		return nil, err
	}
	x, err := mathFloat(args[0])
	if err != nil {
		return nil, err
	}
	if math.IsInf(x, 0) {
		return newTuple([]Value{floatV(math.Copysign(0, x)), floatV(x)}), nil
	}
	ip, frac := math.Modf(x)
	return newTuple([]Value{floatV(frac), floatV(ip)}), nil
}

func mathLdexp(args []Value, kw *dictV) (Value, error) {
	if err := posOnly("ldexp", 2, 2, args, kw); err != nil {
		return nil, err
	}
	x, err := mathFloat(args[0])
	if err != nil {
		return nil, err
	}
	i, ok := asInt(args[1])
	if !ok {
		return nil, typeErr("Expected an int as second argument to ldexp.")
	}
	if x == 0 || math.IsInf(x, 0) || math.IsNaN(x) {
		return floatV(x), nil
	}
	var exp int
	switch {
	case i.Cmp(big.NewInt(100000)) > 0:
		return nil, rangeErr()
	case i.Cmp(big.NewInt(-100000)) < 0:
		return floatV(math.Copysign(0, x)), nil
	default:
		exp = int(i.Int64())
	}
	r := math.Ldexp(x, exp)
	if math.IsInf(r, 0) {
		return nil, rangeErr()
	}
	return floatV(r), nil
}

// mathFsum is math.fsum, mathmodule.c's math_fsum: Shewchuk's exact
// partial sums, with CPython's overflow and special-value handling.
func mathFsum(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.fsum", args, kw); err != nil {
		return nil, err
	}
	it, err := pyIter(args[0])
	if err != nil {
		return nil, err
	}
	var p []float64
	specialSum, infSum := 0.0, 0.0
	for {
		item, ok, err := it.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		x, err := mathFloat(item)
		if err != nil {
			return nil, err
		}
		xsave := x
		i := 0
		for _, y := range p {
			if math.Abs(x) < math.Abs(y) {
				x, y = y, x
			}
			hi := x + y
			yr := hi - x
			lo := y - yr
			if lo != 0 {
				p[i] = lo
				i++
			}
			x = hi
		}
		p = p[:i]
		if x != 0 {
			if math.IsInf(x, 0) || math.IsNaN(x) {
				if !math.IsInf(xsave, 0) && !math.IsNaN(xsave) {
					return nil, newPyErr("OverflowError", "intermediate overflow in fsum")
				}
				if math.IsInf(xsave, 0) {
					infSum += xsave
				}
				specialSum += xsave
				p = p[:0]
			} else {
				p = append(p, x)
			}
		}
	}
	if specialSum != 0 || math.IsNaN(specialSum) {
		if math.IsNaN(infSum) {
			return nil, valueErr("-inf + inf in fsum")
		}
		return floatV(specialSum), nil
	}
	hi := 0.0
	if n := len(p); n > 0 {
		n--
		hi = p[n]
		lo := 0.0
		for n > 0 {
			x := hi
			n--
			y := p[n]
			hi = x + y
			yr := hi - x
			lo = y - yr
			if lo != 0 {
				break
			}
		}
		if n > 0 && (lo < 0 && p[n-1] < 0 || lo > 0 && p[n-1] > 0) {
			y := lo * 2.0
			x := hi + y
			yr := x - hi
			if y == yr {
				hi = x
			}
		}
	}
	return floatV(hi), nil
}

// mathHypot is math.hypot(*coordinates), CPython 3.12's vector_norm.
func mathHypot(args []Value, kw *dictV) (Value, error) {
	if err := noKw("hypot", kw); err != nil {
		return nil, err
	}
	vec := make([]float64, len(args))
	maxv, foundNaN := 0.0, false
	for i, a := range args {
		x, err := mathFloat(a)
		if err != nil {
			return nil, err
		}
		x = math.Abs(x)
		vec[i] = x
		foundNaN = foundNaN || math.IsNaN(x)
		if x > maxv {
			maxv = x
		}
	}
	return floatV(vectorNorm(vec, maxv, foundNaN)), nil
}

func vectorNorm(vec []float64, maxv float64, foundNaN bool) float64 {
	if math.IsInf(maxv, 0) {
		return maxv
	}
	if foundNaN {
		return math.NaN()
	}
	if maxv == 0 || len(vec) <= 1 {
		return maxv
	}
	_, maxE := math.Frexp(maxv)
	if maxE < -1023 {
		for i := range vec {
			vec[i] /= 2.2250738585072014e-308
		}
		return 2.2250738585072014e-308 * vectorNorm(vec, maxv/2.2250738585072014e-308, foundNaN)
	}
	scale := math.Ldexp(1.0, -maxE)
	csum, frac1, frac2 := 1.0, 0.0, 0.0
	for _, x := range vec {
		x = float64(x * scale)
		prHi := float64(x * x)
		prLo := math.FMA(x, x, -prHi)
		smHi := csum + prHi
		smLo := prHi - (smHi - csum)
		csum = smHi
		frac1 += prLo
		frac2 += smLo
	}
	h := math.Sqrt(csum - 1.0 + (frac1 + frac2))
	prHi := float64(-h * h)
	prLo := math.FMA(-h, h, -prHi)
	smHi := csum + prHi
	smLo := prHi - (smHi - csum)
	csum = smHi
	frac1 += prLo
	frac2 += smLo
	x := csum - 1.0 + (frac1 + frac2)
	h += x / (2.0 * h)
	return h / scale
}

// loghelper is mathmodule's loghelper: ints of any size are handled exactly
// enough not to overflow.
func loghelper(v Value, fn func(float64) float64) (float64, error) {
	if i, ok := asInt(v); ok {
		if i.Sign() <= 0 {
			return 0, domainErr()
		}
		f, err := bigToFloat(i)
		if err == nil {
			return fn(f), nil
		}
		e := i.BitLen()
		m := new(big.Float).SetInt(i)
		m.SetMantExp(m, -e)
		mf, _ := m.Float64()
		return fn(mf) + float64(float64(e)*fn(2.0)), nil
	}
	x, err := mathFloat(v)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(x) {
		return x, nil
	}
	if x <= 0 {
		return 0, domainErr()
	}
	if math.IsInf(x, 1) {
		return x, nil
	}
	return fn(x), nil
}

func mathLog(args []Value, kw *dictV) (Value, error) {
	if err := posOnly("log", 1, 2, args, kw); err != nil {
		return nil, err
	}
	num, err := loghelper(args[0], preciseLog)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return floatV(num), nil
	}
	den, err := loghelper(args[1], preciseLog)
	if err != nil {
		return nil, err
	}
	return pyTrueDiv(floatV(num), floatV(den))
}

func mathLogN(name string, fn func(float64) float64) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("math."+name, args, kw); err != nil {
			return nil, err
		}
		r, err := loghelper(args[0], fn)
		return floatV(r), err
	}
}

// mathPow is math.pow.
func mathPow(args []Value, kw *dictV) (Value, error) {
	if err := posOnly("pow", 2, 2, args, kw); err != nil {
		return nil, err
	}
	x, err := mathFloat(args[0])
	if err != nil {
		return nil, err
	}
	y, err := mathFloat(args[1])
	if err != nil {
		return nil, err
	}
	finite := func(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }
	var r float64
	switch {
	case !finite(x) || !finite(y):
		switch {
		case math.IsNaN(x):
			if y == 0 {
				r = 1
			} else {
				r = x
			}
		case math.IsNaN(y):
			if x == 1 {
				r = 1
			} else {
				r = y
			}
		case math.IsInf(x, 0):
			oddY := finite(y) && math.Mod(math.Abs(y), 2.0) == 1.0
			if y > 0 {
				if oddY {
					r = x
				} else {
					r = math.Abs(x)
				}
			} else if y == 0 {
				r = 1
			} else if oddY {
				r = math.Copysign(0, x)
			} else {
				r = 0
			}
		default: // y infinite
			if math.Abs(x) == 1 {
				r = 1
			} else if (y > 0) == (math.Abs(x) > 1) {
				r = y
			} else {
				r = 0
			}
		}
	default:
		if x == 0 && y < 0 {
			return nil, domainErr()
		}
		r = accuratePowSigned(x, y)
		if math.IsNaN(r) {
			return nil, domainErr()
		}
		if math.IsInf(r, 0) {
			return nil, rangeErr()
		}
	}
	return floatV(r), nil
}

// mathDist is math.dist(p, q): the Euclidean distance between two points
// given as iterables of real numbers of the same length.
func mathDist(args []Value, kw *dictV) (Value, error) {
	if err := posOnly("dist", 2, 2, args, kw); err != nil {
		return nil, err
	}
	p, err := toSlice(args[0])
	if err != nil {
		return nil, err
	}
	q, err := toSlice(args[1])
	if err != nil {
		return nil, err
	}
	if len(p) != len(q) {
		return nil, valueErr("both points must have the same number of dimensions")
	}
	diffs := make([]float64, len(p))
	maxv, foundNaN := 0.0, false
	for i := range p {
		px, err := mathFloat(p[i])
		if err != nil {
			return nil, err
		}
		qx, err := mathFloat(q[i])
		if err != nil {
			return nil, err
		}
		x := math.Abs(px - qx)
		diffs[i] = x
		foundNaN = foundNaN || math.IsNaN(x)
		if x > maxv {
			maxv = x
		}
	}
	return floatV(vectorNorm(diffs, maxv, foundNaN)), nil
}

// mathIsclose is math.isclose(a, b, *, rel_tol=1e-09, abs_tol=0.0).
func mathIsclose(args []Value, kw *dictV) (Value, error) {
	buf, err := acArgs("isclose", []string{"a", "b", "rel_tol", "abs_tol"}, 2, 2, args, kw)
	if err != nil {
		return nil, err
	}
	vals := []float64{0, 0, 1e-09, 0}
	for i, v := range buf {
		if v == nil {
			continue
		}
		if vals[i], err = mathFloat(v); err != nil {
			return nil, err
		}
	}
	a, b, rel, abs := vals[0], vals[1], vals[2], vals[3]
	if rel < 0 || abs < 0 {
		return nil, valueErr("tolerances must be non-negative")
	}
	if a == b {
		return trueV, nil
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return falseV, nil
	}
	diff := math.Abs(b - a)
	return pyBool(diff <= math.Abs(float64(rel*b)) || diff <= math.Abs(float64(rel*a)) || diff <= abs), nil
}

// mathGcdLcm is math.gcd(*integers) or math.lcm(*integers).
func mathGcdLcm(name string) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		if err := noKw(name, kw); err != nil {
			return nil, err
		}
		acc := big.NewInt(0)
		if name == "lcm" {
			acc.SetInt64(1)
		}
		for _, a := range args {
			n, err := methodsIndex(a)
			if err != nil {
				return nil, err
			}
			x := new(big.Int).Abs(n)
			if name == "gcd" {
				acc.GCD(nil, nil, acc, x)
				continue
			}
			if acc.Sign() == 0 || x.Sign() == 0 {
				acc.SetInt64(0)
				continue
			}
			g := new(big.Int).GCD(nil, nil, acc, x)
			acc.Mul(acc, new(big.Int).Quo(x, g))
		}
		return mkBig(acc), nil
	}
}

// mathIsqrt is math.isqrt(n), the integer square root.
func mathIsqrt(args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("math.isqrt", args, kw); err != nil {
		return nil, err
	}
	n, err := methodsIndex(args[0])
	if err != nil {
		return nil, err
	}
	if n.Sign() < 0 {
		return nil, valueErr("isqrt() argument must be nonnegative")
	}
	return mkBig(new(big.Int).Sqrt(n)), nil
}

// mathCombPerm is math.comb(n, k) or math.perm(n, k=None).
func mathCombPerm(name string) func(args []Value, kw *dictV) (Value, error) {
	return func(args []Value, kw *dictV) (Value, error) {
		min := 2
		if name == "perm" {
			min = 1
		}
		if err := posOnly(name, min, 2, args, kw); err != nil {
			return nil, err
		}
		if len(args) == 1 || isNoneV(args[1]) {
			return mathFactorial(args[:1], nil)
		}
		n, err := methodsIndex(args[0])
		if err != nil {
			return nil, err
		}
		k, err := methodsIndex(args[1])
		if err != nil {
			return nil, err
		}
		if n.Sign() < 0 {
			return nil, valueErr("n must be a non-negative integer")
		}
		if k.Sign() < 0 {
			return nil, valueErr("k must be a non-negative integer")
		}
		if k.Cmp(n) > 0 {
			return mkInt(0), nil
		}
		if !k.IsInt64() || k.Int64() > 20000 {
			return nil, newPyErr("OverflowError", "min(n - k, k) must not exceed 9223372036854775807")
		}
		// n * (n-1) * ... * (n-k+1), divided by k! for comb.
		r := big.NewInt(1)
		f := new(big.Int)
		for i := int64(0); i < k.Int64(); i++ {
			r.Mul(r, f.Sub(n, big.NewInt(i)))
		}
		if name == "comb" {
			r.Quo(r, new(big.Int).MulRange(1, k.Int64()))
		}
		return mkBig(r), nil
	}
}

// mathProd is math.prod(iterable, *, start=1): start times each item in
// turn, with Python's multiplication.
func mathProd(args []Value, kw *dictV) (Value, error) {
	buf, err := acArgs("prod", []string{"iterable", "start"}, 1, 1, args, kw)
	if err != nil {
		return nil, err
	}
	var result Value = mkInt(1)
	if buf[1] != nil {
		result = buf[1]
	}
	it, err := pyIter(buf[0])
	if err != nil {
		return nil, err
	}
	for {
		item, more, err := it.next()
		if err != nil {
			return nil, err
		}
		if !more {
			return result, nil
		}
		if result, err = binop("Mult", result, item); err != nil {
			return nil, err
		}
	}
}
