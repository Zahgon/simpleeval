// Ported from CPython 3.12 (Grammar/python.gram) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The statement rules of the grammar. Each rule tries its alternatives in
// order, as CPython's generated parser does; the comment above a rule gives
// it as python.gram writes it.

package py

// file: [statements] ENDMARKER
func (p *parser) file() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	if optSeq(p, &a, p.statements()) && p.tok(tENDMARKER) {
		return &node{kind: "Module", body: a}
	}
	p.mark = m
	return nil
}

// statements: statement+
func (p *parser) statements() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	// statement+ is a loop rule of its own.
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var out []*node
	n := 0
	for {
		s := p.statement()
		if s == nil {
			break
		}
		out = append(out, s...)
		n++
		m = p.mark
	}
	p.mark = m
	if n == 0 || p.errInd {
		return nil
	}
	return out
}

// statement: compound_stmt | simple_stmts
func (p *parser) statement() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.compoundStmt(); a != nil {
		return []*node{a}
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.simpleStmts(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// simple_stmts:
//
//	| simple_stmt !';' NEWLINE
//	| ';'.simple_stmt+ [';'] NEWLINE
func (p *parser) simpleStmts() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *node
	if got(&a, p.simpleStmt()) && !p.at(tSEMI) && p.tok(tNEWLINE) {
		return []*node{a}
	}
	if !p.retry(m) {
		return nil
	}
	var l []*node
	if gotSeq(&l, gather(p, tSEMI, p.simpleStmt)) && p.optTok(tSEMI) && p.tok(tNEWLINE) {
		return l
	}
	p.mark = m
	return nil
}

// simple_stmt (memo):
//
//	| assignment
//	| &"type" type_alias
//	| star_expressions
//	| &'return' return_stmt
//	| &('import' | 'from') import_stmt
//	| &'raise' raise_stmt
//	| 'pass'
//	| &'del' del_stmt
//	| &'yield' yield_stmt
//	| &'assert' assert_stmt
//	| 'break'
//	| 'continue'
//	| &'global' global_stmt
//	| &'nonlocal' nonlocal_stmt
func (p *parser) simpleStmt() *node {
	return memo(p, rSimpleStmt, func() *node {
		m := p.mark
		if a := p.assignment(); a != nil {
			return a
		}
		if !p.retry(m) {
			return nil
		}
		if p.atSoftKeyword("type") {
			if a := p.typeAlias(); a != nil {
				return a
			}
		}
		if !p.retry(m) {
			return nil
		}
		if e := p.starExpressions(); e != nil {
			s := p.span("Expr", m)
			s.a = e
			return s
		}
		alts := []struct {
			look []int
			rule func() *node
		}{
			{[]int{kwReturn}, p.returnStmt},
			{[]int{kwImport, kwFrom}, p.importStmt},
			{[]int{kwRaise}, p.raiseStmt},
			{[]int{kwPass}, nil},
			{[]int{kwDel}, p.delStmt},
			{[]int{kwYield}, p.yieldStmt},
			{[]int{kwAssert}, p.assertStmt},
			{[]int{kwBreak}, nil},
			{[]int{kwContinue}, nil},
			{[]int{kwGlobal}, p.globalStmt},
			{[]int{kwNonlocal}, p.nonlocalStmt},
		}
		for _, alt := range alts {
			if !p.retry(m) {
				return nil
			}
			if alt.rule == nil {
				if p.tok(alt.look[0]) {
					return p.span(map[int]string{kwPass: "Pass", kwBreak: "Break", kwContinue: "Continue"}[alt.look[0]], m)
				}
				continue
			}
			if p.atAny(alt.look...) {
				if a := alt.rule(); a != nil {
					return a
				}
			}
		}
		p.mark = m
		return nil
	})
}

// compound_stmt:
//
//	| &('def' | '@' | ASYNC) function_def
//	| &'if' if_stmt
//	| &('class' | '@') class_def
//	| &('with' | ASYNC) with_stmt
//	| &('for' | ASYNC) for_stmt
//	| &'try' try_stmt
//	| &'while' while_stmt
//	| match_stmt
func (p *parser) compoundStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	alts := []struct {
		look []int
		rule func() *node
	}{
		{[]int{kwDef, tAT, tASYNC}, p.functionDef},
		{[]int{kwIf}, p.ifStmt},
		{[]int{kwClass, tAT}, p.classDef},
		{[]int{kwWith, tASYNC}, p.withStmt},
		{[]int{kwFor, tASYNC}, p.forStmt},
		{[]int{kwTry}, p.tryStmt},
		{[]int{kwWhile}, p.whileStmt},
	}
	for _, alt := range alts {
		if !p.retry(m) {
			return nil
		}
		if p.atAny(alt.look...) {
			if a := alt.rule(); a != nil {
				return a
			}
		}
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.matchStmt(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// assignment:
//
//	| NAME ':' expression ['=' annotated_rhs]
//	| ('(' single_target ')' | single_subscript_attribute_target) ':' expression ['=' annotated_rhs]
//	| (star_targets '=')+ (yield_expr | star_expressions) !'=' [TYPE_COMMENT]
//	| single_target augassign ~ (yield_expr | star_expressions)
//	| invalid_assignment
func (p *parser) assignment() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	eqRhs := func() *node {
		return group(p, func() *node {
			if p.tok(tEQUAL) {
				return p.annotatedRhs()
			}
			return nil
		})
	}
	var a, b, c *node
	if got(&a, p.name()) && p.tok(tCOLON) && got(&b, p.expression()) && opt(p, &c, eqRhs()) {
		n := p.span("AnnAssign", m)
		n.a, n.b, n.c, n.level = setCtx(a, "Store"), b, c, 1
		return n
	}
	if !p.retry(m) {
		return nil
	}
	target := group(p, func() *node {
		mm := p.mark
		var b *node
		if p.tok(tLPAR) && got(&b, p.singleTarget()) && p.tok(tRPAR) {
			return b
		}
		if !p.retry(mm) {
			return nil
		}
		return p.singleSubscriptAttributeTarget()
	})
	if got(&a, target) && p.tok(tCOLON) && got(&b, p.expression()) && opt(p, &c, eqRhs()) {
		n := p.span("AnnAssign", m)
		n.a, n.b, n.c = a, b, c
		return n
	}
	if !p.retry(m) {
		return nil
	}
	var targets []*node
	if gotSeq(&targets, loop1(p, func() *node {
		return group(p, func() *node {
			var z *node
			if got(&z, p.starTargets()) && p.tok(tEQUAL) {
				return z
			}
			return nil
		})
	})) && got(&b, p.yieldOrStarExpressions()) && !p.at(tEQUAL) {
		n := p.span("Assign", m)
		n.l1, n.a = targets, b
		return n
	}
	if !p.retry(m) {
		return nil
	}
	var op *augOp
	if got(&a, p.singleTarget()) && got(&op, p.augassign()) {
		if got(&c, p.yieldOrStarExpressions()) {
			n := p.span("AugAssign", m)
			n.a, n.op, n.b = a, op.op, c
			return n
		}
		// The cut: no other alternative is tried.
		p.mark = m
		return nil
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidAssignment()
		p.mark = m
	}
	return nil
}

// yieldOrStarExpressions: (yield_expr | star_expressions)
func (p *parser) yieldOrStarExpressions() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.yieldExpr(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.starExpressions(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// annotated_rhs: yield_expr | star_expressions
func (p *parser) annotatedRhs() *node { return p.yieldOrStarExpressions() }

var augOps = map[int]string{tPLUSEQUAL: "Add", tMINEQUAL: "Sub", tSTAREQUAL: "Mult", tATEQUAL: "MatMult",
	tSLASHEQUAL: "Div", tPERCENTEQUAL: "Mod", tAMPEREQUAL: "BitAnd", tVBAREQUAL: "BitOr",
	tCIRCUMFLEXEQUAL: "BitXor", tLEFTSHIFTEQUAL: "LShift", tRIGHTSHIFTEQUAL: "RShift",
	tDOUBLESTAREQUAL: "Pow", tDOUBLESLASHEQUAL: "FloorDiv"}

// augassign: '+=' | '-=' | '*=' | '@=' | '/=' | '%=' | '&=' | '|=' | '^=' | '<<=' | '>>=' | '**=' | '//='
func (p *parser) augassign() *augOp {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	if op, ok := augOps[p.toks[p.mark].typ]; ok {
		p.mark++
		return &augOp{op}
	}
	return nil
}

// return_stmt: 'return' [star_expressions]
func (p *parser) returnStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.tok(kwReturn) && opt(p, &a, p.starExpressions()) {
		n := p.span("Return", m)
		n.a = a
		return n
	}
	p.mark = m
	return nil
}

// raise_stmt:
//
//	| 'raise' expression ['from' expression]
//	| 'raise'
func (p *parser) raiseStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if p.tok(kwRaise) && got(&a, p.expression()) && opt(p, &b, group(p, func() *node {
		if p.tok(kwFrom) {
			return p.expression()
		}
		return nil
	})) {
		n := p.span("Raise", m)
		n.a, n.b = a, b
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(kwRaise) {
		return p.span("Raise", m)
	}
	p.mark = m
	return nil
}

// global_stmt: 'global' ','.NAME+
func (p *parser) globalStmt() *node { return p.namesStmt(kwGlobal, "Global") }

// nonlocal_stmt: 'nonlocal' ','.NAME+
func (p *parser) nonlocalStmt() *node { return p.namesStmt(kwNonlocal, "Nonlocal") }

func (p *parser) namesStmt(kw int, kind string) *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var names []*node
	if p.tok(kw) && gotSeq(&names, gather(p, tCOMMA, p.name)) {
		n := p.span(kind, m)
		for _, x := range names {
			n.l1 = append(n.l1, &node{kind: "alias", id: x.id})
		}
		return n
	}
	p.mark = m
	return nil
}

// del_stmt:
//
//	| 'del' del_targets &(';' | NEWLINE)
//	| invalid_del_stmt
func (p *parser) delStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	if p.tok(kwDel) && gotSeq(&a, p.delTargets()) && p.atAny(tSEMI, tNEWLINE) {
		n := p.span("Delete", m)
		n.l1 = a
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidDelStmt()
		p.mark = m
	}
	return nil
}

// yield_stmt: yield_expr
func (p *parser) yieldStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if y := p.yieldExpr(); y != nil {
		n := p.span("Expr", m)
		n.a = y
		return n
	}
	p.mark = m
	return nil
}

