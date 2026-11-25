// Ported from CPython 3.12 (Parser/action_helpers.c, Parser/pegen.c, Parser/string_parser.c) to
// Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The actions of the grammar rules: building the nodes of the tree, the
// values of number and string literals, f-strings, and the helpers the
// invalid_* rules use to name what went wrong.

package py

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/codecs"
	"github.com/danthedeckie/simpleeval/internal/pyexc"
	"github.com/danthedeckie/simpleeval/internal/textutil"
)

// The helper values of the rules (pegen.h).
type (
	kvPair  struct{ key, value *node }
	cmpPair struct {
		op   string
		expr *node
	}
	nameDefault  struct{ arg, value *node }
	slashDefault struct {
		plain    []*node
		defaults []*nameDefault
	}
	starEtc struct {
		vararg *node
		kwonly []*nameDefault
		kwarg  *node
	}
	kwOrStar struct {
		elem      *node
		isKeyword bool
	}
	keyPattern struct{ key, pattern *node }
	// withMeta is a node with the debug text of an f-string replacement
	// field (ResultTokenWithMetadata).
	withMeta struct {
		res  *node
		meta *string
	}
	augOp struct{ op string }
)

// span is a node of the kind spanning the tokens from start to the last one
// read (EXTRA).
func (p *parser) span(kind string, start int) *node {
	s, e := p.toks[start], p.lastToken()
	return &node{kind: kind, lineno: s.line, col: s.col, endLineno: e.endLine, endCol: e.endCol, hasPos: true}
}

// tokNode is a node of the kind spanning one token.
func tokNode(kind string, t *ptok) *node {
	return &node{kind: kind, lineno: t.line, col: t.col, endLineno: t.endLine, endCol: t.endCol, hasPos: true}
}

// nameNode is the Name node of a NAME token, its identifier normalised to
// NFKC (_PyPegen_name_from_token).
func nameNode(t *ptok) *node {
	n := tokNode("Name", t)
	n.id, n.ctx = t.text, "Load"
	if !textutil.IsASCII(t.text) {
		n.id = textutil.NFKC(t.text)
	}
	return n
}

// name matches a NAME token (_PyPegen_name_token).
func (p *parser) name() *node {
	t := p.expect(tNAME)
	if t == nil {
		return nil
	}
	return nameNode(t)
}

// softKeywordToken matches any soft keyword (SOFT_KEYWORD).
func (p *parser) softKeywordToken() *node {
	t := p.expect(tNAME)
	if t == nil || !softKeywords[t.text] {
		return nil
	}
	return nameNode(t)
}

// setCtx is a copy of e with the context ctx, for the target kinds
// (_PyPegen_set_expr_context).
func setCtx(e *node, ctx string) *node {
	switch e.kind {
	case "Name", "Subscript", "Attribute":
		c := *e
		c.ctx = ctx
		return &c
	case "Tuple", "List":
		c := *e
		c.ctx = ctx
		c.l1 = make([]*node, len(e.l1))
		for i, x := range e.l1 {
			c.l1[i] = setCtx(x, ctx)
		}
		return &c
	case "Starred":
		c := *e
		c.ctx = ctx
		c.a = setCtx(e.a, ctx)
		return &c
	}
	return e
}

// exprNames are the words for the kinds of expressions in error messages
// (_PyPegen_get_expr_name).
var exprNames = map[string]string{
	"Attribute": "attribute", "Subscript": "subscript", "Starred": "starred", "Name": "name",
	"List": "list", "Tuple": "tuple", "Lambda": "lambda", "Call": "function call",
	"BoolOp": "expression", "BinOp": "expression", "UnaryOp": "expression",
	"GeneratorExp": "generator expression", "Yield": "yield expression", "YieldFrom": "yield expression",
	"Await": "await expression", "ListComp": "list comprehension", "SetComp": "set comprehension",
	"DictComp": "dict comprehension", "Dict": "dict literal", "Set": "set display",
	"JoinedStr": "f-string expression", "FormattedValue": "f-string expression",
	"Compare": "comparison", "IfExp": "conditional expression", "NamedExpr": "named expression",
}

func exprName(e *node) string {
	if e.kind == "Constant" {
		switch e.value {
		case none:
			return "None"
		case falseV:
			return "False"
		case trueV:
			return "True"
		case ellipsisV{}:
			return "ellipsis"
		}
		return "literal"
	}
	return exprNames[e.kind]
}

