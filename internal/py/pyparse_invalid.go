// Ported from CPython 3.12 (Grammar/python.gram) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The invalid_* rules of the grammar, tried only in the parser's second
// pass: each recognises a common mistake and raises its specific
// SyntaxError. An alternative that matches ends the rule, whether or not
// its action raises.

package py

import "strconv"

// raiseNoIndent raises "expected an indented block after ..." for the
// statement whose keyword is on line.
func (p *parser) raiseNoIndent(what string, line int) {
	p.raiseClass("IndentationError", "expected an indented block after "+what+" on line "+strconv.Itoa(line))
}

// newlineNoIndent matches NEWLINE !INDENT.
func (p *parser) newlineNoIndent() bool { return p.tok(tNEWLINE) && !p.at(tINDENT) }

// invalid_arguments:
//
//	| ((','.(starred_expression | (assignment_expression | expression !':=') !'=')+ ',' kwargs) | kwargs) ',' ','.(starred_expression !'=')+
//	| expression for_if_clauses ',' [args | expression for_if_clauses]
//	| NAME '=' expression for_if_clauses
//	| (args ',')? NAME '=' &(',' | ')')
//	| args for_if_clauses
//	| args ',' expression for_if_clauses
//	| args ',' args
func (p *parser) invalidArguments() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	// ((','.(starred_expression | (assignment_expression | expression !':=') !'=')+ ',' kwargs) | kwargs)
	head := func() bool {
		return p.sub(func() bool {
			mm := p.mark
			if p.sub(func() bool {
				return gather(p, tCOMMA, p.positionalArg) != nil && p.tok(tCOMMA) && p.kwargs() != nil
			}) {
				return true
			}
			return p.retry(mm) && p.kwargs() != nil
		})
	}
	var a *ptok
	if head() && got(&a, p.expect(tCOMMA)) && gather(p, tCOMMA, func() *node {
		return group(p, func() *node {
			var s *node
			if got(&s, p.starredExpression()) && !p.at(tEQUAL) {
				return s
			}
			return nil
		})
	}) != nil {
		p.raiseAt("", a.line, "iterable argument unpacking follows keyword argument unpacking")
		return
	}
	if !p.retry(m) {
		return
	}
	var e *node
	var b []*node
	if got(&e, p.expression()) && gotSeq(&b, p.forIfClauses()) && p.tok(tCOMMA) && p.maybe(group(p, func() *node {
		mm := p.mark
		if x := p.args(); x != nil {
			return x
		}
		if !p.retry(mm) {
			return nil
		}
		var y *node
		if got(&y, p.expression()) && p.forIfClauses() != nil {
			return y
		}
		return nil
	}) != nil) {
		p.raiseAt("", e.lineno, "Generator expression must be parenthesized")
		return
	}
	if !p.retry(m) {
		return
	}
	var n *node
	if got(&n, p.name()) && p.tok(tEQUAL) && p.expression() != nil && p.forIfClauses() != nil {
		p.raiseAt("", n.lineno, "invalid syntax. Maybe you meant '==' or ':=' instead of '='?")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.maybe(group(p, func() *node {
		var x *node
		if got(&x, p.args()) && p.tok(tCOMMA) {
			return x
		}
		return nil
	}) != nil) && got(&n, p.name()) && p.tok(tEQUAL) && p.atAny(tCOMMA, tRPAR) {
		p.raiseAt("", n.lineno, "expected argument value expression")
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&n, p.args()) && gotSeq(&b, p.forIfClauses()) {
		p.nonparenGenexpInCall(n)
		return
	}
	if !p.retry(m) {
		return
	}
	if p.args() != nil && p.tok(tCOMMA) && got(&e, p.expression()) && p.forIfClauses() != nil {
		p.raiseAt("", e.lineno, "Generator expression must be parenthesized")
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&n, p.args()) && p.tok(tCOMMA) && p.args() != nil {
		p.argumentsParsingError(n)
		return
	}
	p.mark = m
}

// invalid_kwarg:
//
//	| ('True'|'False'|'None') '='
//	| NAME '=' expression for_if_clauses
//	| !(NAME '=') expression '='
//	| '**' expression '=' expression
func (p *parser) invalidKwarg() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	for _, kw := range []int{kwTrue, kwFalse, kwNone} {
		var a *ptok
		if got(&a, p.expect(kw)) && p.tok(tEQUAL) {
			p.raiseAt("", a.line, "cannot assign to "+a.text)
			return
		}
		if !p.retry(m) {
			return
		}
	}
	var a *node
	if got(&a, p.name()) && p.tok(tEQUAL) && p.expression() != nil && p.forIfClauses() != nil {
		p.raiseAt("", a.lineno, "invalid syntax. Maybe you meant '==' or ':=' instead of '='?")
		return
	}
	if !p.retry(m) {
		return
	}
	if !p.ahead(func() bool { return p.sub(func() bool { return p.name() != nil && p.tok(tEQUAL) }) }) &&
		got(&a, p.expression()) && p.tok(tEQUAL) {
		p.raiseAt("", a.lineno, "expression cannot contain assignment, perhaps you meant \"==\"?")
		return
	}
	if !p.retry(m) {
		return
	}
	var t *ptok
	if got(&t, p.expect(tDOUBLESTAR)) && p.expression() != nil && p.tok(tEQUAL) && p.expression() != nil {
		p.raiseAt("", t.line, "cannot assign to keyword argument unpacking")
		return
	}
	p.mark = m
}

