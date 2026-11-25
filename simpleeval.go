// Copyright (c) 2013-2026 Daniel Fairhead
// SPDX-License-Identifier: MIT
//
// Ported from Python (simpleeval.py) to Go, 2026-11-25.

// Package simpleeval is a short, easy to use, safe and reasonably extensible
// expression evaluator: a Go port of the Python simpleeval library. An
// expression is Python 3.12 source, parsed as ast.parse parses it and
// evaluated node by node with Python's semantics, using only the operators,
// functions and names the evaluator is given.
//
//	s, _ := simpleeval.NewSimpleEval(nil, nil, nil, nil)
//	v, _ := s.Eval("20 + 30")           // 50
//	v, _ = simpleeval.Eval("40 + two", nil, nil, names, nil)
package simpleeval

import (
	"math/big"
	"math/rand"
	"slices"
	"strconv"
	"strings"

	"github.com/danthedeckie/simpleeval/internal/py"
)

// The limits. Like the original's module globals, they are read each time
// they are checked, so changing one affects every evaluator.
var (
	// MaxStringLength is the longest string literal, and the longest
	// result of + and * on sequences and of a piece of an f-string.
	MaxStringLength = 100000
	// MaxComprehensionLength is the most items the comprehensions of one
	// evaluation may produce.
	MaxComprehensionLength = 10000
	// MaxPower is the highest absolute value of either side of **.
	MaxPower = 4000000
	// MaxShift is the highest absolute shift count of << and >>.
	MaxShift = 10000
	// MaxShiftBase is the highest absolute value shifted by << and >>:
	// int(sys.float_info.max).
	MaxShiftBase = maxFloatAsInt()
	// DisallowPrefixes are the attribute name prefixes never looked up.
	DisallowPrefixes = []string{"_", "func_"}
	// DisallowMethods are the attribute names never looked up.
	DisallowMethods = []string{"format", "format_map", "mro", "tb_frame", "gi_frame", "ag_frame", "cr_frame", "exec"}
	// AttrIndexFallback is the default of SimpleEval.AttrIndexFallback.
	AttrIndexFallback = true
)

func maxFloatAsInt() *big.Int {
	i, _ := new(big.Float).SetFloat64(1.7976931348623157e308).Int(nil)
	return i
}

// DisallowFunctions are the functions no evaluator calls or hands back: the
// builtins that inspect, build or run code or reach the file system, the os
// functions that run programs, and operator's attribute and item getters.
// An evaluator refuses to be built with one among its functions.
var DisallowFunctions = disallowedFunctions()

func disallowedFunctions() []Value {
	var out []Value
	for _, name := range []string{"type", "isinstance", "eval", "getattr", "setattr", "repr", "compile", "open",
		"exec", "globals", "locals", "help"} {
		out = append(out, py.Builtin(name))
	}
	osModule := py.Module("os")
	for _, name := range []string{"popen", "system", "execl", "execle", "execlp", "execlpe", "execv", "execve",
		"execvp", "execvpe", "posix_spawn", "posix_spawnp", "spawnl", "spawnle", "spawnlp", "spawnlpe", "spawnv",
		"spawnve", "spawnvp", "spawnvpe"} {
		out = append(out, mustAttr(osModule, name))
	}
	operatorModule := py.Module("operator")
	for _, name := range []string{"attrgetter", "itemgetter", "methodcaller", "call"} {
		out = append(out, mustAttr(operatorModule, name))
	}
	return out
}

func mustAttr(v Value, name string) Value {
	a, err := py.GetAttr(v, name)
	if err != nil {
		panic("simpleeval: " + err.Error())
	}
	return a
}

// BasicAllowedAttrs lists, per type name, the attributes an evaluator whose
// AllowedAttrs is BasicAllowedAttrs lets expressions reach. Its "None" entry
// is the original's entry keyed by None itself rather than by NoneType, so it
// applies to no value.
var BasicAllowedAttrs = map[string][]string{
	"int": {"as_integer_ratio", "bit_length", "conjugate", "denominator", "from_bytes", "imag", "numerator",
		"real", "to_bytes"},
	"float": {"as_integer_ratio", "conjugate", "fromhex", "hex", "imag", "is_integer", "real"},
	"str": {"capitalize", "casefold", "center", "count", "encode", "endswith", "expandtabs", "find", "format",
		"format_map", "index", "isalnum", "isalpha", "isascii", "isdecimal", "isdigit", "isidentifier", "islower",
		"isnumeric", "isprintable", "isspace", "istitle", "isupper", "join", "ljust", "lower", "lstrip",
		"maketrans", "partition", "removeprefix", "removesuffix", "replace", "rfind", "rindex", "rjust",
		"rpartition", "rsplit", "rstrip", "split", "splitlines", "startswith", "strip", "swapcase", "title",
		"translate", "upper", "zfill"},
	"bool": {"as_integer_ratio", "bit_length", "conjugate", "denominator", "from_bytes", "imag", "numerator",
		"real", "to_bytes"},
	"None": {},
	"dict": {"clear", "copy", "fromkeys", "get", "items", "keys", "pop", "popitem", "setdefault", "update",
		"values"},
	"list": {"pop", "append", "index", "reverse", "count", "sort", "copy", "extend", "clear", "insert", "remove"},
	"set": {"pop", "intersection_update", "intersection", "issubset", "symmetric_difference_update", "discard",
		"isdisjoint", "difference_update", "issuperset", "add", "copy", "union", "clear", "update",
		"symmetric_difference", "difference", "remove"},
	"tuple": {"index", "count"},
}

