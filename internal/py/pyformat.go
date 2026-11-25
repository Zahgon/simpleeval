// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Python's string formatting: printf-style `str % args` (PyUnicode_Format in Objects/unicodeobject.c),
// and the replacement-field engine of str.format (Objects/stringlib/unicode_format.h) in the variant
// of string.Formatter, which the f-strings use.

package py

import (
	"github.com/danthedeckie/simpleeval/internal/textutil"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- str % args

// Conversion flags of a printf-style specifier.
const (
	pctLJust = 1 << iota // '-'
	pctSign              // '+'
	pctBlank             // ' '
	pctAlt               // '#'
	pctZero              // '0'
)

// pctFormatter is the state of one `format % args` (unicode_formatter_t).
type pctFormatter struct {
	f    []rune
	pos  int
	eof  bool // a read past the end was attempted (fmtcnt < 0)
	ch   rune
	args Value
	// argidx and arglen follow CPython: arglen is -1 and argidx -2 when
	// args is a single value rather than a tuple.
	argidx, arglen int
	dict           Value // args when it is a mapping, else nil
	out            strings.Builder
	// bytes is set for `bytes % args` (_PyBytes_FormatEx): the format and
	// the output hold one character per byte (Latin-1), mapping keys are
	// bytes, and %s, %r and %c convert as bytes formatting does.
	bytes bool
}

// pctArg is one conversion specifier (unicode_format_arg_t).
type pctArg struct {
	ch    rune
	flags int
	width int
	prec  int
	sign  bool
}

// percentFormat is `format % args` for a str (PyUnicode_Format).
func percentFormat(format string, args Value) (string, error) {
	return (&pctFormatter{f: textutil.Runes(format), args: args, arglen: -1, argidx: -2}).run()
}

// percentFormatBytes is `format % args` for bytes (_PyBytes_FormatEx).
func percentFormatBytes(format string, args Value) (string, error) {
	f := make([]rune, len(format))
	for i := 0; i < len(format); i++ {
		f[i] = rune(format[i])
	}
	s, err := (&pctFormatter{f: f, args: args, arglen: -1, argidx: -2, bytes: true}).run()
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return string(out), nil
}

func (c *pctFormatter) run() (string, error) {
	args := c.args
	if t, ok := args.(*tupleV); ok {
		c.arglen, c.argidx = len(t.items), 0
	} else if fmtIsMapping(args, c.bytes) {
		c.dict = args
	}
	for c.pos < len(c.f) {
		if c.f[c.pos] != '%' {
			start := c.pos
			for c.pos < len(c.f) && c.f[c.pos] != '%' {
				c.pos++
			}
			c.out.WriteString(textutil.FromRunes(c.f[start:c.pos]))
			continue
		}
		c.pos++
		if err := c.arg(); err != nil {
			return "", err
		}
	}
	if c.argidx < c.arglen && c.dict == nil {
		return "", c.notAllConverted()
	}
	return c.out.String(), nil
}

func (c *pctFormatter) notAllConverted() error {
	if c.bytes {
		return typeErr("not all arguments converted during bytes formatting")
	}
	return typeErr("not all arguments converted during string formatting")
}

// next reads the next format character into c.ch, or records the end.
func (c *pctFormatter) next() bool {
	if c.pos < len(c.f) {
		c.ch = c.f[c.pos]
		c.pos++
		return true
	}
	c.eof = true
	return false
}

// nextArg is unicode_format_getnextarg.
func (c *pctFormatter) nextArg() (Value, error) {
	if c.argidx < c.arglen {
		i := c.argidx
		c.argidx++
		if c.arglen < 0 {
			return c.args, nil
		}
		return c.args.(*tupleV).items[i], nil
	}
	return nil, typeErr("not enough arguments for format string")
}

// arg formats one specifier; c.pos is just past its '%'
// (unicode_format_arg).
func (c *pctFormatter) arg() error {
	c.ch = 0
	if c.pos < len(c.f) {
		c.ch = c.f[c.pos]
	}
	if c.ch == '%' {
		c.pos++
		c.out.WriteByte('%')
		return nil
	}
	a := &pctArg{width: -1, prec: -1}
	if err := c.parse(a); err != nil {
		return err
	}
	s, done, err := c.convert(a)
	if err != nil {
		return err
	}
	if done {
		c.out.WriteString(s)
	} else if err := c.output(a, s); err != nil {
		return err
	}
	if c.dict != nil && c.argidx < c.arglen {
		return c.notAllConverted()
	}
	return nil
}

// starArg reads a '*' width or precision from the arguments.
func (c *pctFormatter) starArg(limit int64, cType string) (int64, error) {
	v, err := c.nextArg()
	if err != nil {
		return 0, err
	}
	i, ok := asInt(v)
	if !ok {
		return 0, typeErr("* wants int")
	}
	if !i.IsInt64() || i.Int64() > limit || i.Int64() < -limit-1 {
		return 0, newPyErr("OverflowError", "Python int too large to convert to C "+cType)
	}
	return i.Int64(), nil
}

// parse reads the key, flags, width, precision and length modifier of a
// specifier (unicode_format_arg_parse).
func (c *pctFormatter) parse(a *pctArg) error {
	if c.ch == '(' {
		if c.dict == nil {
			return typeErr("format requires a mapping")
		}
		c.pos++
		start, depth := c.pos, 1
		for depth > 0 && c.next() {
			switch c.ch {
			case ')':
				depth--
			case '(':
				depth++
			}
		}
		if c.eof || depth > 0 {
			return valueErr("incomplete format key")
		}
		var key Value = strV(textutil.FromRunes(c.f[start : c.pos-1]))
		if c.bytes {
			b := make([]byte, 0, c.pos-1-start)
			for _, r := range c.f[start : c.pos-1] {
				b = append(b, byte(r))
			}
			key = bytesV(b)
		}
		v, err := fmtMappingItem(c.dict, key)
		if err != nil {
			return err
		}
		c.args, c.arglen, c.argidx = v, -1, -2
	}
flags:
	for c.next() {
		switch c.ch {
		case '-':
			a.flags |= pctLJust
		case '+':
			a.flags |= pctSign
		case ' ':
			a.flags |= pctBlank
		case '#':
			a.flags |= pctAlt
		case '0':
			a.flags |= pctZero
		default:
			break flags
		}
	}
	if c.ch == '*' {
		w, err := c.starArg(math.MaxInt64, "ssize_t")
		if err != nil {
			return err
		}
		if w < 0 {
			a.flags |= pctLJust
			w = -w
		}
		a.width = int(w)
		c.next()
	} else if c.ch >= '0' && c.ch <= '9' {
		a.width = int(c.ch - '0')
		for c.next() && c.ch >= '0' && c.ch <= '9' {
			d := int(c.ch - '0')
			if a.width > (math.MaxInt64-d)/10 {
				return valueErr("width too big")
			}
			a.width = a.width*10 + d
		}
	}
	if c.ch == '.' {
		a.prec = 0
		c.next()
		if c.ch == '*' {
			p, err := c.starArg(math.MaxInt32, "int")
			if err != nil {
				return err
			}
			a.prec = int(max(p, 0))
			c.next()
		} else if c.ch >= '0' && c.ch <= '9' {
			a.prec = int(c.ch - '0')
			for c.next() && c.ch >= '0' && c.ch <= '9' {
				d := int(c.ch - '0')
				if a.prec > (math.MaxInt32-d)/10 {
					return valueErr("precision too big")
				}
				a.prec = a.prec*10 + d
			}
		}
	}
	if !c.eof && (c.ch == 'h' || c.ch == 'l' || c.ch == 'L') {
		c.next()
	}
	if c.eof {
		return valueErr("incomplete format")
	}
	a.ch = c.ch
	return nil
}

// convert converts the next argument (unicode_format_arg_format). done
// reports that s is final and needs no padding.
func (c *pctFormatter) convert(a *pctArg) (s string, done bool, err error) {
	v, err := c.nextArg()
	if err != nil {
		return "", false, err
	}
	if c.bytes {
		switch a.ch {
		case 's', 'b':
			b, ok := bytesLike(v)
			if !ok {
				return "", false, typeErr("%%b requires a bytes-like object, or an object that implements __bytes__, not '%s'", typeName(v))
			}
			return latin1(b), false, nil
		case 'r', 'a':
			s, err = fmtASCII(v)
			return s, false, err
		case 'c':
			b, err := byteConverter(v)
			return string(rune(b)), false, err
		}
	}
	switch a.ch {
	case 's', 'r', 'a':
		if i, ok := v.(intV); ok && a.width == -1 && a.prec == -1 {
			s, err = intString(i.v)
			return s, true, err
		}
		switch a.ch {
		case 's':
			s, err = pyStr(v)
		case 'r':
			s, err = pyRepr(v)
		default:
			s, err = fmtASCII(v)
		}
		return s, false, err
	case 'i', 'd', 'u', 'o', 'x', 'X':
		a.sign = true
		s, err = pctLong(v, a)
		return s, false, err
	case 'e', 'E', 'f', 'F', 'g', 'G':
		x, err := fmtAsDouble(v)
		if err != nil {
			return "", false, err
		}
		prec := a.prec
		if prec < 0 {
			prec = 6
		}
		a.sign = true
		return fmtFloat(x, byte(a.ch), prec, a.flags&pctAlt != 0, false, false), false, nil
	case 'c':
		r, err := pctChar(v)
		return string(textutil.AppendRune(nil, r)), false, err
	}
	name := '?'
	if a.ch >= 31 && a.ch <= 126 {
		name = a.ch
	}
	return "", false, valueErr("unsupported format character '%c' (0x%x) at index %d", name, a.ch, c.pos-1)
}

// pctLong converts an argument of %d, %i, %u, %o, %x or %X: mainformatlong
// and _PyUnicode_FormatLong.
func pctLong(v Value, a *pctArg) (string, error) {
	i, ok := asInt(v)
	if !ok {
		var err error
		if fmtIsNumber(v) {
			if a.ch == 'o' || a.ch == 'x' || a.ch == 'X' {
				err = typeErr("'%s' object cannot be interpreted as an integer", v.pyType())
			} else {
				i, err = fmtPyInt(v)
			}
		}
		if !fmtIsNumber(v) || isExc(err, "TypeError") {
			if a.ch == 'o' || a.ch == 'x' || a.ch == 'X' {
				return "", typeErr("%%%c format: an integer is required, not %s", a.ch, v.pyType())
			}
			return "", typeErr("%%%c format: a real number is required, not %s", a.ch, v.pyType())
		}
		if err != nil {
			return "", err
		}
	}
	if a.prec > math.MaxInt32-3 {
		return "", newPyErr("OverflowError", "precision too large")
	}
	var digits, prefix string
	abs := new(big.Int).Abs(i)
	switch a.ch {
	case 'o':
		digits, prefix = abs.Text(8), "0o"
	case 'x':
		digits, prefix = abs.Text(16), "0x"
	case 'X':
		digits, prefix = strings.ToUpper(abs.Text(16)), "0X"
	default:
		s, err := intString(i)
		if err != nil {
			return "", err
		}
		digits = strings.TrimPrefix(s, "-")
	}
	if a.flags&pctAlt == 0 {
		prefix = ""
	}
	if a.prec > len(digits) {
		digits = strings.Repeat("0", a.prec-len(digits)) + digits
	}
	if i.Sign() < 0 {
		prefix = "-" + prefix
	}
	return prefix + digits, nil
}

// pctChar converts the argument of %c (formatchar).
func pctChar(v Value) (rune, error) {
	if s, ok := isStr(v); ok {
		if textutil.RuneCount(s) == 1 {
			r, _ := textutil.DecodeRune(s)
			return r, nil
		}
	} else if i, ok := asInt(v); ok {
		if !i.IsInt64() || i.Int64() < 0 || i.Int64() > 0x10ffff {
			return 0, newPyErr("OverflowError", "%c arg not in range(0x110000)")
		}
		return rune(i.Int64()), nil
	}
	return 0, typeErr("%%c requires int or char")
}

// bytesLike is the content of a bytes-like object, and whether v is one.
func bytesLike(v Value) (string, bool) {
	switch x := v.(type) {
	case bytesV:
		return string(x), true
	case *bytearrayV:
		return string(x.b), true
	}
	return "", false
}

// latin1 holds each byte of b as the character of the same code point.
func latin1(b string) string {
	r := make([]rune, len(b))
	for i := 0; i < len(b); i++ {
		r[i] = rune(b[i])
	}
	return string(r)
}

// byteConverter is the argument of bytes formatting's %c: a single byte, or
// an int in range(256).
func byteConverter(v Value) (byte, error) {
	if b, ok := bytesLike(v); ok && len(b) == 1 {
		return b[0], nil
	}
	i, ok := asInt(v)
	if !ok {
		return 0, typeErr("%%c requires an integer in range(256) or a single byte")
	}
	if !i.IsInt64() || i.Int64() < 0 || i.Int64() > 255 {
		return 0, newPyErr("OverflowError", "%c arg not in range(256)")
	}
	return byte(i.Int64()), nil
}

// output pads, signs and truncates a converted argument
// (unicode_format_arg_output).
func (c *pctFormatter) output(a *pctArg, s string) error {
	fill := " "
	if a.sign && a.flags&pctZero != 0 {
		fill = "0"
	}
	r := textutil.Runes(s)
	n := len(r)
	if (a.width == -1 || a.width <= n) && (a.prec == -1 || a.prec >= n) && a.flags&(pctSign|pctBlank) == 0 {
		c.out.WriteString(s)
		return nil
	}
	if a.width > fmtMaxWidth {
		return fmtMemoryErr()
	}
	if (a.ch == 's' || a.ch == 'r' || a.ch == 'a' || c.bytes && a.ch == 'b') && a.prec >= 0 && n > a.prec {
		n = a.prec
	}
	p := 0
	var sign string
	if a.sign {
		switch {
		case r[0] == '-' || r[0] == '+':
			sign = string(r[0])
			n--
			p++
		case a.flags&pctSign != 0:
			sign = "+"
		case a.flags&pctBlank != 0:
			sign = " "
		default:
			a.sign = false
		}
	}
	width := max(a.width, n)
	if a.sign {
		if fill != " " {
			c.out.WriteString(sign)
		}
		if width > n {
			width--
		}
	}
	alt := a.flags&pctAlt != 0 && (a.ch == 'x' || a.ch == 'X' || a.ch == 'o')
	if alt {
		if fill != " " {
			c.out.WriteString(textutil.FromRunes(r[p : p+2]))
			p += 2
		}
		width = max(width-2, 0)
		n -= 2
	}
	if width > n && a.flags&pctLJust == 0 {
		c.out.WriteString(strings.Repeat(fill, width-n))
		width = n
	}
	if fill == " " {
		if a.sign {
			c.out.WriteString(sign)
		}
		if alt {
			c.out.WriteString(textutil.FromRunes(r[p : p+2]))
			p += 2
		}
	}
	c.out.WriteString(textutil.FromRunes(r[p : p+n]))
	if width > n {
		c.out.WriteString(strings.Repeat(" ", width-n))
	}
	return nil
}

// fmtHasGetitem reports whether the value's type defines __getitem__.
func fmtHasGetitem(v Value) bool {
	switch x := v.(type) {
	case *dictV, *listV, *tupleV, strV, rangeV, bytesV, *bytearrayV:
		return true
	case *Object:
		return x.GetItem != nil
	}
	return false
}

// fmtIsMapping is PyMapping_Check for the right operand of %: tuples are
// excluded, and strs (str formatting) or bytes-like objects (bytes
// formatting) as PyUnicode_Format and _PyBytes_FormatEx exclude them.
func fmtIsMapping(v Value, bytesFormat bool) bool {
	switch v.(type) {
	case *tupleV:
		return false
	case strV:
		if !bytesFormat {
			return false
		}
	case bytesV, *bytearrayV:
		if bytesFormat {
			return false
		}
	}
	return fmtHasGetitem(v)
}

// fmtMappingItem is mapping[key] for a %(key)s specifier.
func fmtMappingItem(m Value, key Value) (Value, error) {
	switch x := m.(type) {
	case *dictV:
		v, ok, err := x.get(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, newExc("KeyError", []Value{key})
		}
		return v, nil
	}
	return pyGetitem(m, key)
}

// fmtIsNumber is PyNumber_Check: the type converts with int() or float().
func fmtIsNumber(v Value) bool {
	switch v.(type) {
	case intV, boolV, floatV:
		return true
	}
	return false
}

// fmtAsDouble is PyFloat_AsDouble.
func fmtAsDouble(v Value) (float64, error) {
	return toFloat(v)
}

// fmtPyInt is the builtin int(v) with base 10.
func fmtPyInt(v Value) (*big.Int, error) {
	switch x := v.(type) {
	case intV, boolV:
		i, _ := asInt(x)
		return i, nil
	case floatV:
		i, err := textutil.FloatToInt(float64(x))
		return i, fromLeaf(err)
	case strV:
		return fmtParseInt(string(x), x)
	}
	return nil, typeErr("int() argument must be a string, a bytes-like object or a real number, not '%s'", v.pyType())
}

// fmtParseInt is int(v) for a str v with text s.
func fmtParseInt(s string, v Value) (*big.Int, error) {
	n, err := pyParseIntOf(s, 10, v)
	if err != nil {
		return nil, err
	}
	i, _ := asInt(n)
	return i, nil
}

// fmtASCII is the builtin ascii(): repr() with non-ASCII characters escaped.
func fmtASCII(v Value) (string, error) {
	s, err := pyRepr(v)
	if err != nil {
		return "", err
	}
	return fmtASCIIEscape(s), nil
}

func fmtASCIIEscape(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range textutil.Runes(s) {
		if r < 0x80 {
			b = append(b, byte(r))
		} else {
			b = textutil.AppendBackslashEscape(b, r)
		}
	}
	return string(b)
}

// ---------------------------------------------------------------- str.format

// fmtHooks customise the replacement-field engine: string.Formatter's variant is kept for
// the shared field engine.
type fmtHooks struct {
	// getattr resolves `{0.name}`.
	getattr func(obj Value, name string) (Value, error)
	// getitem resolves `{0[key]}`: key is an int Value if all digits, else a str.
	getitem func(obj Value, key Value) (Value, error)
	// field formats one replacement field; nil means formatValue.
	field func(v Value, spec string) (string, error)
	// mapping, when set, is format_map's argument: keyword fields are looked
	// up in it with Python's subscription, whatever its type.
	mapping Value
	// pyFormatter selects string.Formatter().vformat's behaviour over
	// str.format's: numbering is decided on the whole field name, a missing
	// positional argument is "tuple index out of range", every format spec
	// is expanded (so recursion errors come later) and unknown conversions
	// are named verbatim.
	pyFormatter bool
}

// fmtEngine is the state of one str.format call.
type fmtEngine struct {
	args   []Value
	kwargs *dictV
	h      fmtHooks
	// str.format numbering (AutoNumber): state is 0 before the first
	// numeric field, then 'a'uto or 'm'anual.
	state   byte
	autoNum int
	// string.Formatter numbering: autoOff is auto_arg_index being False.
	autoOff bool
}

// fmtField is one replacement field: {name!conv:spec}.
type fmtField struct {
	name   []rune
	conv   rune
	spec   []rune
	expand bool // spec holds a nested field
}

// strFormat is str.format(*args, **kwargs), or string.Formatter().vformat
// when h.pyFormatter is set.
func strFormat(format string, args []Value, kwargs *dictV, h fmtHooks) (string, error) {
	e := &fmtEngine{args: args, kwargs: kwargs, h: h}
	return e.build(textutil.Runes(format), 2)
}

// build formats s (build_string / Formatter._vformat).
func (e *fmtEngine) build(s []rune, depth int) (string, error) {
	if (depth <= 0 && !e.h.pyFormatter) || depth < 0 {
		return "", valueErr("Max string recursion exceeded")
	}
	var b strings.Builder
	p := 0
	for p < len(s) {
		lit, f, err := fmtMarkupNext(s, &p)
		if err != nil {
			return "", err
		}
		b.WriteString(lit)
		if f == nil {
			continue
		}
		out, err := e.field(f, depth)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// fmtMarkupNext returns the literal text up to the next replacement field
// and that field, nil at the end of s (MarkupIterator_next).
func fmtMarkupNext(s []rune, p *int) (string, *fmtField, error) {
	start := *p
	var c rune
	markup := false
	for *p < len(s) {
		c = s[*p]
		*p++
		if c == '{' || c == '}' {
			markup = true
			break
		}
	}
	atEnd := *p >= len(s)
	n := *p - start
	if c == '}' && (atEnd || s[*p] != '}') {
		return "", nil, valueErr("Single '}' encountered in format string")
	}
	if atEnd && c == '{' {
		return "", nil, valueErr("Single '{' encountered in format string")
	}
	if !atEnd {
		if c == s[*p] {
			*p++ // escaped brace
			markup = false
		} else {
			n--
		}
	}
	lit := textutil.FromRunes(s[start : start+n])
	if !markup {
		return lit, nil, nil
	}
	f, err := fmtParseField(s, p)
	return lit, f, err
}

// fmtParseField parses a replacement field after its '{' (parse_field).
func fmtParseField(s []rune, p *int) (*fmtField, error) {
	f := &fmtField{}
	start := *p
	var c rune
name:
	for *p < len(s) {
		c = s[*p]
		*p++
		switch c {
		case '{':
			return nil, valueErr("unexpected '{' in field name")
		case '[':
			for *p < len(s) && s[*p] != ']' {
				*p++
			}
		case '}', ':', '!':
			break name
		}
	}
	f.name = s[start : *p-1]
	if c != '!' && c != ':' {
		if c != '}' {
			return nil, valueErr("expected '}' before end of string")
		}
		return f, nil
	}
	if c == '!' {
		if *p >= len(s) {
			return nil, valueErr("end of string while looking for conversion specifier")
		}
		f.conv = s[*p]
		*p++
		if *p < len(s) {
			c = s[*p]
			*p++
			if c == '}' {
				return f, nil
			}
			if c != ':' {
				return nil, valueErr("expected ':' after conversion specifier")
			}
		}
	}
	specStart, depth := *p, 1
	for *p < len(s) {
		c = s[*p]
		*p++
		switch c {
		case '{':
			f.expand = true
			depth++
		case '}':
			depth--
			if depth == 0 {
				f.spec = s[specStart : *p-1]
				return f, nil
			}
		}
	}
	return nil, valueErr("unmatched '{' in format spec")
}

// field formats one replacement field (output_markup, or the loop body of
// Formatter._vformat).
func (e *fmtEngine) field(f *fmtField, depth int) (string, error) {
	obj, err := e.fieldObject(f.name)
	if err != nil {
		return "", err
	}
	if f.conv != 0 {
		if obj, err = e.convert(obj, f.conv); err != nil {
			return "", err
		}
	}
	spec := textutil.FromRunes(f.spec)
	if f.expand || e.h.pyFormatter {
		if spec, err = e.build(f.spec, depth-1); err != nil {
			return "", err
		}
	}
	if e.h.field != nil {
		return e.h.field(obj, spec)
	}
	return formatValue(obj, spec)
}

// convert applies a !r, !s or !a conversion.
func (e *fmtEngine) convert(v Value, conv rune) (Value, error) {
	var s string
	var err error
	switch conv {
	case 'r':
		s, err = pyRepr(v)
	case 's':
		s, err = pyStr(v)
	case 'a':
		s, err = fmtASCII(v)
	default:
		if e.h.pyFormatter || conv > 32 && conv < 127 {
			return nil, valueErr("Unknown conversion specifier %c", conv)
		}
		return nil, valueErr(`Unknown conversion specifier \x%x`, conv)
	}
	if err != nil {
		return nil, err
	}
	return strV(s), nil
}

// fmtFieldIndex is get_integer in unicode_format.h: the value of an
// all-decimal-digits name, or -1.
func fmtFieldIndex(s []rune) (int, error) {
	if len(s) == 0 {
		return -1, nil
	}
	acc := 0
	for _, r := range s {
		d := textutil.Decimal(r)
		if d < 0 {
			return -1, nil
		}
		if acc > (math.MaxInt64-d)/10 {
			return -1, valueErr("Too many decimal digits in format string")
		}
		acc = acc*10 + d
	}
	return acc, nil
}

// fmtIsDigits is str.isdigit().
func fmtIsDigits(s []rune) bool {
	return pyIsDigit(textutil.FromRunes(s))
}

// fieldObject looks up the object a field name refers to
// (get_field_object, or Formatter.get_field after _vformat's numbering).
func (e *fmtEngine) fieldObject(name []rune) (Value, error) {
	if e.h.pyFormatter {
		switch {
		case len(name) == 0:
			if e.autoOff {
				return nil, valueErr("cannot switch from manual field specification to automatic field numbering")
			}
			name = []rune(strconv.Itoa(e.autoNum))
			e.autoNum++
		case fmtIsDigits(name):
			if !e.autoOff && e.autoNum > 0 {
				return nil, valueErr("cannot switch from manual field specification to automatic field numbering")
			}
			e.autoOff = true
		}
	}
	split := 0
	for split < len(name) && name[split] != '.' && name[split] != '[' {
		split++
	}
	first, rest := name[:split], name[split:]
	idx, err := fmtFieldIndex(first)
	if err != nil {
		return nil, err
	}
	if !e.h.pyFormatter && (len(first) == 0 || idx != -1) {
		empty := len(first) == 0
		if e.state == 0 {
			e.state = 'm'
			if empty {
				e.state = 'a'
			}
		}
		if e.state == 'm' && empty {
			return nil, valueErr("cannot switch from manual field specification to automatic field numbering")
		}
		if e.state == 'a' && !empty {
			return nil, valueErr("cannot switch from automatic field numbering to manual field specification")
		}
		if empty {
			idx = e.autoNum
			e.autoNum++
		}
	}
	var obj Value
	switch {
	case idx == -1:
		key := strV(string(first))
		var ok bool
		if e.h.mapping != nil {
			if obj, err = pyGetitem(e.h.mapping, key); err != nil {
				return nil, err
			}
			ok = true
		} else if e.kwargs != nil {
			if obj, ok, err = e.kwargs.get(key); err != nil {
				return nil, err
			}
		}
		if !ok {
			return nil, newExc("KeyError", []Value{key})
		}
	case idx < len(e.args):
		obj = e.args[idx]
	case e.h.pyFormatter:
		return nil, newPyErr("IndexError", "tuple index out of range")
	default:
		return nil, newPyErr("IndexError", "Replacement index "+strconv.Itoa(idx)+" out of range for positional args tuple")
	}
	return e.lookupRest(obj, rest)
}

// lookupRest follows the .attr and [key] parts of a field name
// (FieldNameIterator_next).
func (e *fmtEngine) lookupRest(obj Value, rest []rune) (Value, error) {
	for j := 0; j < len(rest); {
		c := rest[j]
		j++
		start := j
		var name []rune
		idx := -1
		switch c {
		case '.':
			for j < len(rest) && rest[j] != '.' && rest[j] != '[' {
				j++
			}
			name = rest[start:j]
		case '[':
			for j < len(rest) && rest[j] != ']' {
				j++
			}
			if j == len(rest) {
				return nil, valueErr("Missing ']' in format string")
			}
			name = rest[start:j]
			j++
			var err error
			if idx, err = fmtFieldIndex(name); err != nil {
				return nil, err
			}
		default:
			return nil, valueErr("Only '.' or '[' may follow ']' in format field specifier")
		}
		if len(name) == 0 {
			return nil, valueErr("Empty attribute in format string")
		}
		var err error
		switch {
		case c == '.' && e.h.getattr != nil:
			obj, err = e.h.getattr(obj, textutil.FromRunes(name))
		case c == '.':
			err = newPyErr("AttributeError", "'"+obj.pyType()+"' object has no attribute "+textutil.Repr(textutil.FromRunes(name)))
		case e.h.getitem == nil:
			err = typeErr("'%s' object is not subscriptable", obj.pyType())
		case idx != -1:
			obj, err = e.h.getitem(obj, mkInt(int64(idx)))
		default:
			obj, err = e.h.getitem(obj, strV(string(name)))
		}
		if err != nil {
			return nil, err
		}
	}
	return obj, nil
}