// invalid_legacy_expression: NAME !'(' star_expressions
func (p *parser) invalidLegacyExpression() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if got(&a, p.name()) && !p.at(tLPAR) && p.starExpressions() != nil {
		if checkLegacyStmt(a) {
			p.raiseAt("", a.lineno, "Missing parentheses in call to '"+a.id+"'. Did you mean "+a.id+"(...)?")
		}
		return
	}
	p.mark = m
}

// invalid_expression:
//
//	| !(NAME STRING | SOFT_KEYWORD) disjunction expression_without_invalid
//	| disjunction 'if' disjunction !('else'|':')
//	| 'lambda' [lambda_params] ':' &FSTRING_MIDDLE
func (p *parser) invalidExpression() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a, b *node
	if !p.ahead(func() bool {
		return p.sub(func() bool {
			mm := p.mark
			if p.name() != nil && p.tok(tSTRING) {
				return true
			}
			p.mark = mm
			return p.softKeywordToken() != nil
		})
	}) && got(&a, p.disjunction()) && got(&b, p.expressionWithoutInvalid()) {
		// Two expressions side by side inside brackets are probably
		// missing a comma.
		if !checkLegacyStmt(a) && p.toks[p.mark-1].level != 0 {
			p.raiseAt("", a.lineno, "invalid syntax. Perhaps you forgot a comma?")
		}
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&a, p.disjunction()) && p.tok(kwIf) && got(&b, p.disjunction()) && !p.atAny(kwElse, tCOLON) {
		p.raiseAt("", a.lineno, "expected 'else' after 'if' expression")
		return
	}
	if !p.retry(m) {
		return
	}
	var t *ptok
	var params *node
	if got(&t, p.expect(kwLambda)) && opt(p, &params, p.lambdaParams()) && p.tok(tCOLON) && p.at(tFSTRINGMIDDLE) {
		p.raiseAt("", t.line, "f-string: lambda expressions are not allowed without parentheses")
		return
	}
	p.mark = m
}

// invalid_named_expression (memo):
//
//	| expression ':=' expression
//	| NAME '=' bitwise_or !('='|':=')
//	| !(list|tuple|genexp|'True'|'None'|'False') bitwise_or '=' bitwise_or !('='|':=')
func (p *parser) invalidNamedExpression() {
	memo(p, rInvalidNamedExpression, func() *node {
		m := p.mark
		var a *node
		if got(&a, p.expression()) && p.tok(tCOLONEQUAL) && p.expression() != nil {
			p.raiseAt("", a.lineno, "cannot use assignment expressions with "+exprName(a))
			return nil
		}
		if !p.retry(m) {
			return nil
		}
		if got(&a, p.name()) && p.tok(tEQUAL) && p.bitwiseOr() != nil && !p.atAny(tEQUAL, tCOLONEQUAL) {
			p.raiseAt("", a.lineno, "invalid syntax. Maybe you meant '==' or ':=' instead of '='?")
			return nil
		}
		if !p.retry(m) {
			return nil
		}
		if !p.ahead(func() bool {
			return p.sub(func() bool {
				for _, f := range []func() *node{p.list, p.tuple, p.genexp} {
					mm := p.mark
					if f() != nil {
						return true
					}
					p.mark = mm
				}
				return p.tok(kwTrue) || p.tok(kwNone) || p.tok(kwFalse)
			})
		}) && got(&a, p.bitwiseOr()) && p.tok(tEQUAL) && p.bitwiseOr() != nil && !p.atAny(tEQUAL, tCOLONEQUAL) {
			p.raiseAt("", a.lineno, "cannot assign to "+exprName(a)+" here. Maybe you meant '==' instead of '='?")
			return nil
		}
		p.mark = m
		return nil
	})
}