// assert_stmt: 'assert' expression [',' expression]
func (p *parser) assertStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if p.tok(kwAssert) && got(&a, p.expression()) && opt(p, &b, group(p, func() *node {
		if p.tok(tCOMMA) {
			return p.expression()
		}
		return nil
	})) {
		n := p.span("Assert", m)
		n.a, n.b = a, b
		return n
	}
	p.mark = m
	return nil
}

// import_stmt: invalid_import | import_name | import_from
func (p *parser) importStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidImport()
		if !p.retry(m) {
			return nil
		}
	}
	if a := p.importName(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.importFrom(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// import_name: 'import' dotted_as_names
func (p *parser) importName() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	if p.tok(kwImport) && gotSeq(&a, p.dottedAsNames()) {
		n := p.span("Import", m)
		n.l1 = a
		return n
	}
	p.mark = m
	return nil
}

// importDot: ('.' | '...')
func (p *parser) importDot() *ptok {
	return group(p, func() *ptok {
		if t := p.expect(tDOT); t != nil {
			return t
		}
		return p.expect(tELLIPSIS)
	})
}

func countDots(dots []*ptok) int {
	n := 0
	for _, d := range dots {
		n += len(d.text)
	}
	return n
}

// import_from:
//
//	| 'from' ('.' | '...')* dotted_name 'import' import_from_targets
//	| 'from' ('.' | '...')+ 'import' import_from_targets
func (p *parser) importFrom() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var dots []*ptok
	var b *node
	var c []*node
	if p.tok(kwFrom) && gotSeq(&dots, loop0(p, p.importDot)) && got(&b, p.dottedName()) && p.tok(kwImport) &&
		gotSeq(&c, p.importFromTargets()) {
		n := p.span("ImportFrom", m)
		n.id, n.l1, n.level = b.id, c, countDots(dots)
		if n.level == 0 && b.id == "__future__" {
			for _, a := range c {
				if a.id == "barry_as_FLUFL" {
					p.barry = true
				}
			}
		}
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(kwFrom) && gotSeq(&dots, loop1(p, p.importDot)) && p.tok(kwImport) && gotSeq(&c, p.importFromTargets()) {
		n := p.span("ImportFrom", m)
		n.noID, n.l1, n.level = true, c, countDots(dots)
		return n
	}
	p.mark = m
	return nil
}

// import_from_targets:
//
//	| '(' import_from_as_names [','] ')'
//	| import_from_as_names !','
//	| '*'
//	| invalid_import_from_targets
func (p *parser) importFromTargets() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	if p.tok(tLPAR) && gotSeq(&a, p.importFromAsNames()) && p.optTok(tCOMMA) && p.tok(tRPAR) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&a, p.importFromAsNames()) && !p.at(tCOMMA) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tSTAR) {
		n := p.span("alias", m)
		n.id, n.noAsname = "*", true
		return []*node{n}
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidImportFromTargets()
		p.mark = m
	}
	return nil
}

