// Ported from CPython 3.12 (Lib/ast.py, Python/Python-ast.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The read-only view of the syntax tree ast.parse builds, and the tree's
// nodes as Python objects.

package py

import (
	"strings"
	"sync"
)

// Node is a node of the tree ast.parse builds. The zero Node stands for
// None (an absent optional child).
type Node struct {
	n *node
}

// ParseModule is ast.parse(source): the Module node, or the SyntaxError
// (with the file name "<unknown>"). Warnings the parser issues (invalid
// escape sequences, number literals running into keywords) go to Warn.
func ParseModule(source string) (Node, error) {
	// compile() refuses a null byte before the parser runs.
	if strings.IndexByte(source, 0) >= 0 {
		return Node{}, toError(newPyErr("SyntaxError", "source code string cannot contain null bytes"))
	}
	tree, err := parseModule(source)
	if err != nil {
		return Node{}, toError(err.exception("<unknown>"))
	}
	if tooDeep(tree, astMaxDepth) {
		return Node{}, toError(newPyErr("RecursionError", "maximum recursion depth exceeded during ast construction"))
	}
	return Node{tree}, nil
}

// astMaxDepth is how deeply nodes may nest when ast.parse turns the tree
// into Python objects (ast2obj, limited to C_RECURSION_LIMIT, 10000, levels
// of which the call of ast.parse already uses two).
const astMaxDepth = 10000 - 2

// tooDeep reports whether the tree under n nests more than depth levels.
func tooDeep(n *node, depth int) bool {
	if n == nil {
		return false
	}
	if depth == 0 {
		return true
	}
	for _, c := range []*node{n.a, n.b, n.c} {
		if tooDeep(c, depth-1) {
			return true
		}
	}
	for _, l := range [][]*node{n.l1, n.l2, n.l3, n.l4, n.body, n.orelse, n.finalbody} {
		for _, c := range l {
			if tooDeep(c, depth-1) {
				return true
			}
		}
	}
	return false
}

// IsNone reports whether the node stands for None.
func (n Node) IsNone() bool { return n.n == nil }

// Kind is the node's class name: Expr, Name, BinOp, Constant, ...
func (n Node) Kind() string { return n.n.kind }

// Op is the operator class name of a BinOp, UnaryOp, BoolOp or AugAssign.
func (n Node) Op() string { return n.n.op }

// Ops are the operator class names of a Compare.
func (n Node) Ops() []string { return append([]string(nil), n.n.ops...) }

// ID is Name.id, Attribute.attr or keyword.arg ("" for a ** keyword).
func (n Node) ID() string { return n.n.id }

// HasArg reports whether a keyword has a name (keyword.arg is not None).
func (n Node) HasArg() bool { return !n.n.noID }

// Constant is Constant.value.
func (n Node) Constant() Value { return n.n.value }

// Stmts is Module.body.
func (n Node) Stmts() []Node { return wrapNodes(n.n.body) }

// Value is the value child of Expr, Assign, AugAssign, Attribute,
// Subscript, Starred, keyword, FormattedValue and DictComp.
func (n Node) Value() Node {
	if n.n.kind == "DictComp" || n.n.kind == "AugAssign" {
		return Node{n.n.b}
	}
	return Node{n.n.a}
}

// Left is BinOp.left and Compare.left.
func (n Node) Left() Node { return Node{n.n.a} }

// Right is BinOp.right.
func (n Node) Right() Node { return Node{n.n.b} }

// Operand is UnaryOp.operand.
func (n Node) Operand() Node { return Node{n.n.a} }

// Test, Body and OrElse are the children of IfExp.
func (n Node) Test() Node { return Node{n.n.a} }

// Body is IfExp.body.
func (n Node) Body() Node { return Node{n.n.b} }

// OrElse is IfExp.orelse.
func (n Node) OrElse() Node { return Node{n.n.c} }

// Func is Call.func.
func (n Node) Func() Node { return Node{n.n.a} }

// Args is Call.args.
func (n Node) Args() []Node { return wrapNodes(n.n.l1) }

// Keywords is Call.keywords.
func (n Node) Keywords() []Node { return wrapNodes(n.n.l2) }

// Slice is Subscript.slice.
func (n Node) Slice() Node { return Node{n.n.b} }

// Lower, Upper and Step are the children of Slice.
func (n Node) Lower() Node { return Node{n.n.a} }

// Upper is Slice.upper.
func (n Node) Upper() Node { return Node{n.n.b} }

// Step is Slice.step.
func (n Node) Step() Node { return Node{n.n.c} }