// invalid_assignment:
//
//	| invalid_ann_assign_target ':' expression
//	| star_named_expression ',' star_named_expressions* ':' expression
//	| expression ':' expression
//	| (star_targets '=')* star_expressions '='
//	| (star_targets '=')* yield_expr '='
//	| star_expressions augassign (yield_expr | star_expressions)
func (p *parser) invalidAssignment() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if got(&a, p.invalidAnnAssignTarget()) && p.tok(tCOLON) && p.expression() != nil {
		p.raiseAt("", a.lineno, "only single target (not "+exprName(a)+") can be annotated")
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&a, p.starNamedExpression()) && p.tok(tCOMMA) &&
		loop0(p, func() *[]*node {
			if l := p.starNamedExpressions(); l != nil {
				return &l
			}
			return nil
		}) != nil && p.tok(tCOLON) && p.expression() != nil {
		p.raiseAt("", a.lineno, "only single target (not tuple) can be annotated")
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&a, p.expression()) && p.tok(tCOLON) && p.expression() != nil {
		p.raiseAt("", a.lineno, "illegal target for annotation")
		return
	}
	if !p.retry(m) {
		return
	}
	targetsEq := func() []*node {
		return loop0(p, func() *node {
			return group(p, func() *node {
				var z *node
				if got(&z, p.starTargets()) && p.tok(tEQUAL) {
					return z
				}
				return nil
			})
		})
	}
	if targetsEq() != nil && got(&a, p.starExpressions()) && p.tok(tEQUAL) {
		p.raiseInvalidTarget(starTargets, a)
		return
	}
	if !p.retry(m) {
		return
	}
	if targetsEq() != nil && got(&a, p.yieldExpr()) && p.tok(tEQUAL) {
		p.raiseAt("", a.lineno, "assignment to yield expression not possible")
		return
	}
	if !p.retry(m) {
		return
	}
	if got(&a, p.starExpressions()) && p.augassign() != nil && p.yieldOrStarExpressions() != nil {
		p.raiseAt("", a.lineno, "'"+exprName(a)+"' is an illegal expression for augmented assignment")
		return
	}
	p.mark = m
}

// invalid_ann_assign_target: list | tuple | '(' invalid_ann_assign_target ')'
func (p *parser) invalidAnnAssignTarget() *node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.list(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.tuple(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var a *node
	if p.tok(tLPAR) && got(&a, p.invalidAnnAssignTarget()) && p.tok(tRPAR) {
		return a
	}
	p.mark = m
	return nil
}

// invalid_del_stmt: 'del' star_expressions
func (p *parser) invalidDelStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.tok(kwDel) && got(&a, p.starExpressions()) {
		p.raiseInvalidTarget(delTargets, a)
		return
	}
	p.mark = m
}

// invalid_block: NEWLINE !INDENT
func (p *parser) invalidBlock() {
	if !p.enter() {
		return
	}
	defer p.leave()
	if p.newlineNoIndent() {
		p.raiseClass("IndentationError", "expected an indented block")
	}
}

// invalid_comprehension:
//
//	| ('[' | '(' | '{') starred_expression for_if_clauses
//	| ('[' | '{') star_named_expression ',' star_named_expressions for_if_clauses
//	| ('[' | '{') star_named_expression ',' for_if_clauses
func (p *parser) invalidComprehension() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.atAny(tLSQB, tLPAR, tLBRACE) {
		p.mark++
		if got(&a, p.starredExpression()) && p.forIfClauses() != nil {
			p.raiseAt("", a.lineno, "iterable unpacking cannot be used in comprehension")
			return
		}
	}
	for i := 0; i < 2; i++ {
		if !p.retry(m) {
			return
		}
		if !p.atAny(tLSQB, tLBRACE) {
			continue
		}
		p.mark++
		if got(&a, p.starNamedExpression()) && p.tok(tCOMMA) &&
			(i == 1 || p.starNamedExpressions() != nil) && p.forIfClauses() != nil {
			p.raiseAt("", a.lineno, "did you forget parentheses around the comprehension target?")
			return
		}
	}
	p.mark = m
}

// invalid_dict_comprehension: '{' '**' bitwise_or for_if_clauses '}'
func (p *parser) invalidDictComprehension() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if p.tok(tLBRACE) && got(&a, p.expect(tDOUBLESTAR)) && p.bitwiseOr() != nil && p.forIfClauses() != nil &&
		p.tok(tRBRACE) {
		p.raiseAt("", a.line, "dict unpacking cannot be used in dict comprehension")
		return
	}
	p.mark = m
}

// invalid_parameters:
//
//	| "/" ','
//	| (slash_no_default | slash_with_default) param_maybe_default* '/'
//	| slash_no_default? param_no_default* invalid_parameters_helper param_no_default
//	| param_no_default* '(' param_no_default+ ','? ')'
//	| (slash_no_default | slash_with_default)? param_maybe_default* '*' (',' | param_no_default) param_maybe_default* '/'
//	| param_maybe_default+ '/' '*'
//
// and invalid_lambda_parameters, whose fourth alternative reads
//
//	| lambda_param_no_default* '(' ','.lambda_param+ ','? ')'
func (p *parser) invalidParameters() { p.invalidParametersOf(false) }

func (p *parser) invalidLambdaParameters() { p.invalidParametersOf(true) }