// import_from_as_names: ','.import_from_as_name+
func (p *parser) importFromAsNames() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return gather(p, tCOMMA, p.importFromAsName)
}

// dotted_as_names: ','.dotted_as_name+
func (p *parser) dottedAsNames() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return gather(p, tCOMMA, p.dottedAsName)
}

// import_from_as_name: NAME ['as' NAME]
func (p *parser) importFromAsName() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if got(&a, p.name()) && opt(p, &b, p.asName()) {
		return p.alias(a, b, m)
	}
	p.mark = m
	return nil
}

// asName: 'as' NAME
func (p *parser) asName() *node {
	return group(p, func() *node {
		if p.tok(kwAs) {
			return p.name()
		}
		return nil
	})
}

func (p *parser) alias(a, b *node, start int) *node {
	n := p.span("alias", start)
	n.id, n.noAsname = a.id, true
	if b != nil {
		n.asname, n.noAsname = b.id, false
	}
	return n
}

// dotted_as_name: dotted_name ['as' NAME]
func (p *parser) dottedAsName() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if got(&a, p.dottedName()) && opt(p, &b, p.asName()) {
		return p.alias(a, b, m)
	}
	p.mark = m
	return nil
}

// dotted_name: dotted_name '.' NAME | NAME
func (p *parser) dottedName() *node {
	return memo(p, rDottedName, func() *node {
		// The growing runs in the rule's _raw function, a rule of its own.
		if !p.enter() {
			return nil
		}
		defer p.leave()
		var a *node
		if !p.seed() || !got(&a, p.name()) {
			return nil
		}
		for {
			m := p.mark
			var b *node
			if !p.seed() || !p.tok(tDOT) || !got(&b, p.name()) {
				p.mark = m
				return a
			}
			n := &node{kind: "Name", id: a.id + "." + b.id, ctx: "Load", lineno: a.lineno, col: a.col,
				endLineno: b.endLineno, endCol: b.endCol, hasPos: true}
			a = n
		}
	})
}