// Target kinds for invalidTarget.
const (
	starTargets = iota
	delTargets
	forTargets
)

// invalidTarget is the part of e that cannot be an assignment, deletion or
// for target, or nil (_PyPegen_get_invalid_target).
func invalidTarget(e *node, kind int) *node {
	if e == nil {
		return nil
	}
	switch e.kind {
	case "List", "Tuple":
		for _, x := range e.l1 {
			if c := invalidTarget(x, kind); c != nil {
				return c
			}
		}
		return nil
	case "Starred":
		if kind == delTargets {
			return e
		}
		return invalidTarget(e.a, kind)
	case "Compare":
		// The "a in b" of "for a in b" parses as a comparison: its left
		// side is the target.
		if kind == forTargets {
			if e.ops[0] == "In" {
				return invalidTarget(e.a, kind)
			}
			return nil
		}
		return e
	case "Name", "Subscript", "Attribute":
		return nil
	}
	return e
}

// raiseInvalidTarget is RAISE_SYNTAX_ERROR_INVALID_TARGET: "cannot assign
// to ..." or "cannot delete ..." for the invalid part of e, if any.
func (p *parser) raiseInvalidTarget(kind int, e *node) {
	t := invalidTarget(e, kind)
	if t == nil {
		return
	}
	verb := "cannot assign to "
	if kind == delTargets {
		verb = "cannot delete "
	}
	p.raiseAt("", t.lineno, verb+exprName(t))
}

// checkLegacyStmt reports whether name is print or exec, the statements of
// Python 2.
func checkLegacyStmt(name *node) bool {
	return name.kind == "Name" && (name.id == "print" || name.id == "exec")
}

// number matches a NUMBER token and makes its Constant
// (_PyPegen_number_token).
func (p *parser) number() *node {
	t := p.expect(tNUMBER)
	if t == nil {
		return nil
	}
	v, err := parseNumber(t.text)
	if err != nil {
		p.raiseAt("", t.line, err.Error())
		return nil
	}
	n := tokNode("Constant", t)
	n.value = v
	return n
}

// parseNumber converts the text of a NUMBER token to its value.
func parseNumber(text string) (Value, error) {
	s := strings.ReplaceAll(text, "_", "")
	last := s[len(s)-1]
	if last == 'j' || last == 'J' {
		f, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil && !isRangeErr(err) {
			return nil, err
		}
		return complexV(complex(0, f)), nil
	}
	if len(s) > 1 && s[0] == '0' && strings.IndexByte("xXoObB", s[1]) >= 0 {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]]
		n, _ := new(big.Int).SetString(s[2:], base)
		return intV{n}, nil
	}
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !isRangeErr(err) {
			return nil, err
		}
		return floatV(f), nil
	}
	if len(s) > pyexc.MaxStrDigits {
		return nil, &syntaxErr{msg: pyexc.DigitLimit(len(s)).Msg +
			" - Consider hexadecimal for huge integer literals to avoid decimal conversion limits."}
	}
	n, _ := new(big.Int).SetString(s, 10)
	return intV{n}, nil
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// warnInvalidEscape issues the SyntaxWarning for the first invalid escape
// sequence of a literal, in the first pass only. The tokenizer has already
// warned about a backslash before a brace in an f-string.
func (p *parser) warnInvalidEscape(invalid string, fstring bool) {
	if invalid == "" || p.invalid {
		return
	}
	c := invalid[0]
	if fstring && (c == '{' || c == '}') {
		return
	}
	if c >= '4' && c <= '7' {
		Warn("SyntaxWarning", "invalid octal escape sequence '\\"+invalid+"'")
		return
	}
	Warn("SyntaxWarning", "invalid escape sequence '\\"+string(c)+"'")
}

// decodeString decodes the body of a str literal or f-string part
// (_PyPegen_decode_string); an error is raised as the "(unicode error)"
// SyntaxError.
func (p *parser) decodeString(raw bool, s string, fstring bool) (string, bool) {
	if raw || strings.IndexByte(s, '\\') < 0 {
		return s, true
	}
	v, invalid, err := decodeStrBody(s, false)
	if err != nil {
		p.raiseDecodeError(err)
		return "", false
	}
	p.warnInvalidEscape(invalid, fstring)
	return v, true
}