// Values is BoolOp.values, JoinedStr.values and Dict.values.
func (n Node) Values() []Node {
	if n.n.kind == "Dict" {
		return wrapNodes(n.n.l2)
	}
	return wrapNodes(n.n.l1)
}

// Keys is Dict.keys, with a None node for each ** item.
func (n Node) Keys() []Node { return wrapNodes(n.n.l1) }

// Comparators is Compare.comparators.
func (n Node) Comparators() []Node { return wrapNodes(n.n.l1) }

// Elts is Tuple.elts, List.elts and Set.elts.
func (n Node) Elts() []Node { return wrapNodes(n.n.l1) }

// Elt is ListComp.elt, SetComp.elt and GeneratorExp.elt.
func (n Node) Elt() Node { return Node{n.n.a} }

// Key is DictComp.key.
func (n Node) Key() Node { return Node{n.n.a} }

// Generators are the comprehension nodes of a comprehension.
func (n Node) Generators() []Node { return wrapNodes(n.n.l1) }

// Target is comprehension.target.
func (n Node) Target() Node { return Node{n.n.a} }

// Iter is comprehension.iter.
func (n Node) Iter() Node { return Node{n.n.b} }

// Ifs is comprehension.ifs.
func (n Node) Ifs() []Node { return wrapNodes(n.n.l1) }

// FormatSpec is FormattedValue.format_spec.
func (n Node) FormatSpec() Node { return Node{n.n.b} }

func wrapNodes(ns []*node) []Node {
	out := make([]Node, len(ns))
	for i, x := range ns {
		out[i] = Node{x}
	}
	return out
}

// The base classes of the node classes, as Lib/ast.py's class tree has them.
var astBases = map[string]string{
	"Load": "expr_context", "Store": "expr_context", "Del": "expr_context",
	"And": "boolop", "Or": "boolop",
	"Add": "operator", "Sub": "operator", "Mult": "operator", "MatMult": "operator", "Div": "operator",
	"Mod": "operator", "Pow": "operator", "LShift": "operator", "RShift": "operator", "BitOr": "operator",
	"BitXor": "operator", "BitAnd": "operator", "FloorDiv": "operator",
	"Invert": "unaryop", "Not": "unaryop", "UAdd": "unaryop", "USub": "unaryop",
	"Eq": "cmpop", "NotEq": "cmpop", "Lt": "cmpop", "LtE": "cmpop", "Gt": "cmpop", "GtE": "cmpop",
	"Is": "cmpop", "IsNot": "cmpop", "In": "cmpop", "NotIn": "cmpop",
}

// NodeClass is the class ast.<name> (ast.Eq, ast.Name, ...).
func NodeClass(name string) Value { return astClass(name) }

func astClass(name string) *classV {
	base := astBases[name]
	if base == "" {
		base = "AST"
	}
	defineClass("ast", "AST", "")
	if base != "AST" {
		defineClass("ast", base, "AST")
	}
	return defineClass("ast", name, base)
}

var (
	nodeObjMu sync.Mutex
	nodeObjs  = map[*node]Value{}
	opObjs    = map[string]Value{}
)

// NodeObject is the node as the Python object ast.parse returns: one object
// per node, with the node's fields and positions as attributes.
func NodeObject(n Node) Value {
	nodeObjMu.Lock()
	defer nodeObjMu.Unlock()
	if v, ok := nodeObjs[n.n]; ok {
		return v
	}
	astClass(n.n.kind)
	attrs := map[string]Value{}
	if n.n.kind == "Name" {
		attrs["id"] = strV(n.n.id)
		attrs["ctx"] = operatorObjectLocked(n.n.ctx)
	}
	if n.n.hasPos {
		attrs["lineno"] = mkInt(int64(n.n.lineno))
		attrs["col_offset"] = mkInt(int64(n.n.col))
		attrs["end_lineno"] = mkInt(int64(n.n.endLineno))
		attrs["end_col_offset"] = mkInt(int64(n.n.endCol))
	}
	v := &Object{Module: "ast", Class: n.n.kind, Attrs: attrs}
	nodeObjs[n.n] = v
	return v
}

// OperatorObject is the instance of an operator or context class the parser
// puts in a tree (ast.Add(), ast.Load(), ...): CPython shares one per class.
func OperatorObject(name string) Value {
	nodeObjMu.Lock()
	defer nodeObjMu.Unlock()
	return operatorObjectLocked(name)
}

func operatorObjectLocked(name string) Value {
	if v, ok := opObjs[name]; ok {
		return v
	}
	astClass(name)
	v := &Object{Module: "ast", Class: name}
	opObjs[name] = v
	return v
}