// The exception classes. Every one derives from InvalidExpression, which
// derives from Exception; AssignmentAttempted and MultipleExpressions are
// warning categories deriving from UserWarning.
func init() {
	py.DefineClass("simpleeval", "InvalidExpression", "Exception")
	for _, name := range []string{"FunctionNotDefined", "NameNotDefined", "AttributeDoesNotExist",
		"OperatorNotDefined", "FeatureNotAvailable", "NumberTooHigh", "IterableTooLong"} {
		py.DefineClass("simpleeval", name, "InvalidExpression")
	}
	py.DefineClass("simpleeval", "AssignmentAttempted", "UserWarning")
	py.DefineClass("simpleeval", "MultipleExpressions", "UserWarning")
}

// NewFunctionNotDefined is FunctionNotDefined(funcName, expression).
func NewFunctionNotDefined(funcName, expression string) *Error {
	msg := "Function '" + funcName + "' not defined, for expression '" + expression + "'."
	e := py.Raise("FunctionNotDefined", py.String(msg))
	e.SetAttr("message", py.String(msg))
	e.SetAttr("func_name", py.String(funcName))
	e.SetAttr("expression", py.String(expression))
	return e
}

// NewNameNotDefined is NameNotDefined(name, expression), which a names
// handler returns for a name it does not define.
func NewNameNotDefined(name, expression string) *Error {
	msg := "'" + name + "' is not defined for expression '" + expression + "'"
	e := py.Raise("NameNotDefined", py.String(msg))
	e.SetAttr("name", py.String(name))
	e.SetAttr("message", py.String(msg))
	e.SetAttr("expression", py.String(expression))
	return e
}

// NewAttributeDoesNotExist is AttributeDoesNotExist(attr, expression).
func NewAttributeDoesNotExist(attr, expression string) *Error {
	msg := "Attribute '" + attr + "' does not exist in expression '" + expression + "'"
	e := py.Raise("AttributeDoesNotExist", py.String(msg))
	e.SetAttr("message", py.String(msg))
	e.SetAttr("attr", py.String(attr))
	e.SetAttr("expression", py.String(expression))
	return e
}

// operatorNotDefined is OperatorNotDefined(op, expression), op being the
// operator node.
func operatorNotDefined(op, expression string) error {
	node := py.OperatorObject(op)
	text, err := py.Str(node)
	if err != nil {
		return err
	}
	msg := "Operator '" + text + "' does not exist in expression '" + expression + "'"
	e := py.Raise("OperatorNotDefined", py.String(msg))
	e.SetAttr("message", py.String(msg))
	e.SetAttr("attr", node)
	e.SetAttr("expression", py.String(expression))
	return e
}

func featureNotAvailable(msg string) error { return py.Raise("FeatureNotAvailable", py.String(msg)) }

// ModuleWrapper is the class of NewModuleWrapper's objects.
var moduleWrapperClass = py.DefineClass("simpleeval", "ModuleWrapper", "")

// NewModuleWrapper wraps a module so that expressions can reach its
// attributes: modules themselves are refused. Private names and
// DisallowMethods stay unreachable; a non-nil allowedAttrs further limits the
// reachable names to those it lists.
func NewModuleWrapper(module Value, allowedAttrs []string) (Value, error) {
	if !py.IsModule(module) {
		typ, err := py.Str(py.TypeOf(module))
		if err != nil {
			return nil, err
		}
		return nil, py.Raise("TypeError", py.String("ModuleWrapper requires a module, got "+typ))
	}
	var allowed []string
	if allowedAttrs != nil {
		allowed = append([]string{}, allowedAttrs...)
	}
	return py.NewObject(py.Object{Module: "simpleeval", Class: "ModuleWrapper",
		GetAttr: func(name string) (Value, error) {
			if strings.HasPrefix(name, "_") {
				return nil, featureNotAvailable("Access to private attribute '" + name + "' is not allowed")
			}
			if slices.Contains(DisallowMethods, name) {
				return nil, featureNotAvailable("Method '" + name + "' is not allowed on modules")
			}
			if allowed != nil && !slices.Contains(allowed, name) {
				return nil, featureNotAvailable("Access to '" + name + "' is not allowed on this wrapped module")
			}
			return py.GetAttr(module, name)
		}}), nil
}

// The default operator functions that limit their operands.

// pyCompare is `a <op> b` with Python's result.
func pyCompare(op string, a, b Value) (bool, error) {
	r, err := py.Compare(op, a, b)
	if err != nil {
		return false, err
	}
	return py.Truth(r)
}

// hasLen is hasattr(v, "__len__").
func hasLen(v Value) bool {
	ok, err := py.HasAttr(v, "__len__")
	return err == nil && ok
}

// lenOf is len(v) as a Python int.
func lenOf(v Value) (Value, error) {
	n, err := py.Len(v)
	return py.Int(int64(n)), err
}

// exceeds is `abs(v) > limit`.
func exceeds(v Value, limit Value) (bool, error) {
	a, err := py.Call(py.Builtin("abs"), []Value{v}, nil)
	if err != nil {
		return false, err
	}
	return pyCompare("Gt", a, limit)
}

// randomInt is random_int(top): a random int below top.
func randomInt(args []Value, kwargs []Keyword) (Value, error) {
	if err := arity("random_int", []string{"top"}, args, kwargs); err != nil {
		return nil, err
	}
	r, err := py.BinaryOp("Mult", py.Float(rand.Float64()), args[0])
	if err != nil {
		return nil, err
	}
	return py.Call(py.Builtin("int"), []Value{r}, nil)
}