// block (memo):
//
//	| NEWLINE INDENT statements DEDENT
//	| simple_stmts
//	| invalid_block
func (p *parser) block() []*node {
	r := memo(p, rBlock, func() *[]*node {
		m := p.mark
		var a []*node
		if p.tok(tNEWLINE) && p.tok(tINDENT) && gotSeq(&a, p.statements()) && p.tok(tDEDENT) {
			return &a
		}
		if !p.retry(m) {
			return nil
		}
		if gotSeq(&a, p.simpleStmts()) {
			return &a
		}
		if !p.retry(m) {
			return nil
		}
		if p.invalid {
			p.invalidBlock()
			p.mark = m
		}
		return nil
	})
	if r == nil {
		return nil
	}
	return *r
}

// decorators: ('@' named_expression NEWLINE)+
func (p *parser) decorators() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return loop1(p, func() *node {
		return group(p, func() *node {
			var f *node
			if p.tok(tAT) && got(&f, p.namedExpression()) && p.tok(tNEWLINE) {
				return f
			}
			return nil
		})
	})
}

// class_def: decorators class_def_raw | class_def_raw
func (p *parser) classDef() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var d []*node
	var b *node
	if gotSeq(&d, p.decorators()) && got(&b, p.classDefRaw()) {
		c := *b
		c.l3 = d
		return &c
	}
	if !p.retry(m) {
		return nil
	}
	if b := p.classDefRaw(); b != nil {
		return b
	}
	p.mark = m
	return nil
}

// class_def_raw:
//
//	| invalid_class_def_raw
//	| 'class' NAME [type_params] ['(' [arguments] ')'] ':' block
func (p *parser) classDefRaw() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidClassDefRaw()
		if !p.retry(m) {
			return nil
		}
	}
	var a, b *node
	var t, c []*node
	if p.tok(kwClass) && got(&a, p.name()) && optSeq(p, &t, p.typeParams()) && opt(p, &b, group(p, func() *node {
		var z *node
		if p.tok(tLPAR) && opt(p, &z, p.arguments()) && p.tok(tRPAR) {
			if z == nil {
				return &node{kind: "Call"}
			}
			return z
		}
		return nil
	})) && p.tok(tCOLON) && gotSeq(&c, p.block()) {
		n := p.span("ClassDef", m)
		n.id, n.body, n.l4 = a.id, c, t
		if b != nil {
			n.l1, n.l2 = b.l1, b.l2
		}
		return n
	}
	p.mark = m
	return nil
}

// function_def: decorators function_def_raw | function_def_raw
func (p *parser) functionDef() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var d []*node
	var f *node
	if gotSeq(&d, p.decorators()) && got(&f, p.functionDefRaw()) {
		c := *f
		c.l1 = d
		return &c
	}
	if !p.retry(m) {
		return nil
	}
	if f := p.functionDefRaw(); f != nil {
		return f
	}
	p.mark = m
	return nil
}

