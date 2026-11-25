// Copyright (c) 2013-2026 Daniel Fairhead
// SPDX-License-Identifier: MIT
//
// Ported from Python (tests/) to Go, 2026-11-25.

package simpleeval_test

import (
	"errors"
	"testing"

	se "github.com/danthedeckie/simpleeval"
)

func mustRepr(t *testing.T, v se.Value, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, err := se.Repr(v)
	if err != nil {
		t.Fatalf("repr: %v", err)
	}
	return r
}

// reprOf adapts mustRepr to a call returning a value and an error.
func reprOf(t *testing.T) func(se.Value, error) string {
	return func(v se.Value, err error) string { return mustRepr(t, v, err) }
}

func wantError(t *testing.T, err error, class, message string) {
	t.Helper()
	var pe *se.Error
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want %s", err, class)
	}
	if pe.Class != class || pe.Message != message {
		t.Errorf("got %s: %s, want %s: %s", pe.Class, pe.Message, class, message)
	}
}

func TestSimpleEvalFunction(t *testing.T) {
	names, _ := se.Dict(se.String("two"), se.Int(2))
	v, err := se.Eval("40 + two", nil, nil, names, nil)
	if got := mustRepr(t, v, err); got != "42" {
		t.Errorf("got %s, want 42", got)
	}
}

func TestOperatorsCanBeRemovedAndReplaced(t *testing.T) {
	ops := se.DefaultOperators()
	delete(ops, "Add")
	ops["Sub"] = ops["Mult"]
	s, err := se.NewSimpleEval(ops, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reprOf(t)(s.Eval("6 - 7")); got != "42" {
		t.Errorf("6 - 7 with Sub as multiplication: got %s", got)
	}
	_, err = s.Eval("1 + 1")
	var pe *se.Error
	if !errors.As(err, &pe) || pe.Class != "OperatorNotDefined" {
		t.Errorf("1 + 1 without Add: got %v", err)
	}
}

func TestLimitsAreReadWhenChecked(t *testing.T) {
	old := se.MaxPower
	defer func() { se.MaxPower = old }()
	se.MaxPower = 10
	_, err := se.Eval("11 ** 2", nil, nil, nil, nil)
	wantError(t, err, "NumberTooHigh", "Sorry! I don't want to evaluate 11 ** 2")
}

func TestParsedNodeIsReused(t *testing.T) {
	node, err := se.Parse("1 + 1")
	if err != nil {
		t.Fatal(err)
	}
	if node.Type() != "Expr" {
		t.Errorf("node type %q, want Expr", node.Type())
	}
	s, err := se.NewSimpleEval(nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reprOf(t)(s.EvalParsed("ignored", node)); got != "2" {
		t.Errorf("got %s, want 2", got)
	}
}

func TestMultipleExpressionsWarn(t *testing.T) {
	var got string
	warned := se.CatchWarnings(func() {
		v, err := se.Eval("1; 2", nil, nil, nil, nil)
		got = mustRepr(t, v, err)
	})
	if got != "1" {
		t.Errorf("got %s, want 1", got)
	}
	if len(warned) != 1 || warned[0].Category != "MultipleExpressions" {
		t.Fatalf("warnings %v", warned)
	}
	if want := "'1; 2' contains multiple expressions. Only the first will be used."; warned[0].Message != want {
		t.Errorf("message %q, want %q", warned[0].Message, want)
	}
}

func TestNamesHandlerFallsBackToFunctions(t *testing.T) {
	handler := se.Func("handler", func(args []se.Value, _ []se.Keyword) (se.Value, error) {
		id, err := se.GetAttr(args[0], "id")
		if err != nil {
			return nil, err
		}
		name, err := se.Str(id)
		if err != nil {
			return nil, err
		}
		if name == "known" {
			return se.Int(7), nil
		}
		return nil, se.NewNameNotDefined(name, "handler")
	})
	s, err := se.NewSimpleEval(nil, nil, handler, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reprOf(t)(s.Eval("known * 6")); got != "42" {
		t.Errorf("got %s, want 42", got)
	}
	if got := reprOf(t)(s.Eval("int")); got != "<class 'int'>" {
		t.Errorf("got %s, want <class 'int'>", got)
	}
	_, err = s.Eval("other")
	wantError(t, err, "NameNotDefined", "'other' is not defined for expression 'other'")
}

func TestDisallowedFunctionsAreRefused(t *testing.T) {
	functions, _ := se.Dict(se.String("ev"), se.Builtin("eval"))
	_, err := se.NewSimpleEval(nil, functions, nil, nil)
	wantError(t, err, "FeatureNotAvailable", "This function <built-in function eval> is a really bad idea.")
}

func TestCompoundTypesAddConstructors(t *testing.T) {
	functions := se.DefaultFunctions()
	if _, err := se.NewEvalWithCompoundTypes(nil, functions, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list", "tuple", "dict", "set"} {
		if _, err := se.GetItem(functions, se.String(name)); err != nil {
			t.Errorf("functions has no %s: %v", name, err)
		}
	}
}

func TestModuleWrapperRestrictsNames(t *testing.T) {
	m, err := se.NewModuleWrapper(se.Module("math"), []string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	names, _ := se.Dict(se.String("m"), m)
	s, err := se.NewSimpleEval(nil, nil, names, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reprOf(t)(s.Eval("m.pi")); got != "3.141592653589793" {
		t.Errorf("got %s", got)
	}
	_, err = s.Eval("m.e")
	wantError(t, err, "FeatureNotAvailable", "Access to 'e' is not allowed on this wrapped module")
	_, err = se.NewModuleWrapper(se.Int(3), nil)
	wantError(t, err, "TypeError", "ModuleWrapper requires a module, got <class 'int'>")
}