func (p *parser) invalidParametersOf(lambda bool) {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	noDefault, maybe := p.paramNoDefaultRule(lambda), p.paramMaybeDefaultRule(lambda)
	slashAny := func() bool {
		return p.sub(func() bool {
			mm := p.mark
			if p.slashNoDefault(lambda) != nil {
				return true
			}
			return p.retry(mm) && p.slashWithDefault(lambda) != nil
		})
	}
	var a *ptok
	if got(&a, p.expect(tSLASH)) && p.tok(tCOMMA) {
		p.raiseAt("", a.line, "at least one argument must precede /")
		return
	}
	if !p.retry(m) {
		return
	}
	if slashAny() && loop0(p, maybe) != nil && got(&a, p.expect(tSLASH)) {
		p.raiseAt("", a.line, "/ may appear only once")
		return
	}
	if !p.retry(m) {
		return
	}
	var n *node
	var s []*node
	if optSeq(p, &s, p.slashNoDefault(lambda)) && loop0(p, noDefault) != nil && p.invalidParametersHelper(lambda) &&
		got(&n, noDefault()) {
		p.raiseAt("", n.lineno, "parameter without a default follows parameter with a default")
		return
	}
	if !p.retry(m) {
		return
	}
	inner := func() bool { return loop1(p, noDefault) != nil }
	what := "Function parameters"
	if lambda {
		inner = func() bool { return gather(p, tCOMMA, func() *node { return p.param(true) }) != nil }
		what = "Lambda expression parameters"
	}
	if loop0(p, noDefault) != nil && got(&a, p.expect(tLPAR)) && inner() && p.optTok(tCOMMA) && p.tok(tRPAR) {
		p.raiseAt("", a.line, what+" cannot be parenthesized")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.maybe(slashAny()) && loop0(p, maybe) != nil && p.tok(tSTAR) &&
		p.sub(func() bool { return p.tok(tCOMMA) || noDefault() != nil }) &&
		loop0(p, maybe) != nil && got(&a, p.expect(tSLASH)) {
		p.raiseAt("", a.line, "/ must be ahead of *")
		return
	}
	if !p.retry(m) {
		return
	}
	if loop1(p, maybe) != nil && p.tok(tSLASH) && got(&a, p.expect(tSTAR)) {
		p.raiseAt("", a.line, "expected comma between / and *")
		return
	}
	p.mark = m
}

// invalid_parameters_helper: slash_with_default | param_with_default+
func (p *parser) invalidParametersHelper(lambda bool) bool {
	if !p.enter() {
		return false
	}
	defer p.leave()
	m := p.mark
	if p.slashWithDefault(lambda) != nil {
		return true
	}
	if !p.retry(m) {
		return false
	}
	if loop1(p, p.paramWithDefaultRule(lambda)) != nil {
		return true
	}
	p.mark = m
	return false
}

// invalid_default: '=' &(')'|',')
func (p *parser) invalidDefault() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if got(&a, p.expect(tEQUAL)) && p.atAny(tRPAR, tCOMMA) {
		p.raiseAt("", a.line, "expected default value expression")
		return
	}
	p.mark = m
}

// invalid_star_etc:
//
//	| '*' (')' | ',' (')' | '**'))
//	| '*' ',' TYPE_COMMENT
//	| '*' param '='
//	| '*' (param_no_default | ',') param_maybe_default* '*' (param_no_default | ',')
//
// The type comment alternative never matches: type comments are not read.
func (p *parser) invalidStarEtc() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if got(&a, p.expect(tSTAR)) && (p.tok(tRPAR) || p.tok(tCOMMA) && (p.tok(tRPAR) || p.tok(tDOUBLESTAR))) {
		p.raiseAt("", a.line, "named arguments must follow bare *")
		return
	}
	if !p.retry(m) {
		return
	}
	p.invalidStarEtcTail(false)
}

// invalidStarEtcTail are the last two alternatives of invalid_star_etc and
// invalid_lambda_star_etc.
func (p *parser) invalidStarEtcTail(lambda bool) {
	m := p.mark
	var a *ptok
	if p.tok(tSTAR) && p.param(lambda) != nil && got(&a, p.expect(tEQUAL)) {
		p.raiseAt("", a.line, "var-positional argument cannot have default value")
		return
	}
	if !p.retry(m) {
		return
	}
	noDefaultOrComma := func() bool {
		return p.sub(func() bool {
			mm := p.mark
			if p.paramNoDefaultRule(lambda)() != nil {
				return true
			}
			return p.retry(mm) && p.tok(tCOMMA)
		})
	}
	if p.tok(tSTAR) && noDefaultOrComma() && loop0(p, p.paramMaybeDefaultRule(lambda)) != nil &&
		got(&a, p.expect(tSTAR)) && noDefaultOrComma() {
		p.raiseAt("", a.line, "* argument may appear only once")
		return
	}
	p.mark = m
}