// function_def_raw:
//
//	| invalid_def_raw
//	| 'def' NAME [type_params] &&'(' [params] ')' ['->' expression] &&':' [func_type_comment] block
//	| ASYNC 'def' NAME [type_params] &&'(' [params] ')' ['->' expression] &&':' [func_type_comment] block
func (p *parser) functionDefRaw() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidDefRaw()
		if !p.retry(m) {
			return nil
		}
	}
	for _, async := range []bool{false, true} {
		var n, params, ret *node
		var t, b []*node
		if (!async || p.tok(tASYNC)) && p.tok(kwDef) && got(&n, p.name()) && optSeq(p, &t, p.typeParams()) &&
			p.expectForced(tLPAR, "(") && opt(p, &params, p.params()) && p.tok(tRPAR) &&
			opt(p, &ret, group(p, func() *node {
				if p.tok(tRARROW) {
					return p.expression()
				}
				return nil
			})) && p.expectForced(tCOLON, ":") && gotSeq(&b, p.block()) {
			kind := "FunctionDef"
			if async {
				kind = "AsyncFunctionDef"
			}
			f := p.span(kind, m)
			if params == nil {
				params = &node{kind: "arguments"}
			}
			f.id, f.a, f.body, f.b, f.l2 = n.id, params, b, ret, t
			return f
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

// params: invalid_parameters | parameters
func (p *parser) params() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidParameters()
		if !p.retry(m) {
			return nil
		}
	}
	if a := p.parameters(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// parameters:
//
//	| slash_no_default param_no_default* param_with_default* [star_etc]
//	| slash_with_default param_with_default* [star_etc]
//	| param_no_default+ param_with_default* [star_etc]
//	| param_with_default+ [star_etc]
//	| star_etc
func (p *parser) parameters() *node { return p.parametersOf(false) }

// lambda_parameters: as parameters, with the lambda_ forms of the rules.
func (p *parser) lambdaParameters() *node { return p.parametersOf(true) }

// parametersOf is parameters, or lambda_parameters when lambda is set: the
// same rules, but a lambda's parameters end at ':' and have no
// annotations.
func (p *parser) parametersOf(lambda bool) *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	noDefault, withDefault := p.paramNoDefaultRule(lambda), p.paramWithDefaultRule(lambda)
	var a, b []*node
	var c []*nameDefault
	var d *starEtc
	var s *slashDefault
	if gotSeq(&a, p.slashNoDefault(lambda)) && gotSeq(&b, loop0(p, noDefault)) && gotSeq(&c, loop0(p, withDefault)) &&
		opt(p, &d, p.starEtcRule(lambda)) {
		return makeArguments(a, nil, b, c, d)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&s, p.slashWithDefault(lambda)) && gotSeq(&c, loop0(p, withDefault)) && opt(p, &d, p.starEtcRule(lambda)) {
		return makeArguments(nil, s, nil, c, d)
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&a, loop1(p, noDefault)) && gotSeq(&c, loop0(p, withDefault)) && opt(p, &d, p.starEtcRule(lambda)) {
		return makeArguments(nil, nil, a, c, d)
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&c, loop1(p, withDefault)) && opt(p, &d, p.starEtcRule(lambda)) {
		return makeArguments(nil, nil, nil, c, d)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&d, p.starEtcRule(lambda)) {
		return makeArguments(nil, nil, nil, nil, d)
	}
	p.mark = m
	return nil
}

// paramsEnd is the token after the last parameter: ')' for a def, ':' for a
// lambda.
func paramsEnd(lambda bool) int {
	if lambda {
		return tCOLON
	}
	return tRPAR
}

// slash_no_default:
//
//	| param_no_default+ '/' ','
//	| param_no_default+ '/' &')'
func (p *parser) slashNoDefault(lambda bool) []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	for _, alt := range []bool{true, false} {
		if gotSeq(&a, loop1(p, p.paramNoDefaultRule(lambda))) && p.tok(tSLASH) &&
			(alt && p.tok(tCOMMA) || !alt && p.at(paramsEnd(lambda))) {
			return a
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

// slash_with_default:
//
//	| param_no_default* param_with_default+ '/' ','
//	| param_no_default* param_with_default+ '/' &')'
func (p *parser) slashWithDefault(lambda bool) *slashDefault {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*node
	var b []*nameDefault
	for _, alt := range []bool{true, false} {
		if gotSeq(&a, loop0(p, p.paramNoDefaultRule(lambda))) && gotSeq(&b, loop1(p, p.paramWithDefaultRule(lambda))) &&
			p.tok(tSLASH) && (alt && p.tok(tCOMMA) || !alt && p.at(paramsEnd(lambda))) {
			return &slashDefault{plain: a, defaults: b}
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

// star_etc:
//
//	| invalid_star_etc
//	| '*' param_no_default param_maybe_default* [kwds]
//	| '*' param_no_default_star_annotation param_maybe_default* [kwds]
//	| '*' ',' param_maybe_default+ [kwds]
//	| kwds
//
// and lambda_star_etc, which has no star annotation.
func (p *parser) starEtcRule(lambda bool) *starEtc {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		if lambda {
			p.invalidLambdaStarEtc()
		} else {
			p.invalidStarEtc()
		}
		if !p.retry(m) {
			return nil
		}
	}
	maybe := p.paramMaybeDefaultRule(lambda)
	var a, c *node
	var b []*nameDefault
	if p.tok(tSTAR) && got(&a, p.paramNoDefaultRule(lambda)()) && gotSeq(&b, loop0(p, maybe)) &&
		opt(p, &c, p.kwds(lambda)) {
		return &starEtc{a, b, c}
	}
	if !p.retry(m) {
		return nil
	}
	if !lambda {
		if p.tok(tSTAR) && got(&a, p.paramNoDefaultStarAnnotation()) && gotSeq(&b, loop0(p, maybe)) &&
			opt(p, &c, p.kwds(lambda)) {
			return &starEtc{a, b, c}
		}
		if !p.retry(m) {
			return nil
		}
	}
	if p.tok(tSTAR) && p.tok(tCOMMA) && gotSeq(&b, loop1(p, maybe)) && opt(p, &c, p.kwds(lambda)) {
		return &starEtc{nil, b, c}
	}
	if !p.retry(m) {
		return nil
	}
	if got(&a, p.kwds(lambda)) {
		return &starEtc{nil, nil, a}
	}
	p.mark = m
	return nil
}

// kwds: invalid_kwds | '**' param_no_default
func (p *parser) kwds(lambda bool) *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidKwds(lambda)
		if !p.retry(m) {
			return nil
		}
	}
	var a *node
	if p.tok(tDOUBLESTAR) && got(&a, p.paramNoDefaultRule(lambda)()) {
		return a
	}
	p.mark = m
	return nil
}

// param_no_default:
//
//	| param ',' TYPE_COMMENT?
//	| param TYPE_COMMENT? &')'
func (p *parser) paramNoDefaultRule(lambda bool) func() *node {
	return func() *node {
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a *node
		if got(&a, p.param(lambda)) && p.tok(tCOMMA) {
			return a
		}
		if !p.retry(m) {
			return nil
		}
		if got(&a, p.param(lambda)) && p.at(paramsEnd(lambda)) {
			return a
		}
		p.mark = m
		return nil
	}
}

// param_no_default_star_annotation:
//
//	| param_star_annotation ',' TYPE_COMMENT?
//	| param_star_annotation TYPE_COMMENT? &')'
func (p *parser) paramNoDefaultStarAnnotation() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *node
	if got(&a, p.paramStarAnnotation()) && p.tok(tCOMMA) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if got(&a, p.paramStarAnnotation()) && p.at(tRPAR) {
		return a
	}
	p.mark = m
	return nil
}

// param_with_default:
//
//	| param default ',' TYPE_COMMENT?
//	| param default TYPE_COMMENT? &')'
func (p *parser) paramWithDefaultRule(lambda bool) func() *nameDefault {
	return func() *nameDefault {
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a, c *node
		if got(&a, p.param(lambda)) && got(&c, p.defaultRule()) && p.tok(tCOMMA) {
			return &nameDefault{a, c}
		}
		if !p.retry(m) {
			return nil
		}
		if got(&a, p.param(lambda)) && got(&c, p.defaultRule()) && p.at(paramsEnd(lambda)) {
			return &nameDefault{a, c}
		}
		p.mark = m
		return nil
	}
}

// param_maybe_default:
//
//	| param default? ',' TYPE_COMMENT?
//	| param default? TYPE_COMMENT? &')'
func (p *parser) paramMaybeDefaultRule(lambda bool) func() *nameDefault {
	return func() *nameDefault {
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a, c *node
		if got(&a, p.param(lambda)) && opt(p, &c, p.defaultRule()) && p.tok(tCOMMA) {
			return &nameDefault{a, c}
		}
		if !p.retry(m) {
			return nil
		}
		if got(&a, p.param(lambda)) && opt(p, &c, p.defaultRule()) && p.at(paramsEnd(lambda)) {
			return &nameDefault{a, c}
		}
		p.mark = m
		return nil
	}
}

// param: NAME annotation?
// lambda_param: NAME
func (p *parser) param(lambda bool) *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if got(&a, p.name()) && (lambda || opt(p, &b, p.annotation(false))) {
		n := p.span("arg", m)
		n.id, n.a = a.id, b
		return n
	}
	p.mark = m
	return nil
}

// param_star_annotation: NAME star_annotation
func (p *parser) paramStarAnnotation() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if got(&a, p.name()) && got(&b, p.annotation(true)) {
		n := p.span("arg", m)
		n.id, n.a = a.id, b
		return n
	}
	p.mark = m
	return nil
}