// raiseDecodeError raises the error of decoding the escapes of a literal as
// a SyntaxError (_Pypegen_raise_decode_error).
func (p *parser) raiseDecodeError(err error) {
	kind := "value error"
	if _, ok := err.(*codecs.Error); ok {
		kind = "unicode error"
	}
	p.raise("(" + kind + ") " + err.Error())
}

// constantFromString is the Constant of a STRING token
// (_PyPegen_constant_from_string and _PyPegen_parse_string).
func (p *parser) constantFromString(t *ptok) *node {
	s := t.text
	i := 0
	bytesMode, rawMode := false, false
	for ; s[i] != '\'' && s[i] != '"'; i++ {
		switch s[i] {
		case 'b', 'B':
			bytesMode = true
		case 'r', 'R':
			rawMode = true
		}
	}
	q := 1
	if len(s)-i >= 6 && s[i+1] == s[i] && s[i+2] == s[i] {
		q = 3
	}
	body := s[i+q : len(s)-q]
	n := tokNode("Constant", t)
	if s[0] == 'u' {
		n.id = "u"
	}
	if !bytesMode {
		v, ok := p.decodeString(rawMode, body, false)
		if !ok {
			return nil
		}
		n.value = strV(v)
		return n
	}
	if !textutil.IsASCII(body) {
		p.raiseAt("", t.line, "bytes can only contain ASCII literal characters")
		return nil
	}
	if rawMode || strings.IndexByte(body, '\\') < 0 {
		n.value = bytesV(body)
		return n
	}
	v, invalid, err := decodeStrBody(body, true)
	if err != nil {
		p.raiseDecodeError(err)
		return nil
	}
	p.warnInvalidEscape(invalid, false)
	n.value = bytesV(v)
	return n
}

// concatenateStrings joins adjacent string literals and f-strings into one
// Constant or JoinedStr spanning start to the last token read
// (_PyPegen_concatenate_strings).
func (p *parser) concatenateStrings(strs []*node, start int) *node {
	return p.concatAt(strs, p.span("JoinedStr", start))
}

// concatAt is concatenateStrings with the position of the result given by
// at.
func (p *parser) concatAt(strs []*node, at *node) *node {
	fstringFound, unicodeFound, bytesFound := false, false, false
	for _, e := range strs {
		switch e.kind {
		case "Constant":
			if _, ok := e.value.(bytesV); ok {
				bytesFound = true
			} else {
				unicodeFound = true
			}
		default:
			fstringFound = true
		}
	}
	if (unicodeFound || fstringFound) && bytesFound {
		p.raise("cannot mix bytes and nonbytes literals")
		return nil
	}
	if bytesFound {
		var b strings.Builder
		for _, e := range strs {
			b.WriteString(string(e.value.(bytesV)))
		}
		n := *at
		n.kind, n.value, n.id = "Constant", bytesV(b.String()), strs[0].id
		return &n
	}
	if !fstringFound && len(strs) == 1 {
		return strs[0]
	}

	var flat []*node
	for _, e := range strs {
		if e.kind == "JoinedStr" {
			flat = append(flat, e.l1...)
		} else {
			flat = append(flat, e)
		}
	}
	// Fold runs of constants; drop empty ones next to f-string parts.
	var values []*node
	for i := 0; i < len(flat); i++ {
		e := flat[i]
		if e.kind == "Constant" {
			if i+1 < len(flat) && flat[i+1].kind == "Constant" {
				first, last := e, e
				var b strings.Builder
				j := i
				for ; j < len(flat) && flat[j].kind == "Constant"; j++ {
					b.WriteString(string(flat[j].value.(strV)))
					last = flat[j]
				}
				i = j - 1
				e = &node{kind: "Constant", value: strV(b.String()), id: first.id, lineno: first.lineno, col: first.col,
					endLineno: last.endLineno, endCol: last.endCol, hasPos: true}
			}
			if fstringFound && e.value.(strV) == "" {
				continue
			}
		}
		values = append(values, e)
	}
	if !fstringFound {
		return values[0]
	}
	n := *at
	n.kind, n.l1 = "JoinedStr", values
	return &n
}