// invalid_lambda_star_etc:
//
//	| '*' (':' | ',' (':' | '**'))
//	| '*' lambda_param '='
//	| '*' (lambda_param_no_default | ',') lambda_param_maybe_default* '*' (lambda_param_no_default | ',')
func (p *parser) invalidLambdaStarEtc() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if p.tok(tSTAR) && (p.tok(tCOLON) || p.tok(tCOMMA) && (p.tok(tCOLON) || p.tok(tDOUBLESTAR))) {
		p.raise("named arguments must follow bare *")
		return
	}
	if !p.retry(m) {
		return
	}
	p.invalidStarEtcTail(true)
}

// invalid_kwds:
//
//	| '**' param '='
//	| '**' param ',' param
//	| '**' param ',' ('*'|'**'|'/')
//
// and invalid_lambda_kwds, of lambda_param.
func (p *parser) invalidKwds(lambda bool) {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if p.tok(tDOUBLESTAR) && p.param(lambda) != nil && got(&a, p.expect(tEQUAL)) {
		p.raiseAt("", a.line, "var-keyword argument cannot have default value")
		return
	}
	if !p.retry(m) {
		return
	}
	var n *node
	if p.tok(tDOUBLESTAR) && p.param(lambda) != nil && p.tok(tCOMMA) && got(&n, p.param(lambda)) {
		p.raiseAt("", n.lineno, "arguments cannot follow var-keyword argument")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(tDOUBLESTAR) && p.param(lambda) != nil && p.tok(tCOMMA) {
		for _, t := range []int{tSTAR, tDOUBLESTAR, tSLASH} {
			if got(&a, p.expect(t)) {
				p.raiseAt("", a.line, "arguments cannot follow var-keyword argument")
				return
			}
		}
	}
	p.mark = m
}

// invalid_with_item: expression 'as' expression &(',' | ')' | ':')
func (p *parser) invalidWithItem() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.expression() != nil && p.tok(kwAs) && got(&a, p.expression()) && p.atAny(tCOMMA, tRPAR, tCOLON) {
		p.raiseInvalidTarget(starTargets, a)
		return
	}
	p.mark = m
}

// invalid_for_target: ASYNC? 'for' star_expressions
func (p *parser) invalidForTarget() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.optTok(tASYNC) && p.tok(kwFor) && got(&a, p.starExpressions()) {
		p.raiseInvalidTarget(forTargets, a)
		return
	}
	p.mark = m
}

// invalid_group:
//
//	| '(' starred_expression ')'
//	| '(' '**' expression ')'
func (p *parser) invalidGroup() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if p.tok(tLPAR) && got(&a, p.starredExpression()) && p.tok(tRPAR) {
		p.raiseAt("", a.lineno, "cannot use starred expression here")
		return
	}
	if !p.retry(m) {
		return
	}
	var t *ptok
	if p.tok(tLPAR) && got(&t, p.expect(tDOUBLESTAR)) && p.expression() != nil && p.tok(tRPAR) {
		p.raiseAt("", t.line, "cannot use double starred expression here")
		return
	}
	p.mark = m
}

// invalid_import: 'import' ','.dotted_name+ 'from' dotted_name
func (p *parser) invalidImport() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if got(&a, p.expect(kwImport)) && gather(p, tCOMMA, p.dottedName) != nil && p.tok(kwFrom) && p.dottedName() != nil {
		p.raiseAt("", a.line, "Did you mean to use 'from ... import ...' instead?")
		return
	}
	p.mark = m
}

// invalid_import_from_targets: import_from_as_names ',' NEWLINE
func (p *parser) invalidImportFromTargets() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if p.importFromAsNames() != nil && p.tok(tCOMMA) && p.tok(tNEWLINE) {
		p.raise("trailing comma not allowed without surrounding parentheses")
		return
	}
	p.mark = m
}

// withItems matches the items of a with statement in invalid_with_stmt
// and invalid_with_stmt_indent: ','.(expression ['as' star_target])+, or
// with parenthesised, '(' ','.(expressions ['as' star_target])+ ','? ')'.
func (p *parser) withItems(parenthesised bool) bool {
	item := func() *node {
		return group(p, func() *node {
			var e *node
			if parenthesised {
				e = p.expressions()
			} else {
				e = p.expression()
			}
			if e == nil {
				return nil
			}
			if !opt(p, new(*node), group(p, func() *node {
				if p.tok(kwAs) {
					return p.starTarget()
				}
				return nil
			})) {
				return nil
			}
			return e
		})
	}
	if !parenthesised {
		return gather(p, tCOMMA, item) != nil
	}
	return p.tok(tLPAR) && gather(p, tCOMMA, item) != nil && p.optTok(tCOMMA) && p.tok(tRPAR)
}

