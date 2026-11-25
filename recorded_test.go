// Copyright (c) 2013-2026 Daniel Fairhead
// SPDX-License-Identifier: MIT
//
// Ported from Python (simpleeval.py) to Go, 2026-11-25.

package simpleeval_test

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"testing"

	se "github.com/danthedeckie/simpleeval"
)

// recordedCase is one expectation testdata/record.py recorded from the
// original simpleeval on CPython 3.12.
type recordedCase struct {
	Expr     string   `json:"expr"`
	Simple   bool     `json:"simple"`
	Outcome  string   `json:"outcome"`
	Warnings []string `json:"warnings"`
}

// address matches the object address of a default repr, which differs from
// run to run.
var address = regexp.MustCompile(` at 0x[0-9a-f]+>`)

func TestRecordedExpressions(t *testing.T) {
	data, err := os.ReadFile("testdata/recorded.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []recordedCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Expr, func(t *testing.T) {
			var got string
			warned := se.CatchWarnings(func() { got = evaluate(t, c) })
			if address.ReplaceAllString(got, " at 0x?>") != address.ReplaceAllString(c.Outcome, " at 0x?>") {
				t.Errorf("%q: got %q, want %q", c.Expr, got, c.Outcome)
			}
			var gotWarnings []string
			for _, w := range warned {
				gotWarnings = append(gotWarnings, w.Category+": "+w.Message)
			}
			if len(gotWarnings) != len(c.Warnings) {
				t.Fatalf("%q: warnings %q, want %q", c.Expr, gotWarnings, c.Warnings)
			}
			for i := range gotWarnings {
				if gotWarnings[i] != c.Warnings[i] {
					t.Errorf("%q: warning %d is %q, want %q", c.Expr, i, gotWarnings[i], c.Warnings[i])
				}
			}
		})
	}
}

// evaluate runs one recorded case as record.py does and formats its
// outcome the same way.
func evaluate(t *testing.T, c recordedCase) string {
	t.Helper()
	functions := se.DefaultFunctions()
	for _, name := range []string{"abs", "len", "max", "min", "sorted", "sum"} {
		if err := se.SetItem(functions, se.String(name), se.Builtin(name)); err != nil {
			t.Fatal(err)
		}
	}
	math, err := se.NewModuleWrapper(se.Module("math"), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := se.Dict(se.String("key"), se.String("value"))
	if err != nil {
		t.Fatal(err)
	}
	names, err := se.Dict(se.String("x"), se.List(se.Int(1), se.Int(2)), se.String("d"), d, se.String("m"), math)
	if err != nil {
		t.Fatal(err)
	}
	var v se.Value
	if c.Simple {
		s, err := se.NewSimpleEval(nil, functions, names, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, err = s.Eval(c.Expr)
		if err != nil {
			return failure(t, err)
		}
	} else {
		s, err := se.NewEvalWithCompoundTypes(nil, functions, names, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, err = s.Eval(c.Expr)
		if err != nil {
			return failure(t, err)
		}
	}
	r, err := se.Repr(v)
	if err != nil {
		return failure(t, err)
	}
	return "= " + r
}

func failure(t *testing.T, err error) string {
	t.Helper()
	var pe *se.Error
	if !errors.As(err, &pe) {
		t.Fatalf("not a Python exception: %v", err)
	}
	return "! " + pe.Class + ": " + pe.Message
}