// annotation: ':' expression
// star_annotation: ':' star_expression
func (p *parser) annotation(star bool) *node {
	return group(p, func() *node {
		if !p.tok(tCOLON) {
			return nil
		}
		if star {
			return p.starExpression()
		}
		return p.expression()
	})
}

// default: '=' expression | invalid_default
func (p *parser) defaultRule() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.tok(tEQUAL) && got(&a, p.expression()) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidDefault()
		p.mark = m
	}
	return nil
}

// if_stmt:
//
//	| invalid_if_stmt
//	| 'if' named_expression ':' block elif_stmt
//	| 'if' named_expression ':' block [else_block]
//
// and elif_stmt, the same with 'elif'.
func (p *parser) ifStmt() *node { return p.ifRule(kwIf) }

func (p *parser) ifRule(kw int) *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		if kw == kwIf {
			p.invalidIfStmt()
		} else {
			p.invalidElifStmt()
		}
		if !p.retry(m) {
			return nil
		}
	}
	var a, c *node
	var b, e []*node
	if p.tok(kw) && got(&a, p.namedExpression()) && p.tok(tCOLON) && gotSeq(&b, p.block()) && got(&c, p.ifRule(kwElif)) {
		n := p.span("If", m)
		n.a, n.body, n.orelse = a, b, []*node{c}
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(kw) && got(&a, p.namedExpression()) && p.tok(tCOLON) && gotSeq(&b, p.block()) &&
		optSeq(p, &e, p.elseBlock()) {
		n := p.span("If", m)
		n.a, n.body, n.orelse = a, b, e
		return n
	}
	p.mark = m
	return nil
}

// else_block: invalid_else_stmt | 'else' &&':' block
func (p *parser) elseBlock() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidElseStmt()
		if !p.retry(m) {
			return nil
		}
	}
	var b []*node
	if p.tok(kwElse) && p.expectForced(tCOLON, ":") && gotSeq(&b, p.block()) {
		return b
	}
	p.mark = m
	return nil
}