// invalid_with_stmt:
//
//	| [ASYNC] 'with' ','.(expression ['as' star_target])+ NEWLINE
//	| [ASYNC] 'with' '(' ','.(expressions ['as' star_target])+ ','? ')' NEWLINE
func (p *parser) invalidWithStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	for _, paren := range []bool{false, true} {
		if p.optTok(tASYNC) && p.tok(kwWith) && p.withItems(paren) && p.tok(tNEWLINE) {
			p.raise("expected ':'")
			return
		}
		if !p.retry(m) {
			return
		}
	}
}

// invalid_with_stmt_indent:
//
//	| [ASYNC] 'with' ','.(expression ['as' star_target])+ ':' NEWLINE !INDENT
//	| [ASYNC] 'with' '(' ','.(expressions ['as' star_target])+ ','? ')' ':' NEWLINE !INDENT
func (p *parser) invalidWithStmtIndent() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	for _, paren := range []bool{false, true} {
		var a *ptok
		if p.optTok(tASYNC) && got(&a, p.expect(kwWith)) && p.withItems(paren) && p.tok(tCOLON) && p.newlineNoIndent() {
			p.raiseNoIndent("'with' statement", a.line)
			return
		}
		if !p.retry(m) {
			return
		}
	}
}

// invalid_try_stmt:
//
//	| 'try' ':' NEWLINE !INDENT
//	| 'try' ':' block !('except' | 'finally')
//	| 'try' ':' block* except_block+ 'except' '*' expression ['as' NAME] ':'
//	| 'try' ':' block* except_star_block+ 'except' [expression ['as' NAME]] ':'
func (p *parser) invalidTryStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a, b *ptok
	if got(&a, p.expect(kwTry)) && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("'try' statement", a.line)
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(kwTry) && p.tok(tCOLON) && p.block() != nil && !p.atAny(kwExcept, kwFinally) {
		p.raise("expected 'except' or 'finally' block")
		return
	}
	if !p.retry(m) {
		return
	}
	blocks := func() bool {
		return loop0(p, func() *[]*node {
			if b := p.block(); b != nil {
				return &b
			}
			return nil
		}) != nil
	}
	var t *node
	if p.tok(kwTry) && p.tok(tCOLON) && blocks() && loop1(p, p.exceptBlock) != nil && got(&a, p.expect(kwExcept)) &&
		got(&b, p.expect(tSTAR)) && p.expression() != nil && opt(p, &t, p.asName()) && p.tok(tCOLON) {
		p.raiseAt("", a.line, "cannot have both 'except' and 'except*' on the same 'try'")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(kwTry) && p.tok(tCOLON) && blocks() && loop1(p, p.exceptStarBlock) != nil && got(&a, p.expect(kwExcept)) &&
		opt(p, &t, group(p, func() *node {
			var e, n *node
			if got(&e, p.expression()) && opt(p, &n, p.asName()) {
				return e
			}
			return nil
		})) && p.tok(tCOLON) {
		p.raiseAt("", a.line, "cannot have both 'except' and 'except*' on the same 'try'")
		return
	}
	p.mark = m
}

// invalid_except_stmt:
//
//	| 'except' '*'? expression ',' expressions ['as' NAME ] ':'
//	| 'except' '*'? expression ['as' NAME ] NEWLINE
//	| 'except' NEWLINE
//	| 'except' '*' (NEWLINE | ':')
func (p *parser) invalidExceptStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a, t *node
	if p.tok(kwExcept) && p.optTok(tSTAR) && got(&a, p.expression()) && p.tok(tCOMMA) && p.expressions() != nil &&
		opt(p, &t, p.asName()) && p.tok(tCOLON) {
		p.raiseAt("", a.lineno, "multiple exception types must be parenthesized")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(kwExcept) && p.optTok(tSTAR) && p.expression() != nil && opt(p, &t, p.asName()) && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(kwExcept) && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.tok(kwExcept) && p.tok(tSTAR) && (p.tok(tNEWLINE) || p.tok(tCOLON)) {
		p.raise("expected one or more exception types")
		return
	}
	p.mark = m
}

// invalid_finally_stmt: 'finally' ':' NEWLINE !INDENT
func (p *parser) invalidFinallyStmt() {
	p.invalidKeywordColonIndent(kwFinally, "'finally' statement")
}

// invalidKeywordColonIndent is the rule KW ':' NEWLINE !INDENT, the missing
// block of the statement what.
func (p *parser) invalidKeywordColonIndent(kw int, what string) {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if got(&a, p.expect(kw)) && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent(what, a.line)
		return
	}
	p.mark = m
}

// invalid_except_stmt_indent:
//
//	| 'except' expression ['as' NAME ] ':' NEWLINE !INDENT
//	| 'except' ':' NEWLINE !INDENT
func (p *parser) invalidExceptStmtIndent() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	var t *node
	if got(&a, p.expect(kwExcept)) && p.expression() != nil && opt(p, &t, p.asName()) && p.tok(tCOLON) &&
		p.newlineNoIndent() {
		p.raiseNoIndent("'except' statement", a.line)
		return
	}
	if !p.retry(m) {
		return
	}
	p.invalidKeywordColonIndent(kwExcept, "'except' statement")
}