// safePower is a ** b, refusing operands larger than MaxPower.
func safePower(args []Value, kwargs []Keyword) (Value, error) {
	if err := arity("safe_power", []string{"a", "b"}, args, kwargs); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	limit := py.Int(int64(MaxPower))
	for _, v := range []Value{a, b} {
		over, err := exceeds(v, limit)
		if err != nil {
			return nil, err
		}
		if over {
			return nil, numberTooHigh(a, "**", b)
		}
	}
	return py.BinaryOp("Pow", a, b)
}

// safeMult is a * b, refusing a repeated sequence longer than
// MaxStringLength.
func safeMult(args []Value, kwargs []Keyword) (Value, error) {
	if err := arity("safe_mult", []string{"a", "b"}, args, kwargs); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	for _, pair := range [][2]Value{{a, b}, {b, a}} {
		seq, times := pair[0], pair[1]
		if !hasLen(seq) {
			continue
		}
		n, err := lenOf(seq)
		if err != nil {
			return nil, err
		}
		total, err := py.BinaryOp("Mult", times, n)
		if err != nil {
			return nil, err
		}
		over, err := pyCompare("Gt", total, py.Int(int64(MaxStringLength)))
		if err != nil {
			return nil, err
		}
		if over {
			return nil, py.Raise("IterableTooLong", py.String("Sorry, I will not evaluate something that long."))
		}
	}
	return py.BinaryOp("Mult", a, b)
}

// safeAdd is a + b, refusing a sum of sequences longer than MaxStringLength.
func safeAdd(args []Value, kwargs []Keyword) (Value, error) {
	if err := arity("safe_add", []string{"a", "b"}, args, kwargs); err != nil {
		return nil, err
	}
	a, b := args[0], args[1]
	if hasLen(a) && hasLen(b) {
		la, err := lenOf(a)
		if err != nil {
			return nil, err
		}
		lb, err := lenOf(b)
		if err != nil {
			return nil, err
		}
		total, err := py.BinaryOp("Add", la, lb)
		if err != nil {
			return nil, err
		}
		over, err := pyCompare("Gt", total, py.Int(int64(MaxStringLength)))
		if err != nil {
			return nil, err
		}
		if over {
			return nil, py.Raise("IterableTooLong",
				py.String("Sorry, adding those two together would make something too long."))
		}
	}
	return py.BinaryOp("Add", a, b)
}

// safeShift is a << b or a >> b, refusing a shift count above MaxShift or a
// shifted value above MaxShiftBase.
func safeShift(name, op, symbol string) func(args []Value, kwargs []Keyword) (Value, error) {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := arity(name, []string{"a", "b"}, args, kwargs); err != nil {
			return nil, err
		}
		a, b := args[0], args[1]
		over, err := exceeds(b, py.Int(int64(MaxShift)))
		if err != nil {
			return nil, err
		}
		if !over {
			if over, err = exceeds(a, py.BigInt(MaxShiftBase)); err != nil {
				return nil, err
			}
		}
		if over {
			return nil, numberTooHigh(a, symbol, b)
		}
		return py.BinaryOp(op, a, b)
	}
}

// numberTooHigh is NumberTooHigh(f"Sorry! I don't want to evaluate {a} op {b}").
func numberTooHigh(a Value, symbol string, b Value) error {
	sa, err := py.Str(a)
	if err != nil {
		return err
	}
	sb, err := py.Str(b)
	if err != nil {
		return err
	}
	return py.Raise("NumberTooHigh", py.String("Sorry! I don't want to evaluate "+sa+" "+symbol+" "+sb))
}