// while_stmt:
//
//	| invalid_while_stmt
//	| 'while' named_expression ':' block [else_block]
func (p *parser) whileStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidWhileStmt()
		if !p.retry(m) {
			return nil
		}
	}
	var a *node
	var b, c []*node
	if p.tok(kwWhile) && got(&a, p.namedExpression()) && p.tok(tCOLON) && gotSeq(&b, p.block()) &&
		optSeq(p, &c, p.elseBlock()) {
		n := p.span("While", m)
		n.a, n.body, n.orelse = a, b, c
		return n
	}
	p.mark = m
	return nil
}

// for_stmt:
//
//	| invalid_for_stmt
//	| 'for' star_targets 'in' ~ star_expressions ':' [TYPE_COMMENT] block [else_block]
//	| ASYNC 'for' star_targets 'in' ~ star_expressions ':' [TYPE_COMMENT] block [else_block]
//	| invalid_for_target
func (p *parser) forStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidForStmt()
		if !p.retry(m) {
			return nil
		}
	}
	for _, async := range []bool{false, true} {
		var t, ex *node
		var b, el []*node
		if (!async || p.tok(tASYNC)) && p.tok(kwFor) && got(&t, p.starTargets()) && p.tok(kwIn) {
			if got(&ex, p.starExpressions()) && p.tok(tCOLON) && gotSeq(&b, p.block()) && optSeq(p, &el, p.elseBlock()) {
				kind := "For"
				if async {
					kind = "AsyncFor"
				}
				n := p.span(kind, m)
				n.a, n.b, n.body, n.orelse = t, ex, b, el
				return n
			}
			// The cut: no other alternative is tried.
			p.mark = m
			return nil
		}
		if !p.retry(m) {
			return nil
		}
	}
	if p.invalid {
		p.invalidForTarget()
		p.mark = m
	}
	return nil
}

// with_stmt:
//
//	| invalid_with_stmt_indent
//	| 'with' '(' ','.with_item+ ','? ')' ':' block
//	| 'with' ','.with_item+ ':' [TYPE_COMMENT] block
//	| ASYNC 'with' '(' ','.with_item+ ','? ')' ':' block
//	| ASYNC 'with' ','.with_item+ ':' [TYPE_COMMENT] block
//	| invalid_with_stmt
func (p *parser) withStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidWithStmtIndent()
		if !p.retry(m) {
			return nil
		}
	}
	for _, async := range []bool{false, true} {
		kind := "With"
		if async {
			kind = "AsyncWith"
		}
		var a, b []*node
		if (!async || p.tok(tASYNC)) && p.tok(kwWith) && p.tok(tLPAR) && gotSeq(&a, gather(p, tCOMMA, p.withItem)) &&
			p.optTok(tCOMMA) && p.tok(tRPAR) && p.tok(tCOLON) && gotSeq(&b, p.block()) {
			n := p.span(kind, m)
			n.l1, n.body = a, b
			return n
		}
		if !p.retry(m) {
			return nil
		}
		if (!async || p.tok(tASYNC)) && p.tok(kwWith) && gotSeq(&a, gather(p, tCOMMA, p.withItem)) && p.tok(tCOLON) &&
			gotSeq(&b, p.block()) {
			n := p.span(kind, m)
			n.l1, n.body = a, b
			return n
		}
		if !p.retry(m) {
			return nil
		}
	}
	if p.invalid {
		p.invalidWithStmt()
		p.mark = m
	}
	return nil
}

// with_item:
//
//	| expression 'as' star_target &(',' | ')' | ':')
//	| invalid_with_item
//	| expression
func (p *parser) withItem() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var e, t *node
	if got(&e, p.expression()) && p.tok(kwAs) && got(&t, p.starTarget()) && p.atAny(tCOMMA, tRPAR, tCOLON) {
		return &node{kind: "withitem", a: e, b: t}
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidWithItem()
		if !p.retry(m) {
			return nil
		}
	}
	if got(&e, p.expression()) {
		return &node{kind: "withitem", a: e}
	}
	p.mark = m
	return nil
}