// invalid_except_star_stmt_indent: 'except' '*' expression ['as' NAME ] ':' NEWLINE !INDENT
func (p *parser) invalidExceptStarStmtIndent() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	var t *node
	if got(&a, p.expect(kwExcept)) && p.tok(tSTAR) && p.expression() != nil && opt(p, &t, p.asName()) &&
		p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("'except*' statement", a.line)
		return
	}
	p.mark = m
}

// invalid_if_stmt:
//
//	| 'if' named_expression NEWLINE
//	| 'if' named_expression ':' NEWLINE !INDENT
func (p *parser) invalidIfStmt() { p.invalidCondStmt(kwIf, "'if' statement") }

// invalid_elif_stmt: as invalid_if_stmt, with 'elif'.
func (p *parser) invalidElifStmt() { p.invalidCondStmt(kwElif, "'elif' statement") }

// invalid_while_stmt: as invalid_if_stmt, with 'while'.
func (p *parser) invalidWhileStmt() { p.invalidCondStmt(kwWhile, "'while' statement") }

func (p *parser) invalidCondStmt(kw int, what string) {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if p.tok(kw) && p.namedExpression() != nil && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	var a *ptok
	if got(&a, p.expect(kw)) && p.namedExpression() != nil && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent(what, a.line)
		return
	}
	p.mark = m
}

// invalid_else_stmt: 'else' ':' NEWLINE !INDENT
func (p *parser) invalidElseStmt() { p.invalidKeywordColonIndent(kwElse, "'else' statement") }

// invalid_for_stmt:
//
//	| [ASYNC] 'for' star_targets 'in' star_expressions NEWLINE
//	| [ASYNC] 'for' star_targets 'in' star_expressions ':' NEWLINE !INDENT
func (p *parser) invalidForStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if p.optTok(tASYNC) && p.tok(kwFor) && p.starTargets() != nil && p.tok(kwIn) && p.starExpressions() != nil &&
		p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	var a *ptok
	if p.optTok(tASYNC) && got(&a, p.expect(kwFor)) && p.starTargets() != nil && p.tok(kwIn) &&
		p.starExpressions() != nil && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("'for' statement", a.line)
		return
	}
	p.mark = m
}

// invalid_def_raw:
//
//	| [ASYNC] 'def' NAME [type_params] '(' [params] ')' ['->' expression] ':' NEWLINE !INDENT
func (p *parser) invalidDefRaw() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	var t []*node
	var params, ret *node
	if p.optTok(tASYNC) && got(&a, p.expect(kwDef)) && p.name() != nil && optSeq(p, &t, p.typeParams()) &&
		p.tok(tLPAR) && opt(p, &params, p.params()) && p.tok(tRPAR) && opt(p, &ret, group(p, func() *node {
		if p.tok(tRARROW) {
			return p.expression()
		}
		return nil
	})) && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("function definition", a.line)
		return
	}
	p.mark = m
}

// invalid_class_def_raw:
//
//	| 'class' NAME [type_params] ['(' [arguments] ')'] NEWLINE
//	| 'class' NAME [type_params] ['(' [arguments] ')'] ':' NEWLINE !INDENT
func (p *parser) invalidClassDefRaw() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	head := func() *ptok {
		var a *ptok
		var t []*node
		var b *bool
		if got(&a, p.expect(kwClass)) && p.name() != nil && optSeq(p, &t, p.typeParams()) && opt(p, &b, group(p, func() *bool {
			var z *node
			if p.tok(tLPAR) && opt(p, &z, p.arguments()) && p.tok(tRPAR) {
				ok := true
				return &ok
			}
			return nil
		})) {
			return a
		}
		return nil
	}
	if head() != nil && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	var a *ptok
	if got(&a, head()) && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("class definition", a.line)
		return
	}
	p.mark = m
}

// invalid_double_starred_kvpairs:
//
//	| ','.double_starred_kvpair+ ',' invalid_kvpair
//	| expression ':' '*' bitwise_or
//	| expression ':' &('}'|',')
func (p *parser) invalidDoubleStarredKvpairs() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if gather(p, tCOMMA, p.doubleStarredKvpair) != nil && p.tok(tCOMMA) {
		p.invalidKvpair()
	}
	if !p.retry(m) {
		return
	}
	p.invalidKvpairTail()
}

// invalid_kvpair:
//
//	| expression !(':')
//	| expression ':' '*' bitwise_or
//	| expression ':' &('}'|',')
func (p *parser) invalidKvpair() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *node
	if got(&a, p.expression()) && !p.at(tCOLON) {
		p.raiseAt("", a.lineno, "':' expected after dictionary key")
		return
	}
	if !p.retry(m) {
		return
	}
	p.invalidKvpairTail()
}