// joinedStr is the JoinedStr of an f-string from its FSTRING_START token a
// to its FSTRING_END token b: debug fields are flattened, literal parts
// decoded and empty ones dropped (_PyPegen_joined_str).
func (p *parser) joinedStr(a *ptok, parts []*node, b *ptok) *node {
	raw := strings.ContainsAny(a.text, "rR")
	var values []*node
	for _, item := range parts {
		switch item.kind {
		case "JoinedStr":
			values = append(values, item.l1...)
			continue
		case "Constant":
			s := string(item.value.(strV))
			if s == "{{" || s == "}}" {
				s = s[:1]
			}
			v, ok := p.decodeString(raw, s, true)
			if !ok {
				return nil
			}
			if v == "" {
				continue
			}
			c := *item
			c.value = strV(v)
			item = &c
		}
		values = append(values, item)
	}
	return &node{kind: "JoinedStr", l1: values, lineno: a.line, col: a.col, endLineno: b.endLine, endCol: b.endCol,
		hasPos: true}
}

// constantFromToken is the Constant of the text of an f-string part
// (_PyPegen_constant_from_token).
func constantFromToken(t *ptok) *node {
	n := tokNode("Constant", t)
	n.value = strV(t.text)
	return n
}

// decodedConstantFromToken is the Constant of a part of a format spec,
// its escapes decoded even in a raw f-string
// (_PyPegen_decoded_constant_from_token). A decoding error here is raised
// as the UnicodeDecodeError itself, not as a SyntaxError.
func (p *parser) decodedConstantFromToken(t *ptok) *node {
	v := t.text
	if strings.IndexByte(v, '\\') >= 0 {
		d, invalid, err := decodeStrBody(v, false)
		if err != nil {
			if !p.errInd || p.exc == nil {
				p.errInd = true
				p.exc = &syntaxErr{msg: err.Error(), leaf: err}
			}
			return nil
		}
		p.warnInvalidEscape(invalid, true)
		v = d
	}
	n := tokNode("Constant", t)
	n.value = strV(v)
	return n
}

// checkFStringConversion checks that the conversion character follows the
// '!' directly (_PyPegen_check_fstring_conversion).
func (p *parser) checkFStringConversion(bang *ptok, conv *node) *withMeta {
	if bang.line != conv.lineno || bang.endCol != conv.col {
		p.raiseAt("", bang.line, "f-string: conversion type must come right after the exclamanation mark")
		return nil
	}
	return &withMeta{res: conv, meta: bang.meta}
}

// setupFullFormatSpec is the JoinedStr of a format spec from ':' at
// colon (_PyPegen_setup_full_format_spec).
func (p *parser) setupFullFormatSpec(colon *ptok, spec []*node, start int) *withMeta {
	// An empty format spec is an empty JoinedStr, as in 3.11.
	if len(spec) == 1 && spec[0].kind == "Constant" {
		if s, ok := spec[0].value.(strV); ok && s == "" {
			spec = nil
		}
	}
	at := p.span("JoinedStr", start)
	var res *node
	if len(spec) == 0 || len(spec) == 1 && spec[0].kind == "Constant" {
		at.l1 = spec
		res = at
	} else if res = p.concatAt(spec, at); res == nil {
		return nil
	}
	return &withMeta{res: res, meta: colon.meta}
}

// formattedValue is the FormattedValue of a replacement field, preceded by
// the Constant of its text for a debug field (f"{x=}")
// (_PyPegen_formatted_value).
func (p *parser) formattedValue(expr *node, debug *ptok, conv, format *withMeta, rbrace *ptok, start int) *node {
	conversion := -1
	if conv != nil {
		id := conv.res.id
		if id != "s" && id != "r" && id != "a" {
			p.raiseAt("", conv.res.lineno, "f-string: invalid conversion character '"+id+
				"': expected 's', 'r', or 'a'")
			return nil
		}
		conversion = int(id[0])
	} else if debug != nil && format == nil {
		// A debug field without a conversion uses !r.
		conversion = 'r'
	}
	fv := p.span("FormattedValue", start)
	fv.a, fv.conversion = expr, conversion
	if format != nil {
		fv.b = format.res
	}
	if debug == nil {
		return fv
	}
	var endLine, endCol int
	var meta *string
	switch {
	case conv != nil:
		endLine, endCol, meta = conv.res.lineno, conv.res.col, conv.meta
	case format != nil:
		endLine, endCol, meta = format.res.lineno, format.res.col+1, format.meta
	default:
		endLine, endCol, meta = fv.endLineno, fv.endCol, rbrace.meta
	}
	text := &node{kind: "Constant", lineno: fv.lineno, col: fv.col + 1, endLineno: endLine, endCol: endCol - 1,
		hasPos: true, value: strV("")}
	if meta != nil {
		text.value = strV(*meta)
	}
	return &node{kind: "JoinedStr", l1: []*node{text, fv}, lineno: fv.lineno, col: fv.col, endLineno: endLine,
		endCol: endCol, hasPos: true}
}

