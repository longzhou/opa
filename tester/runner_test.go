// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package tester_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/tester"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util/test"
)

func TestRunner_EnableFailureLine(t *testing.T) {

	ctx := context.Background()

	files := map[string]string{
		"/a_test.rego": `package foo
			test_a { 
				true
				false
				true
			}
			test_b { 
				false
				true
			}
			test_c {
				input.x = 1  # indexer understands this
			}`,
	}

	tests := map[[2]string]struct {
		wantErr  bool
		wantFail bool
		FailRow  int
	}{
		{"data.foo", "test_a"}: {false, true, 4},
		{"data.foo", "test_b"}: {false, true, 8},
		{"data.foo", "test_c"}: {false, true, 0},
	}

	test.WithTempFS(files, func(d string) {
		paths := []string{d}
		modules, store, err := tester.Load(paths, nil)
		if err != nil {
			t.Fatal(err)
		}
		ch, err := tester.NewRunner().EnableFailureLine(true).SetStore(store).Run(ctx, modules)
		if err != nil {
			t.Fatal(err)
		}
		var rs []*tester.Result
		for r := range ch {
			rs = append(rs, r)
		}
		seen := map[[2]string]struct{}{}
		for i := range rs {
			k := [2]string{rs[i].Package, rs[i].Name}
			seen[k] = struct{}{}
			exp, ok := tests[k]
			if !ok {
				t.Errorf("Unexpected result for %v", k)
			} else if exp.wantErr != (rs[i].Error != nil) || exp.wantFail != rs[i].Fail {
				t.Errorf("Expected %v for %v but got: %v", exp, k, rs[i])
			} else if exp.FailRow != 0 {
				if rs[i].FailedAt == nil || rs[i].FailedAt.Location == nil {
					t.Errorf("Failed line not set")
				} else if rs[i].FailedAt.Location.Row != exp.FailRow {
					t.Errorf("Expected Failed Line %v but got: %v", exp.FailRow, rs[i].FailedAt.Location.Row)
				}
			} else if rs[i].FailedAt != nil {
				t.Errorf("Failed line set, but expected not set.")
			}
		}
		// This makes sure all tests were executed
		for k := range tests {
			if _, ok := seen[k]; !ok {
				t.Errorf("Expected result for %v", k)
			}
		}
	})
}

func TestRun(t *testing.T) {

	ctx := context.Background()

	files := map[string]string{
		"/a.rego": `package foo
			allow { true }
			`,
		"/a_test.rego": `package foo
			test_pass { allow }
			non_test { true }
			test_fail { not allow }
			test_fail_non_bool = 100
			test_err { conflict }
			conflict = true
			conflict = false
			test_duplicate { false }
			test_duplicate { true }
			test_duplicate { true }
			`,
		"/b_test.rego": `package bar

		test_duplicate { true }`,
	}

	tests := map[[2]string]struct {
		wantErr  bool
		wantFail bool
	}{
		{"data.foo", "test_pass"}:          {false, false},
		{"data.foo", "test_fail"}:          {false, true},
		{"data.foo", "test_fail_non_bool"}: {false, true},
		{"data.foo", "test_duplicate"}:     {false, true},
		{"data.foo", "test_duplicate#01"}:  {false, false},
		{"data.foo", "test_duplicate#02"}:  {false, false},
		{"data.foo", "test_err"}:           {true, false},
		{"data.bar", "test_duplicate"}:     {false, false},
	}

	test.WithTempFS(files, func(d string) {

		rs, err := tester.Run(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[[2]string]struct{}{}
		for i := range rs {
			k := [2]string{rs[i].Package, rs[i].Name}
			seen[k] = struct{}{}
			exp, ok := tests[k]
			if !ok {
				t.Errorf("Unexpected result for %v", k)
			} else if exp.wantErr != (rs[i].Error != nil) || exp.wantFail != rs[i].Fail {
				t.Errorf("Expected %v for %v but got: %v", exp, k, rs[i])
			}
		}
		for k := range tests {
			if _, ok := seen[k]; !ok {
				t.Errorf("Expected result for %v", k)
			}
		}
	})
}

func TestRunnerCancel(t *testing.T) {

	registerSleepBuiltin()
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	module := `package foo

	test_1 { test.sleep("100ms") }
	test_2 { true }`

	files := map[string]string{
		"/a_test.rego": module,
	}

	test.WithTempFS(files, func(d string) {
		results, err := tester.Run(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if !topdown.IsCancel(results[0].Error) {
			t.Fatalf("Expected cancel error but got: %v", results[0].Error)
		}
	})
}

func TestRunner_Timeout(t *testing.T) {

	registerSleepBuiltin()

	ctx := context.Background()

	files := map[string]string{
		"/a_test.rego": `package foo

		test_1 { test.sleep("100ms") }
		test_2 { true }`,
	}

	test.WithTempFS(files, func(d string) {
		paths := []string{d}
		modules, store, err := tester.Load(paths, nil)
		if err != nil {
			t.Fatal(err)
		}
		duration, err := time.ParseDuration("15ms")
		if err != nil {
			t.Fatal(err)
		}
		ch, err := tester.NewRunner().SetTimeout(duration).SetStore(store).Run(ctx, modules)
		if err != nil {
			t.Fatal(err)
		}
		var results []*tester.Result
		for r := range ch {
			results = append(results, r)
		}
		if !topdown.IsCancel(results[0].Error) {
			t.Fatalf("Expected cancel error but got: %v", results[0].Error)
		}
		if topdown.IsCancel(results[1].Error) {
			t.Fatalf("Expected no error for second test, but it timed out")
		}
	})
}

func registerSleepBuiltin() {
	ast.RegisterBuiltin(&ast.Builtin{
		Name: "test.sleep",
		Decl: types.NewFunction(
			types.Args(types.S),
			types.NewNull(),
		),
	})

	topdown.RegisterFunctionalBuiltin1("test.sleep", func(a ast.Value) (ast.Value, error) {
		d, _ := time.ParseDuration(string(a.(ast.String)))
		time.Sleep(d)
		return ast.Null{}, nil
	})
}