// arity checks the arguments of a Python function with the given positional
// parameters, as a call to it would.
func arity(name string, params []string, args []Value, kwargs []Keyword) error {
	bound := make([]bool, len(params))
	for i := range args {
		if i >= len(params) {
			return py.Raise("TypeError", py.String(name+"() takes "+itoa(len(params))+" positional argument"+
				plural(len(params))+" but "+itoa(len(args))+" were given"))
		}
		bound[i] = true
	}
	for _, kw := range kwargs {
		i := slices.Index(params, kw.Name)
		if i < 0 {
			return py.Raise("TypeError", py.String(name+"() got an unexpected keyword argument '"+kw.Name+"'"))
		}
		if bound[i] {
			return py.Raise("TypeError", py.String(name+"() got multiple values for argument '"+kw.Name+"'"))
		}
		bound[i] = true
	}
	var missing []string
	for i, ok := range bound {
		if !ok {
			missing = append(missing, "'"+params[i]+"'")
		}
	}
	if len(missing) > 0 {
		list := strings.Join(missing, " and ")
		if len(missing) > 2 {
			list = strings.Join(missing[:len(missing)-1], ", ") + ", and " + missing[len(missing)-1]
		}
		return py.Raise("TypeError", py.String(name+"() missing "+itoa(len(missing))+" required positional argument"+
			plural(len(missing))+": "+list))
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func itoa(n int) string { return strconv.Itoa(n) }

// The defaults for the evaluator.

var (
	safeAddFunc    = py.Func("safe_add", safeAdd)
	safeMultFunc   = py.Func("safe_mult", safeMult)
	safePowerFunc  = py.Func("safe_power", safePower)
	safeLShiftFunc = py.Func("safe_lshift", safeShift("safe_lshift", "LShift", "<<"))
	safeRShiftFunc = py.Func("safe_rshift", safeShift("safe_rshift", "RShift", ">>"))
	randomIntFunc  = py.Func("random_int", randomInt)
	// random.random: a method of the random module's shared Random instance.
	randomFunc = py.BoundMethod(py.NewObject(py.Object{Module: "random", Class: "Random"}), "random",
		func(args []Value, kwargs []Keyword) (Value, error) {
			if len(kwargs) > 0 {
				return nil, py.Raise("TypeError", py.String("Random.random() takes no keyword arguments"))
			}
			if len(args) > 0 {
				return nil, py.Raise("TypeError", py.String("Random.random() takes no arguments ("+
					itoa(len(args))+" given)"))
			}
			return py.Float(rand.Float64()), nil
		})
)

// DefaultOperators returns a new map of the default operator functions,
// keyed by the class name of the operator's node (Add, USub, Eq, In, ...).
func DefaultOperators() map[string]Value {
	op := py.Module("operator")
	ops := map[string]Value{
		"Add": safeAddFunc, "Mult": safeMultFunc, "Pow": safePowerFunc,
		"LShift": safeLShiftFunc, "RShift": safeRShiftFunc,
	}
	for name, fn := range map[string]string{"Sub": "sub", "Div": "truediv", "FloorDiv": "floordiv", "Mod": "mod",
		"Eq": "eq", "NotEq": "ne", "Gt": "gt", "Lt": "lt", "GtE": "ge", "LtE": "le", "Not": "not_", "USub": "neg",
		"UAdd": "pos", "BitXor": "xor", "BitOr": "or_", "BitAnd": "and_", "Invert": "invert"} {
		ops[name] = mustAttr(op, fn)
	}
	for _, name := range []string{"In", "NotIn", "Is", "IsNot"} {
		name := name
		ops[name] = py.Func("<lambda>", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := arity("<lambda>", []string{"x", "y"}, args, kwargs); err != nil {
				return nil, err
			}
			return py.Compare(name, args[0], args[1])
		})
	}
	return ops
}

// DefaultFunctions returns a new dict of the default functions: rand,
// randint, int, float and str.
func DefaultFunctions() Value {
	d, _ := Dict(py.String("rand"), randomFunc, py.String("randint"), randomIntFunc,
		py.String("int"), py.Builtin("int"), py.String("float"), py.Builtin("float"),
		py.String("str"), py.Builtin("str"))
	return d
}

// DefaultNames returns a new dict of the default names: True, False and
// None.
func DefaultNames() Value {
	d, _ := Dict(py.String("True"), py.Bool(true), py.String("False"), py.Bool(false),
		py.String("None"), py.None())
	return d
}

// The evaluator.

// Node is a parsed statement, as Parse returns it.
type Node struct {
	node py.Node
}

// Type is the class name of the statement's node (Expr, Assign, ...).
func (n *Node) Type() string { return n.node.Kind() }

// handler evaluates one kind of node.
type handler func(n py.Node) (Value, error)

// SimpleEval is a very simple expression evaluator. Operators maps the class
// name of an operator node to the function applying it, Functions is a
// mapping of the functions an expression may call, Names a mapping of the
// names it may use (or a callable given the Name node, or any other value),
// and AllowedAttrs, when not nil, lists per type name the attributes an
// expression may reach.
type SimpleEval struct {
	Operators         map[string]Value
	Functions         Value
	Names             Value
	AllowedAttrs      map[string][]string
	AttrIndexFallback bool
	// Expr is the expression being evaluated, for error messages.
	Expr  string
	nodes map[string]handler
}

// NewSimpleEval returns an evaluator. Nil operators, functions or names (or
// None) give the defaults; an empty map or dict gives none.
func NewSimpleEval(operators map[string]Value, functions, names Value, allowedAttrs map[string][]string) (*SimpleEval, error) {
	s := &SimpleEval{}
	if err := s.init(operators, functions, names, allowedAttrs); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SimpleEval) init(operators map[string]Value, functions, names Value, allowedAttrs map[string][]string) error {
	if operators == nil {
		operators = DefaultOperators()
	}
	if functions == nil || isNone(functions) {
		functions = DefaultFunctions()
	}
	if names == nil || isNone(names) {
		names = DefaultNames()
	}
	s.Operators, s.Functions, s.Names, s.AllowedAttrs = operators, functions, names, allowedAttrs
	s.nodes = map[string]handler{
		"Expr": s.evalExpr, "Assign": s.evalAssign, "AugAssign": s.evalAssign, "Import": s.evalImport,
		"Name": s.evalName, "UnaryOp": s.evalUnaryOp, "BinOp": s.evalBinOp, "BoolOp": s.evalBoolOp,
		"Compare": s.evalCompare, "IfExp": s.evalIfExp, "Call": s.evalCall, "keyword": s.evalKeyword,
		"Subscript": s.evalSubscript, "Attribute": s.evalAttribute, "Slice": s.evalSlice,
		"JoinedStr": s.evalJoinedStr, "FormattedValue": s.evalFormattedValue, "Constant": s.evalConstant,
	}
	s.AttrIndexFallback = AttrIndexFallback
	values, err := py.CallMethod(s.Functions, "values")
	if err != nil {
		return err
	}
	return py.Iterate(values, func(f Value) error {
		bad, err := isDisallowed(f)
		if err != nil {
			return err
		}
		if bad {
			text, err := py.Str(f)
			if err != nil {
				return err
			}
			return featureNotAvailable("This function " + text + " is a really bad idea.")
		}
		return nil
	})
}

func isNone(v Value) bool { return py.SameObject(v, py.None()) }

// isDisallowed is `f in DISALLOW_FUNCTIONS`, a set membership test.
func isDisallowed(f Value) (bool, error) {
	set, err := py.Set(true, DisallowFunctions...)
	if err != nil {
		return false, err
	}
	return py.Contains(set, f)
}

// primitiveTypes are the types whose values are always safe: they cannot be
// modules or disallowed functions, nor contain any.
var primitiveTypes = map[string]bool{"int": true, "float": true, "str": true, "bool": true, "NoneType": true,
	"bytes": true, "complex": true}

// checkDisallowedItems refuses a module (a wrapped one is allowed) or a
// disallowed function, looking into lists, tuples, sets and dict values.
func (s *SimpleEval) checkDisallowedItems(item Value) error {
	typ := py.TypeName(item)
	switch {
	case primitiveTypes[typ]:
		return nil
	case py.TypeOf(item) == moduleWrapperClass:
		return nil
	case py.IsModule(item):
		return featureNotAvailable("Sorry, modules are not allowed")
	case typ == "list" || typ == "tuple" || typ == "set" || typ == "frozenset":
		return py.Iterate(item, s.checkDisallowedItems)
	case typ == "dict":
		values, err := py.CallMethod(item, "values")
		if err != nil {
			return err
		}
		return py.Iterate(values, s.checkDisallowedItems)
	case py.IsCallable(item):
		bad, err := isDisallowed(item)
		if err != nil {
			return err
		}
		if bad {
			return featureNotAvailable("This function is forbidden")
		}
	}
	return nil
}

// Parse parses an expression into the node of its first statement. An
// expression of several statements issues a MultipleExpressions warning.
func Parse(expr string) (*Node, error) {
	module, err := py.ParseModule(py.Strip(expr))
	if err != nil {
		return nil, err
	}
	body := module.Stmts()
	if len(body) == 0 {
		return nil, py.Raise("InvalidExpression", py.String("Sorry, cannot evaluate empty string"))
	}
	if len(body) > 1 {
		py.Warn("MultipleExpressions", "'"+expr+"' contains multiple expressions. Only the first will be used.")
	}
	return &Node{body[0]}, nil
}

// Eval evaluates an expression with the evaluator's operators, functions and
// names.
func (s *SimpleEval) Eval(expr string) (Value, error) { return s.EvalParsed(expr, nil) }

// EvalParsed evaluates parsed, a node Parse returned, as the expression
// expr; a nil parsed parses expr.
func (s *SimpleEval) EvalParsed(expr string, parsed *Node) (Value, error) {
	s.Expr = expr
	if parsed == nil {
		var err error
		if parsed, err = Parse(expr); err != nil {
			return nil, err
		}
	}
	return s.eval(parsed.node)
}

// eval evaluates one node and checks its value.
func (s *SimpleEval) eval(n py.Node) (Value, error) {
	h, ok := s.nodes[n.Kind()]
	if !ok {
		return nil, featureNotAvailable("Sorry, " + n.Kind() + " is not available in this evaluator")
	}
	result, err := h(n)
	if err != nil {
		return nil, err
	}
	if err := s.checkDisallowedItems(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SimpleEval) evalExpr(n py.Node) (Value, error) { return s.eval(n.Value()) }

func (s *SimpleEval) evalAssign(n py.Node) (Value, error) {
	py.Warn("AssignmentAttempted", "Assignment ("+s.Expr+") attempted, but this is ignored")
	return s.eval(n.Value())
}

func (s *SimpleEval) evalImport(py.Node) (Value, error) {
	return nil, featureNotAvailable("Sorry, 'import' is not allowed.")
}

func (s *SimpleEval) evalConstant(n py.Node) (Value, error) {
	v := n.Constant()
	if hasLen(v) {
		length, err := py.Len(v)
		if err != nil {
			return nil, err
		}
		if length > MaxStringLength {
			return nil, py.Raise("IterableTooLong", py.String("Literal in statement is too long! ("+itoa(length)+
				", when "+itoa(MaxStringLength)+" is max)"))
		}
	}
	return v, nil
}

// operator is the function the evaluator applies for an operator node.
func (s *SimpleEval) operator(op string) (Value, error) {
	f, ok := s.Operators[op]
	if !ok {
		return nil, operatorNotDefined(op, s.Expr)
	}
	return f, nil
}

func (s *SimpleEval) evalUnaryOp(n py.Node) (Value, error) {
	f, err := s.operator(n.Op())
	if err != nil {
		return nil, err
	}
	operand, err := s.eval(n.Operand())
	if err != nil {
		return nil, err
	}
	return py.Call(f, []Value{operand}, nil)
}

func (s *SimpleEval) evalBinOp(n py.Node) (Value, error) {
	f, err := s.operator(n.Op())
	if err != nil {
		return nil, err
	}
	left, err := s.eval(n.Left())
	if err != nil {
		return nil, err
	}
	right, err := s.eval(n.Right())
	if err != nil {
		return nil, err
	}
	return py.Call(f, []Value{left, right}, nil)
}

// evalBoolOp gives the value that decided an `and` or an `or`: the first
// false (or true) operand, or the last.
func (s *SimpleEval) evalBoolOp(n py.Node) (Value, error) {
	result := py.Bool(false)
	for _, operand := range n.Values() {
		v, err := s.eval(operand)
		if err != nil {
			return nil, err
		}
		result = v
		t, err := py.Truth(v)
		if err != nil {
			return nil, err
		}
		if t == (n.Op() == "Or") {
			break
		}
	}
	return result, nil
}

func (s *SimpleEval) evalCompare(n py.Node) (Value, error) {
	right, err := s.eval(n.Left())
	if err != nil {
		return nil, err
	}
	result := py.Bool(true)
	for i, comparator := range n.Comparators() {
		t, err := py.Truth(result)
		if err != nil {
			return nil, err
		}
		if !t {
			break
		}
		left := right
		if right, err = s.eval(comparator); err != nil {
			return nil, err
		}
		op := n.Ops()[i]
		f, ok := s.Operators[op]
		if !ok {
			return nil, py.Raise("KeyError", py.NodeClass(op))
		}
		if result, err = py.Call(f, []Value{left, right}, nil); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *SimpleEval) evalIfExp(n py.Node) (Value, error) {
	test, err := s.eval(n.Test())
	if err != nil {
		return nil, err
	}
	t, err := py.Truth(test)
	if err != nil {
		return nil, err
	}
	if t {
		return s.eval(n.Body())
	}
	return s.eval(n.OrElse())
}

func (s *SimpleEval) evalCall(n py.Node) (Value, error) {
	var fn Value
	callee := n.Func()
	if callee.Kind() == "Attribute" {
		var err error
		if fn, err = s.eval(callee); err != nil {
			return nil, err
		}
	} else {
		if callee.Kind() != "Name" {
			return nil, featureNotAvailable("Lambda Functions not implemented")
		}
		f, err := py.GetItem(s.Functions, py.String(callee.ID()))
		if err != nil {
			if py.ErrorIs(err, "KeyError") {
				return nil, NewFunctionNotDefined(callee.ID(), s.Expr)
			}
			return nil, err
		}
		bad, err := isDisallowed(f)
		if err != nil {
			return nil, err
		}
		if bad {
			return nil, featureNotAvailable("This function is forbidden")
		}
		fn = f
	}
	args := make([]Value, 0, len(n.Args()))
	for _, a := range n.Args() {
		v, err := s.eval(a)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	// dict(self._eval(k) for k in node.keywords): later names replace
	// earlier ones, and a ** item becomes the key None.
	kwargs := py.NewDict()
	for _, k := range n.Keywords() {
		pair, err := s.eval(k)
		if err != nil {
			return nil, err
		}
		key, err := py.GetItem(pair, py.Int(0))
		if err != nil {
			return nil, err
		}
		value, err := py.GetItem(pair, py.Int(1))
		if err != nil {
			return nil, err
		}
		if err := py.SetItem(kwargs, key, value); err != nil {
			return nil, err
		}
	}
	keywords, err := keywordArgs(fn, kwargs)
	if err != nil {
		return nil, err
	}
	return py.Call(fn, args, keywords)
}

// keywordArgs unpacks the keyword dict of a call: fn(**kwargs) requires a
// callable fn and str keys.
func keywordArgs(fn, kwargs Value) ([]Keyword, error) {
	if !py.IsCallable(fn) {
		return nil, py.Raise("TypeError", py.String("'"+py.TypeName(fn)+"' object is not callable"))
	}
	var out []Keyword
	items, err := py.CallMethod(kwargs, "items")
	if err != nil {
		return nil, err
	}
	err = py.Iterate(items, func(item Value) error {
		key, _ := py.GetItem(item, py.Int(0))
		value, _ := py.GetItem(item, py.Int(1))
		name, ok := py.AsString(key)
		if !ok {
			return py.Raise("TypeError", py.String("keywords must be strings"))
		}
		out = append(out, Keyword{Name: name, Value: value})
		return nil
	})
	return out, err
}

func (s *SimpleEval) evalKeyword(n py.Node) (Value, error) {
	v, err := s.eval(n.Value())
	if err != nil {
		return nil, err
	}
	if !n.HasArg() {
		return py.Tuple(py.None(), v), nil
	}
	return py.Tuple(py.String(n.ID()), v), nil
}

func (s *SimpleEval) evalName(n py.Node) (Value, error) {
	v, err := py.GetItem(s.Names, py.String(n.ID()))
	if err == nil {
		return v, nil
	}
	if !py.ErrorIs(err, "TypeError") && !py.ErrorIs(err, "KeyError") {
		return nil, err
	}
	if py.IsCallable(s.Names) {
		v, err := py.Call(s.Names, []Value{py.NodeObject(n)}, nil)
		if err == nil {
			return v, nil
		}
		if !py.ErrorIs(err, "NameNotDefined") {
			return nil, err
		}
	} else if ok, err := py.HasAttr(s.Names, "__getitem__"); err != nil {
		return nil, err
	} else if !ok {
		return nil, py.Raise("InvalidExpression", py.String("Trying to use name (variable) \""+n.ID()+
			"\" when no \"names\" defined for evaluator"))
	}
	defined, err := py.Contains(s.Functions, py.String(n.ID()))
	if err != nil {
		return nil, err
	}
	if defined {
		return py.GetItem(s.Functions, py.String(n.ID()))
	}
	return nil, NewNameNotDefined(n.ID(), s.Expr)
}

func (s *SimpleEval) evalSubscript(n py.Node) (Value, error) {
	container, err := s.eval(n.Value())
	if err != nil {
		return nil, err
	}
	key, err := s.eval(n.Slice())
	if err != nil {
		return nil, err
	}
	return py.GetItem(container, key)
}

func (s *SimpleEval) evalAttribute(n py.Node) (Value, error) {
	attr := n.ID()
	// The disallowed names are refused before the object is evaluated.
	for _, prefix := range DisallowPrefixes {
		if strings.HasPrefix(attr, prefix) {
			return nil, featureNotAvailable("Sorry, access to __attributes  or func_ attributes is not available. (" +
				attr + ")")
		}
	}
	if slices.Contains(DisallowMethods, attr) {
		return nil, featureNotAvailable("Sorry, this method is not available. (" + attr + ")")
	}
	obj, err := s.eval(n.Value())
	if err != nil {
		return nil, err
	}
	if s.AllowedAttrs != nil {
		typ, err := py.Str(py.TypeOf(obj))
		if err != nil {
			return nil, err
		}
		allowed, ok := s.AllowedAttrs[py.TypeName(obj)]
		if !ok {
			return nil, featureNotAvailable("Sorry, attribute access not allowed on '" + typ +
				"' (attempted to access `." + attr + "`)")
		}
		if !slices.Contains(allowed, attr) {
			return nil, featureNotAvailable("Sorry, '." + attr + "' access not allowed on '" + typ + "'")
		}
	}
	item, err := py.GetAttr(obj, attr)
	if err != nil {
		if !py.ErrorIs(err, "AttributeError") && !py.ErrorIs(err, "TypeError") {
			return nil, err
		}
		item = nil
		if s.AttrIndexFallback {
			v, ierr := py.GetItem(obj, py.String(attr))
			switch {
			case ierr == nil:
				item = v
			case !py.ErrorIs(ierr, "KeyError") && !py.ErrorIs(ierr, "TypeError"):
				return nil, ierr
			}
		}
	}
	if item == nil {
		return nil, NewAttributeDoesNotExist(attr, s.Expr)
	}
	if py.IsModule(item) {
		return nil, featureNotAvailable("Sorry, modules are not allowed in attribute access")
	}
	if py.IsCallable(item) {
		bad, err := isDisallowed(item)
		if err != nil {
			return nil, err
		}
		if bad {
			return nil, featureNotAvailable("This function is forbidden")
		}
	}
	return item, nil
}

func (s *SimpleEval) evalSlice(n py.Node) (Value, error) {
	bounds := []Value{py.None(), py.None(), py.None()}
	for i, child := range []py.Node{n.Lower(), n.Upper(), n.Step()} {
		if child.IsNone() {
			continue
		}
		v, err := s.eval(child)
		if err != nil {
			return nil, err
		}
		bounds[i] = v
	}
	return py.Call(py.Builtin("slice"), bounds, nil)
}

// evalJoinedStr is an f-string: the str() of each piece, joined. Each piece
// on its own may not be longer than MaxStringLength.
func (s *SimpleEval) evalJoinedStr(n py.Node) (Value, error) {
	var b strings.Builder
	for _, piece := range n.Values() {
		v, err := s.eval(piece)
		if err != nil {
			return nil, err
		}
		text, err := py.Str(v)
		if err != nil {
			return nil, err
		}
		length, err := py.Len(py.String(text))
		if err != nil {
			return nil, err
		}
		if length > MaxStringLength {
			return nil, py.Raise("IterableTooLong", py.String("Sorry, I will not evaluate something this long."))
		}
		b.WriteString(text)
	}
	return py.String(b.String()), nil
}

// evalFormattedValue is a replacement field of an f-string: its value
// formatted with "{:spec}".format(value) when it has a format spec. The
// conversion (!s, !r, !a) is not applied.
func (s *SimpleEval) evalFormattedValue(n py.Node) (Value, error) {
	if n.FormatSpec().IsNone() {
		return s.eval(n.Value())
	}
	spec, err := s.eval(n.FormatSpec())
	if err != nil {
		return nil, err
	}
	fmt, err := py.BinaryOp("Add", py.String("{:"), spec)
	if err != nil {
		return nil, err
	}
	if fmt, err = py.BinaryOp("Add", fmt, py.String("}")); err != nil {
		return nil, err
	}
	v, err := s.eval(n.Value())
	if err != nil {
		return nil, err
	}
	return py.CallMethod(fmt, "format", v)
}

// EvalWithCompoundTypes is SimpleEval with lists, tuples, dicts and sets,
// their constructor functions (list, tuple, dict, set) and list and dict
// comprehensions (and generator expressions, evaluated to lists).
type EvalWithCompoundTypes struct {
	SimpleEval
	count int
}

// NewEvalWithCompoundTypes returns an evaluator for compound types. The
// constructor functions are added to functions, which the caller sees.
func NewEvalWithCompoundTypes(operators map[string]Value, functions, names Value, allowedAttrs map[string][]string) (*EvalWithCompoundTypes, error) {
	s := &EvalWithCompoundTypes{}
	if err := s.init(operators, functions, names, allowedAttrs); err != nil {
		return nil, err
	}
	for _, name := range []string{"list", "tuple", "dict", "set"} {
		if err := py.SetItem(s.Functions, py.String(name), py.Builtin(name)); err != nil {
			return nil, err
		}
	}
	for kind, h := range map[string]handler{"Dict": s.evalDict, "Tuple": s.evalTuple, "List": s.evalList,
		"Set": s.evalSet, "ListComp": s.evalComprehension, "GeneratorExp": s.evalComprehension,
		"DictComp": s.evalComprehension} {
		s.nodes[kind] = h
	}
	return s, nil
}

// Eval evaluates an expression; the comprehension length limit applies to
// each evaluation anew.
func (s *EvalWithCompoundTypes) Eval(expr string) (Value, error) { return s.EvalParsed(expr, nil) }

// EvalParsed evaluates parsed (or expr when parsed is nil), as
// SimpleEval.EvalParsed does.
func (s *EvalWithCompoundTypes) EvalParsed(expr string, parsed *Node) (Value, error) {
	s.count = 0
	return s.SimpleEval.EvalParsed(expr, parsed)
}

// evalDict builds a dict; for each item the value is evaluated before the
// key, and a ** item is merged with dict.update.
func (s *EvalWithCompoundTypes) evalDict(n py.Node) (Value, error) {
	result := py.NewDict()
	keys, values := n.Keys(), n.Values()
	for i := range keys {
		if keys[i].IsNone() {
			v, err := s.eval(values[i])
			if err != nil {
				return nil, err
			}
			if _, err := py.CallMethod(result, "update", v); err != nil {
				return nil, err
			}
			continue
		}
		v, err := s.eval(values[i])
		if err != nil {
			return nil, err
		}
		k, err := s.eval(keys[i])
		if err != nil {
			return nil, err
		}
		if err := py.SetItem(result, k, v); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// evalList builds a list; a *item extends it with an iterable.
func (s *EvalWithCompoundTypes) evalList(n py.Node) (Value, error) {
	result := py.List()
	for _, item := range n.Elts() {
		if item.Kind() == "Starred" {
			v, err := s.eval(item.Value())
			if err != nil {
				return nil, err
			}
			if _, err := py.CallMethod(result, "extend", v); err != nil {
				return nil, err
			}
			continue
		}
		v, err := s.eval(item)
		if err != nil {
			return nil, err
		}
		if _, err := py.CallMethod(result, "append", v); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *EvalWithCompoundTypes) evalTuple(n py.Node) (Value, error) {
	items, err := s.evalAll(n.Elts())
	if err != nil {
		return nil, err
	}
	return py.Tuple(items...), nil
}

func (s *EvalWithCompoundTypes) evalSet(n py.Node) (Value, error) {
	result, _ := py.Set(false)
	for _, elt := range n.Elts() {
		v, err := s.eval(elt)
		if err != nil {
			return nil, err
		}
		if _, err := py.CallMethod(result, "add", v); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *EvalWithCompoundTypes) evalAll(nodes []py.Node) ([]Value, error) {
	out := make([]Value, 0, len(nodes))
	for _, x := range nodes {
		v, err := s.eval(x)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// evalComprehension evaluates a list or dict comprehension or a generator
// expression (to a list). The names its targets bind shadow the
// evaluator's names while it runs.
func (s *EvalWithCompoundTypes) evalComprehension(n py.Node) (Value, error) {
	isDict := n.Kind() == "DictComp"
	var result Value
	if isDict {
		result = py.NewDict()
	} else {
		result = py.List()
	}
	extra := map[string]Value{}
	previous := s.nodes["Name"]
	s.nodes["Name"] = func(name py.Node) (Value, error) {
		if v, ok := extra[name.ID()]; ok {
			return v, nil
		}
		return previous(name)
	}
	defer func() { s.nodes["Name"] = previous }()

	var bind func(target py.Node, value Value) error
	bind = func(target py.Node, value Value) error {
		if target.Kind() == "Name" {
			extra[target.ID()] = value
			return nil
		}
		if target.Kind() != "Tuple" && target.Kind() != "List" {
			return py.Raise("AttributeError", py.String("'"+target.Kind()+"' object has no attribute 'elts'"))
		}
		// zip(target.elts, value): as many as the shorter of the two.
		it, err := py.Iter(value)
		if err != nil {
			return err
		}
		for _, t := range target.Elts() {
			v, more, err := it.Next()
			if err != nil || !more {
				return err
			}
			if err := bind(t, v); err != nil {
				return err
			}
		}
		return nil
	}

	generators := n.Generators()
	var run func(gi int) error
	run = func(gi int) error {
		g := generators[gi]
		iterable, err := s.eval(g.Iter())
		if err != nil {
			return err
		}
		return py.Iterate(iterable, func(item Value) error {
			s.count++
			if s.count > MaxComprehensionLength {
				return py.Raise("IterableTooLong", py.String("Comprehension generates too many elements"))
			}
			if err := bind(g.Target(), item); err != nil {
				return err
			}
			for _, cond := range g.Ifs() {
				v, err := s.eval(cond)
				if err != nil {
					return err
				}
				t, err := py.Truth(v)
				if err != nil {
					return err
				}
				if !t {
					return nil
				}
			}
			if gi+1 < len(generators) {
				return run(gi + 1)
			}
			if !isDict {
				v, err := s.eval(n.Elt())
				if err != nil {
					return err
				}
				_, err = py.CallMethod(result, "append", v)
				return err
			}
			v, err := s.eval(n.Value())
			if err != nil {
				return err
			}
			k, err := s.eval(n.Key())
			if err != nil {
				return err
			}
			return py.SetItem(result, k, v)
		})
	}
	if err := run(0); err != nil {
		return nil, err
	}
	return result, nil
}

// Eval evaluates an expression with a new SimpleEval: simple_eval.
func Eval(expr string, operators map[string]Value, functions, names Value, allowedAttrs map[string][]string) (Value, error) {
	s, err := NewSimpleEval(operators, functions, names, allowedAttrs)
	if err != nil {
		return nil, err
	}
	return s.Eval(expr)
}