// makeArguments builds the arguments node of a parameter list
// (_PyPegen_make_arguments).
func makeArguments(slashNoDefault []*node, slashWithDefault *slashDefault, plain []*node,
	withDefault []*nameDefault, star *starEtc) *node {
	args := &node{kind: "arguments"}
	switch {
	case slashNoDefault != nil:
		args.l1 = slashNoDefault
	case slashWithDefault != nil:
		args.l1 = append(append([]*node{}, slashWithDefault.plain...), defaultNames(slashWithDefault.defaults)...)
	}
	args.l2 = append(append([]*node{}, plain...), defaultNames(withDefault)...)
	if slashWithDefault != nil {
		args.body = defaultValues(slashWithDefault.defaults)
	}
	args.body = append(args.body, defaultValues(withDefault)...)
	if star != nil {
		args.a, args.b = star.vararg, star.kwarg
		args.l3, args.l4 = defaultNames(star.kwonly), defaultValues(star.kwonly)
	}
	return args
}

func defaultNames(ds []*nameDefault) []*node {
	var out []*node
	for _, d := range ds {
		out = append(out, d.arg)
	}
	return out
}

func defaultValues(ds []*nameDefault) []*node {
	var out []*node
	for _, d := range ds {
		out = append(out, d.value)
	}
	return out
}

// collectCallSeqs is the Call of the args rule: the positional arguments
// followed by the starred ones among the keywords
// (_PyPegen_collect_call_seqs).
func (p *parser) collectCallSeqs(args []*node, kws []*kwOrStar, start int) *node {
	c := p.span("Call", start)
	c.l1 = args
	if kws != nil {
		c.l1 = append(append([]*node{}, args...), starredOf(kws)...)
		c.l2 = keywordsOf(kws)
	}
	return c
}

// starredOf are the starred expressions of kws
// (_PyPegen_seq_extract_starred_exprs).
func starredOf(kws []*kwOrStar) []*node {
	var out []*node
	for _, k := range kws {
		if !k.isKeyword {
			out = append(out, k.elem)
		}
	}
	return out
}

// keywordsOf are the keywords of kws (_PyPegen_seq_delete_starred_exprs).
func keywordsOf(kws []*kwOrStar) []*node {
	var out []*node
	for _, k := range kws {
		if k.isKeyword {
			out = append(out, k.elem)
		}
	}
	return out
}

// nonparenGenexpInCall raises the error for a generator expression that is
// one of several arguments without parentheses
// (_PyPegen_nonparen_genexp_in_call).
func (p *parser) nonparenGenexpInCall(args *node) {
	if len(args.l1) <= 1 {
		return
	}
	p.raiseAt("", args.l1[len(args.l1)-1].lineno, "Generator expression must be parenthesized")
}

// argumentsParsingError raises the error for a positional argument after
// keyword arguments (_PyPegen_arguments_parsing_error).
func (p *parser) argumentsParsingError(call *node) {
	for _, k := range call.l2 {
		if k.noID {
			p.raise("positional argument follows keyword argument unpacking")
			return
		}
	}
	p.raise("positional argument follows keyword argument")
}

// checkBarryAsFlufl reports whether a NOTEQUAL token is the wrong spelling
// of "not equal": "<>" normally, "!=" after
// "from __future__ import barry_as_FLUFL", which also raises an error
// (_PyPegen_check_barry_as_flufl).
func (p *parser) checkBarryAsFlufl(t *ptok) bool {
	if p.barry && t.text != "<>" {
		p.raise("with Barry as BDFL, use '<>' instead of '!='")
		return true
	}
	return !p.barry && t.text != "!="
}