// try_stmt:
//
//	| invalid_try_stmt
//	| 'try' &&':' block finally_block
//	| 'try' &&':' block except_block+ [else_block] [finally_block]
//	| 'try' &&':' block except_star_block+ [else_block] [finally_block]
func (p *parser) tryStmt() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidTryStmt()
		if !p.retry(m) {
			return nil
		}
	}
	var b, f, ex, el []*node
	if p.tok(kwTry) && p.expectForced(tCOLON, ":") && gotSeq(&b, p.block()) && gotSeq(&f, p.finallyBlock()) {
		n := p.span("Try", m)
		n.body, n.finalbody = b, f
		return n
	}
	if !p.retry(m) {
		return nil
	}
	for _, star := range []bool{false, true} {
		handler := p.exceptBlock
		kind := "Try"
		if star {
			handler, kind = p.exceptStarBlock, "TryStar"
		}
		if p.tok(kwTry) && p.expectForced(tCOLON, ":") && gotSeq(&b, p.block()) && gotSeq(&ex, loop1(p, handler)) &&
			optSeq(p, &el, p.elseBlock()) && optSeq(p, &f, p.finallyBlock()) {
			n := p.span(kind, m)
			n.body, n.l1, n.orelse, n.finalbody = b, ex, el, f
			return n
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

// except_block:
//
//	| invalid_except_stmt_indent
//	| 'except' expression ['as' NAME] ':' block
//	| 'except' ':' block
//	| invalid_except_stmt
func (p *parser) exceptBlock() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidExceptStmtIndent()
		if !p.retry(m) {
			return nil
		}
	}
	var e, t *node
	var b []*node
	if p.tok(kwExcept) && got(&e, p.expression()) && opt(p, &t, p.asName()) && p.tok(tCOLON) && gotSeq(&b, p.block()) {
		return p.exceptHandler(e, t, b, m)
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(kwExcept) && p.tok(tCOLON) && gotSeq(&b, p.block()) {
		return p.exceptHandler(nil, nil, b, m)
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidExceptStmt()
		p.mark = m
	}
	return nil
}

func (p *parser) exceptHandler(e, t *node, b []*node, start int) *node {
	n := p.span("ExceptHandler", start)
	n.a, n.body, n.noID = e, b, t == nil
	if t != nil {
		n.id = t.id
	}
	return n
}

// except_star_block:
//
//	| invalid_except_star_stmt_indent
//	| 'except' '*' expression ['as' NAME] ':' block
//	| invalid_except_stmt
func (p *parser) exceptStarBlock() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidExceptStarStmtIndent()
		if !p.retry(m) {
			return nil
		}
	}
	var e, t *node
	var b []*node
	if p.tok(kwExcept) && p.tok(tSTAR) && got(&e, p.expression()) && opt(p, &t, p.asName()) && p.tok(tCOLON) &&
		gotSeq(&b, p.block()) {
		return p.exceptHandler(e, t, b, m)
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidExceptStmt()
		p.mark = m
	}
	return nil
}

// finally_block: invalid_finally_stmt | 'finally' &&':' block
func (p *parser) finallyBlock() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidFinallyStmt()
		if !p.retry(m) {
			return nil
		}
	}
	var a []*node
	if p.tok(kwFinally) && p.expectForced(tCOLON, ":") && gotSeq(&a, p.block()) {
		return a
	}
	p.mark = m
	return nil
}

// type_alias: "type" NAME [type_params] '=' expression
func (p *parser) typeAlias() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var n, b *node
	var t []*node
	if p.softKeyword("type") != nil && got(&n, p.name()) && optSeq(p, &t, p.typeParams()) && p.tok(tEQUAL) &&
		got(&b, p.expression()) {
		s := p.span("TypeAlias", m)
		s.a, s.l1, s.b = setCtx(n, "Store"), t, b
		return s
	}
	p.mark = m
	return nil
}

// type_params: '[' type_param_seq ']'
func (p *parser) typeParams() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var t []*node
	if p.tok(tLSQB) && gotSeq(&t, p.typeParamSeq()) && p.tok(tRSQB) {
		return t
	}
	p.mark = m
	return nil
}

// type_param_seq: ','.type_param+ [',']
func (p *parser) typeParamSeq() []*node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var t []*node
	if gotSeq(&t, gather(p, tCOMMA, p.typeParam)) && p.optTok(tCOMMA) {
		return t
	}
	p.mark = m
	return nil
}

// type_param (memo):
//
//	| NAME [type_param_bound]
//	| '*' NAME ':' expression
//	| '*' NAME
//	| '**' NAME ':' expression
//	| '**' NAME
func (p *parser) typeParam() *node {
	return memo(p, rTypeParam, func() *node {
		m := p.mark
		var a, b *node
		if got(&a, p.name()) && opt(p, &b, p.annotation(false)) {
			n := p.span("TypeVar", m)
			n.id, n.a = a.id, b
			return n
		}
		if !p.retry(m) {
			return nil
		}
		for _, star := range []int{tSTAR, tDOUBLESTAR} {
			kind, what := "TypeVarTuple", "TypeVarTuple"
			if star == tDOUBLESTAR {
				kind, what = "ParamSpec", "ParamSpec"
			}
			var colon *ptok
			var e *node
			if p.tok(star) && got(&a, p.name()) && got(&colon, p.expect(tCOLON)) && got(&e, p.expression()) {
				msg := "cannot use bound with " + what
				if e.kind == "Tuple" {
					msg = "cannot use constraints with " + what
				}
				p.raiseAt("", colon.line, msg)
				return nil
			}
			if !p.retry(m) {
				return nil
			}
			if p.tok(star) && got(&a, p.name()) {
				n := p.span(kind, m)
				n.id = a.id
				return n
			}
			if !p.retry(m) {
				return nil
			}
		}
		return nil
	})
}