// invalidKvpairTail are the last two alternatives of invalid_kvpair and
// invalid_double_starred_kvpairs.
func (p *parser) invalidKvpairTail() {
	m := p.mark
	var a *ptok
	if p.expression() != nil && p.tok(tCOLON) && got(&a, p.expect(tSTAR)) && p.bitwiseOr() != nil {
		p.raiseAt("", a.line, "cannot use a starred expression in a dictionary value")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.expression() != nil && got(&a, p.expect(tCOLON)) && p.atAny(tRBRACE, tCOMMA) {
		p.raiseAt("", a.line, "expression expected after dictionary key and ':'")
		return
	}
	p.mark = m
}

// invalid_starred_expression: '*' expression '=' expression
func (p *parser) invalidStarredExpression() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *ptok
	if got(&a, p.expect(tSTAR)) && p.expression() != nil && p.tok(tEQUAL) && p.expression() != nil {
		p.raiseAt("", a.line, "cannot assign to iterable argument unpacking")
		return
	}
	p.mark = m
}

// invalid_replacement_field:
//
//	| '{' '='
//	| '{' '!'
//	| '{' ':'
//	| '{' '}'
//	| '{' !(yield_expr | star_expressions)
//	| '{' (yield_expr | star_expressions) !('=' | '!' | ':' | '}')
//	| '{' (yield_expr | star_expressions) '=' !('!' | ':' | '}')
//	| '{' (yield_expr | star_expressions) '='? invalid_conversion_character
//	| '{' (yield_expr | star_expressions) '='? ['!' NAME] !(':' | '}')
//	| '{' (yield_expr | star_expressions) '='? ['!' NAME] ':' fstring_format_spec* !'}'
//	| '{' (yield_expr | star_expressions) '='? ['!' NAME] !'}'
func (p *parser) invalidReplacementField() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	for _, t := range []int{tEQUAL, tEXCLAMATION, tCOLON, tRBRACE} {
		var a *ptok
		if p.tok(tLBRACE) && got(&a, p.expect(t)) {
			p.raiseAt("", a.line, "f-string: valid expression required before '"+a.text+"'")
			return
		}
		if !p.retry(m) {
			return
		}
	}
	if p.tok(tLBRACE) && !p.ahead(func() bool { return p.yieldOrStarExpressions() != nil }) {
		p.raiseOnNextToken("f-string: expecting a valid expression after '{'")
		return
	}
	if !p.retry(m) {
		return
	}
	field := func() bool { return p.tok(tLBRACE) && p.yieldOrStarExpressions() != nil }
	if field() && !p.atAny(tEQUAL, tEXCLAMATION, tCOLON, tRBRACE) {
		p.raiseOnNextToken("f-string: expecting '=', or '!', or ':', or '}'")
		return
	}
	if !p.retry(m) {
		return
	}
	if field() && p.tok(tEQUAL) && !p.atAny(tEXCLAMATION, tCOLON, tRBRACE) {
		p.raiseOnNextToken("f-string: expecting '!', or ':', or '}'")
		return
	}
	if !p.retry(m) {
		return
	}
	if field() && p.optTok(tEQUAL) && p.invalidConversionCharacter() {
		return
	}
	if !p.retry(m) {
		return
	}
	conversion := func() bool {
		return field() && p.optTok(tEQUAL) && p.maybe(group(p, func() *node {
			if p.tok(tEXCLAMATION) {
				return p.name()
			}
			return nil
		}) != nil)
	}
	if conversion() && !p.atAny(tCOLON, tRBRACE) {
		p.raiseOnNextToken("f-string: expecting ':' or '}'")
		return
	}
	if !p.retry(m) {
		return
	}
	if conversion() && p.tok(tCOLON) && loop0(p, p.fstringFormatSpec) != nil && !p.at(tRBRACE) {
		p.raiseOnNextToken("f-string: expecting '}', or format specs")
		return
	}
	if !p.retry(m) {
		return
	}
	if conversion() && !p.at(tRBRACE) {
		p.raiseOnNextToken("f-string: expecting '}'")
		return
	}
	p.mark = m
}

// invalid_conversion_character:
//
//	| '!' &(':' | '}')
//	| '!' !NAME
//
// It reports whether an alternative matched.
func (p *parser) invalidConversionCharacter() bool {
	if !p.enter() {
		return false
	}
	defer p.leave()
	m := p.mark
	if p.tok(tEXCLAMATION) && p.atAny(tCOLON, tRBRACE) {
		p.raiseOnNextToken("f-string: missing conversion character")
		return true
	}
	if !p.retry(m) {
		return false
	}
	if p.tok(tEXCLAMATION) && !p.at(tNAME) {
		p.raiseOnNextToken("f-string: invalid conversion character")
		return true
	}
	p.mark = m
	return false
}
