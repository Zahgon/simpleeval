// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The syntax tree ast.parse builds: one node type whose kind is the CPython class name, with the
// fields of that class and its source positions.

package py

// node is an ast.AST. Which fields are used depends on kind.
type node struct {
	kind string
	// Positions as CPython reports them: lines from 1, columns as UTF-8
	// byte offsets. hasPos is false for the classes without positions
	// (arguments, comprehension, withitem, and the operator and context
	// singletons).
	lineno, col, endLineno, endCol int
	hasPos                         bool

	// id is Name.id, Attribute.attr, FunctionDef.name, ClassDef.name,
	// arg.arg, keyword.arg, alias.name, ExceptHandler.name,
	// ImportFrom.module, the name of the type parameters, MatchAs.name,
	// MatchStar.name and MatchMapping.rest; noID marks Python's None for the
	// optional ones.
	id   string
	noID bool
	// asname is alias.asname (noAsname for None).
	asname   string
	noAsname bool
	// ctx is Load, Store or Del for the expression kinds that carry one.
	ctx string
	// op is the operator class of BinOp, UnaryOp, BoolOp and AugAssign.
	op string
	// ops are Compare's operators.
	ops []string
	// value is Constant.value.
	value Value
	// conversion is FormattedValue.conversion (-1 for none).
	conversion int
	// level is ImportFrom.level, AnnAssign.simple and
	// comprehension.is_async.
	level int

	// Single children, by role.
	a, b, c *node
	// List children, by role.
	l1, l2, l3, l4 []*node
	// body, orelse and finalbody of the compound statements; Module.body.
	body, orelse, finalbody []*node
}

// The roles of a, b, c and l1..l4 per kind:
//
//	Expr, Return, Starred, Await          a=value
//	Assign                                l1=targets a=value
//	AugAssign                             a=target b=value
//	AnnAssign                             a=target b=annotation c=value
//	Delete                                l1=targets
//	If, While                             a=test
//	For, AsyncFor                         a=target b=iter
//	With, AsyncWith                       l1=items
//	withitem                              a=context_expr b=optional_vars
//	Try, TryStar                          l1=handlers
//	ExceptHandler                         a=type
//	Raise                                 a=exc b=cause
//	Assert                                a=test b=msg
//	Import                                l1=names
//	ImportFrom                            l1=names
//	FunctionDef, AsyncFunctionDef         a=args l1=decorator_list b=returns l2=type_params
//	ClassDef                              l1=bases l2=keywords l3=decorator_list l4=type_params
//	Global, Nonlocal                      l1=names (alias nodes holding the identifiers)
//	TypeAlias                             a=name l1=type_params b=value
//	TypeVar                               a=bound
//	Match                                 a=subject l1=cases
//	match_case                            a=pattern b=guard
//	MatchValue                            a=value
//	MatchSingleton                        value
//	MatchSequence, MatchOr                l1=patterns
//	MatchMapping                          l1=keys l2=patterns
//	MatchClass                            a=cls l1=patterns l2=kwd_attrs (alias nodes) l3=kwd_patterns
//	MatchAs                               a=pattern
//	arguments                             l1=posonlyargs l2=args a=vararg l3=kwonlyargs l4=kw_defaults b=kwarg; defaults in body
//	arg                                   a=annotation
//	Lambda                                a=args b=body
//	BoolOp                                l1=values
//	NamedExpr                             a=target b=value
//	BinOp                                 a=left b=right
//	UnaryOp                               a=operand
//	IfExp                                 a=test b=body c=orelse
//	Dict                                  l1=keys (nil for **) l2=values
//	Set, List, Tuple                      l1=elts
//	ListComp, SetComp, GeneratorExp       a=elt l1=generators
//	DictComp                              a=key b=value l1=generators
//	comprehension                         a=target b=iter l1=ifs
//	Compare                               a=left l1=comparators
//	Call                                  a=func l1=args l2=keywords
//	keyword                               a=value
//	FormattedValue                        a=value b=format_spec
//	JoinedStr                             l1=values
//	Attribute                             a=value
//	Subscript                             a=value b=slice
//	Slice                                 a=lower b=upper c=step
//	Yield, YieldFrom                      a=value
