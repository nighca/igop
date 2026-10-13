/*
 * Copyright (c) 2026 The GoPlus Authors (goplus.org). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ixgo

import (
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestTupleAfterReuse(t *testing.T) {
	interp := loadTuple(t)
	for _, test := range []struct {
		name      string
		first     int
		later     int
		laterWant Tuple
	}{
		{name: "Pair", first: 11, later: 23, laterWant: Tuple{23, 24}},
		{name: "Recursive", first: 31, later: 42, laterWant: Tuple{42, 43}},
		{name: "Recovering", first: 11, later: -1, laterWant: Tuple{-1, -2}},
		{name: "EnvPair", first: 11, later: 23, laterWant: Tuple{23, 24}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runTuple(t, interp, test.name, test.first)
			result := runTuple(t, interp, test.name, test.first)
			want := Tuple{test.first, test.first + 1}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("result before reuse = %v, want %v", result, want)
			}
			if got := runTuple(t, interp, test.name, test.later); !reflect.DeepEqual(got, test.laterWant) {
				t.Errorf("later result = %v, want %v", got, test.laterWant)
			}
			if !reflect.DeepEqual(result, want) {
				t.Errorf("retained result = %v, want %v", result, want)
			}
		})
	}
}

func TestTupleConcurrent(t *testing.T) {
	interp := loadTuple(t)
	runTuple(t, interp, "PairCalls", 0)
	const workers, calls = 4, 40
	results := make([]Tuple, workers*calls)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for n := 0; n < calls; n++ {
				want := worker*calls + n
				value, err := interp.RunFunc("PairCalls", want)
				if err != nil {
					t.Error(err)
					return
				}
				results[want] = value.(Tuple)
			}
		}(worker)
	}
	wg.Wait()
	for want, result := range results {
		if len(result) != 2 || result[0] != 2*want+1 || result[1] != 2*want+3 {
			t.Fatalf("retained tuple = %v, want (%d, %d)", result, 2*want+1, 2*want+3)
		}
	}
}

func TestTupleClosures(t *testing.T) {
	interp := loadTuple(t)
	const calls = 64
	results := make([]Tuple, calls)
	for n := range results {
		results[n] = runTuple(t, interp, "Capture", n)
	}
	for n, result := range results {
		for slot := 0; slot < 2; slot++ {
			if got := result[slot].(func() int)(); got != n+slot {
				t.Fatalf("retained closure %d/%d returned %d, want %d", n, slot, got, n+slot)
			}
		}
	}
}

func TestFrameCleanup(t *testing.T) {
	interp := loadFrame(t, EnableCachedReg)
	p := interp.funcs[interp.mainpkg.Func("Work")]
	if len(p.Fn.Locals) != 1 || p.Fn.Locals[0].Heap || len(p.cacheRegs) == 0 {
		t.Fatal("fixture needs one reusable local and a hidden cache")
	}
	argReg := p.regIndex(p.Fn.Params[0])
	localReg := p.regIndex(p.Fn.Locals[0])
	var active *frame
	first := p.Instrs[0]
	p.Instrs[0] = func(fr *frame) {
		active = fr
		fr.phiVals = []value{new(int)}
		first(fr)
	}
	for call := 0; call < 3; call++ {
		var local value
		result := runFrame(t, interp, "Work", func() { local = active.stack[localReg] })
		if result != 7 || local == nil {
			t.Fatalf("call %d: result = %v, local = %v", call, result, local)
		}
		if active.caller != nil || active.callee != nil || active._defer != nil || active._panic != nil || active.deferid != 0 {
			t.Fatalf("call %d: returned frame retains its call chain", call)
		}
		if active.stack[argReg] != nil {
			t.Errorf("call %d: callback argument retained", call)
		}
		if active.stack[localReg] != local || !reflect.ValueOf(local).Elem().IsZero() {
			t.Errorf("call %d: reusable local was replaced or retained a value", call)
		}
		for _, reg := range p.cacheRegs {
			if active.stack[reg] != nil {
				t.Errorf("call %d: hidden cache %d retained %T", call, reg, active.stack[reg])
			}
		}
		for _, v := range active.phiVals {
			if v != nil {
				t.Errorf("call %d: phi scratch retained %T", call, v)
			}
		}
	}
}

func TestFrameSkipNonRefClear(t *testing.T) {
	interp := loadFrame(t, 0)
	p := interp.funcs[interp.mainpkg.Func("Add")]
	argReg := p.regIndex(p.Fn.Params[0])
	var active *frame
	first := p.Instrs[0]
	p.Instrs[0] = func(fr *frame) {
		active = fr
		first(fr)
	}
	if got := runFrame(t, interp, "Add", 11); got != 11 {
		t.Fatalf("Add(11) = %v", got)
	}
	if got := runFrame(t, interp, "Add", 22); got != 22 {
		t.Fatalf("Add(22) = %v", got)
	}
	if active == nil {
		t.Fatal("frame not captured")
	}
	if active.stack[0] != 22 {
		t.Errorf("int result slot cleared, got %v", active.stack[0])
	}
	if active.stack[argReg] != 22 {
		t.Errorf("int arg slot cleared, got %v", active.stack[argReg])
	}
}

func TestFrameReusesChild(t *testing.T) {
	interp := loadTuple(t)
	p := interp.funcs[interp.mainpkg.Func("Pair")]
	first := p.Instrs[0]
	var calls []*frame
	p.Instrs[0] = func(fr *frame) {
		calls = append(calls, fr)
		fr.phiVals = []value{new(int)}
		first(fr)
	}
	for n := 1; n <= 3; n++ {
		calls = nil
		if got := runTuple(t, interp, "PairCalls", n); !reflect.DeepEqual(got, Tuple{2*n + 1, 2*n + 3}) {
			t.Fatalf("PairCalls(%d) = %v", n, got)
		}
		if len(calls) != 2 || calls[0] != calls[1] {
			t.Fatal("successive calls did not reuse the returned child")
		}
		fr := calls[0]
		if fr.caller != nil || fr.callee != nil || fr._defer != nil || fr._panic != nil || fr.deferid != 0 {
			t.Fatal("cached child retained its call chain")
		}
		if fr.phiVals[0] != nil {
			t.Fatal("cached child retained phi scratch")
		}
	}
	calls = nil
	if got := runTuple(t, interp, "MixedPairCalls", 7); !reflect.DeepEqual(got, Tuple{32, 44}) {
		t.Fatalf("MixedPairCalls = %v; want [32 44]", got)
	}
	if len(calls) != 2 || calls[0] != calls[1] {
		t.Fatal("calling another function lost the cached child")
	}
}

func TestFrameElidedTupleCleanup(t *testing.T) {
	interp := loadTuple(t)
	callee := interp.funcs[interp.mainpkg.Func("RefPair")]
	parent := interp.funcs[interp.mainpkg.Func("RefCalls")]
	if callee.frameCacheSafe || parent.childFrameReg < 0 {
		t.Fatal("fixture needs a reference-bearing child and an elided tuple")
	}
	var active, child *frame
	firstParent, firstChild := parent.Instrs[0], callee.Instrs[0]
	parent.Instrs[0] = func(fr *frame) { active = fr; firstParent(fr) }
	callee.Instrs[0] = func(fr *frame) { child = fr; firstChild(fr) }
	retained := runTuple(t, interp, "RefPair", 7)
	for n := 1; n <= 3; n++ {
		if got := runFrame(t, interp, "RefCalls", n); got != 4*n+4 {
			t.Fatalf("RefCalls(%d) = %v", n, got)
		}
		for _, reg := range parent.elidedTuples {
			if active.stack[reg] != nil {
				t.Fatal("reference-bearing child retained in an elided tuple slot")
			}
		}
		if child.stack[0] != nil || child.stack[1] != nil {
			t.Fatal("returned child retained pointer results")
		}
	}
	if *retained[0].(*int) != 7 || *retained[1].(*int) != 8 {
		t.Fatal("retained results changed after frame reuse")
	}
}

func TestPairCallShapes(t *testing.T) {
	interp := loadTuple(t)
	for _, tc := range []struct {
		name string
		want Tuple
	}{
		{"CapturedPair", Tuple{7, 8}},
		{"CapturedArgPair", Tuple{14, 15}},
		{"PartialPair", Tuple{7, 8}},
	} {
		for n := 0; n < 3; n++ {
			if got := runTuple(t, interp, tc.name, 7); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s = %v; want %v", tc.name, got, tc.want)
			}
		}
	}
	for n := 0; n < 3; n++ {
		if got := runFrame(t, interp, "DiscardPair", 7); got != 7 {
			t.Fatalf("DiscardPair = %v; want 7", got)
		}
	}
	for n := 0; n < 3; n++ {
		if got := runTuple(t, interp, "TwoArgCalls", 7); !reflect.DeepEqual(got, Tuple{14, 16}) {
			t.Fatalf("TwoArgCalls = %v; want [14 16]", got)
		}
	}
}

func TestCallSpecializerRegistry(t *testing.T) {
	shape := callSpecializerShape{2, 2}
	original := callSpecializers[shape]
	called := false
	registerCallSpecializer(shape, func(interp *Interp, fn *function, args, dests []register) func(*frame) {
		called = true
		return original(interp, fn, args, dests)
	})
	defer registerCallSpecializer(shape, original)
	interp := loadTuple(t)
	if got := runTuple(t, interp, "TwoArgCalls", 7); !reflect.DeepEqual(got, Tuple{14, 16}) {
		t.Fatalf("TwoArgCalls = %v; want [14 16]", got)
	}
	if !called {
		t.Fatal("registered call specializer was not used")
	}
}

func TestTypesMayRef(t *testing.T) {
	intVar := types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])
	strField := types.NewVar(token.NoPos, nil, "s", types.Typ[types.String])
	intField := types.NewVar(token.NoPos, nil, "n", types.Typ[types.Int])
	namedInt := types.NewNamed(types.NewTypeName(token.NoPos, nil, "N", nil), types.Typ[types.Int], nil)
	cases := []struct {
		typ  types.Type
		want bool
	}{
		{nil, true},
		{types.Typ[types.Int], false},
		{types.Typ[types.Bool], false},
		{types.Typ[types.Float64], false},
		{types.Typ[types.Complex128], false},
		{namedInt, false},
		{types.Typ[types.String], true},
		{types.Typ[types.UnsafePointer], true},
		{types.NewPointer(types.Typ[types.Int]), true},
		{types.NewSlice(types.Typ[types.Int]), true},
		{types.NewMap(types.Typ[types.Int], types.Typ[types.Int]), true},
		{types.NewChan(types.SendRecv, types.Typ[types.Int]), true},
		{types.NewSignatureType(nil, nil, nil, nil, nil, false), true},
		{types.NewInterfaceType(nil, nil), true},
		{types.NewArray(types.Typ[types.Int], 0), false},
		{types.NewArray(types.NewPointer(types.Typ[types.Int]), 1), true},
		{types.NewStruct([]*types.Var{intField}, nil), false},
		{types.NewStruct([]*types.Var{strField}, nil), true},
		{types.NewTuple(intVar), true},
		{types.NewTypeParam(types.NewTypeName(token.NoPos, nil, "T", nil), types.NewInterfaceType(nil, nil)), true},
	}
	for _, test := range cases {
		if got := typesMayRef(test.typ); got != test.want {
			t.Errorf("typesMayRef(%v) = %v, want %v", test.typ, got, test.want)
		}
	}
}

func TestFrameResults(t *testing.T) {
	interp := loadFrame(t, 0)
	retained := runFrame(t, interp, "Values", 7).(Tuple)
	pointer := runFrame(t, interp, "LocalPointer", 7)
	for n := 20; n < 24; n++ {
		runFrame(t, interp, "Values", n)
		runFrame(t, interp, "LocalPointer", n)
	}
	check := func(record reflect.Value, want int) {
		t.Helper()
		if got := record.FieldByName("Numbers").Interface().([]int); !reflect.DeepEqual(got, []int{want, want + 1}) {
			t.Errorf("numbers = %v; want [%d %d]", got, want, want+1)
		}
		if got := record.FieldByName("Lookup").Interface().(map[string]int)["n"]; got != want {
			t.Errorf("map value = %d; want %d", got, want)
		}
		if got := record.FieldByName("Any").Interface(); got != want {
			t.Errorf("interface value = %v; want %d", got, want)
		}
	}
	check(reflect.ValueOf(retained[0]), 7)
	check(reflect.ValueOf(retained[1]).Elem(), 9)
	if got := retained[2].(func() int)(); got != 7 {
		t.Errorf("captured value = %d; want 7", got)
	}
	if got := *pointer.(*int); got != 7 {
		t.Errorf("returned local pointer = %d; want 7", got)
	}
}

func TestFramePanicStack(t *testing.T) {
	interp := loadFrame(t, 0)
	runFrame(t, interp, "Crash", 1)
	_, err := interp.RunFunc("Crash", -1)
	panicked, ok := err.(FatalError)
	if !ok {
		t.Fatalf("panic = %v; want FatalError", err)
	}
	stack := string(panicked.Stack())
	for _, name := range []string{"main.Crash", "main.crashLeaf", "frame.go:"} {
		if !strings.Contains(stack, name) {
			t.Errorf("panic stack missing %q:\n%s", name, stack)
		}
	}
	runFrame(t, interp, "Crash", 2)
	if string(panicked.Stack()) != stack {
		t.Error("retained panic stack changed after another call")
	}
}

func loadTuple(t *testing.T) *Interp {
	t.Helper()
	ctx := NewContext(SupportMultipleInterp)
	ctx.SetLeastCallForEnablePool(0)
	interp, err := ctx.LoadInterp("tuple.go", tupleSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(interp.UnsafeRelease)
	return interp
}

func runTuple(t *testing.T, interp *Interp, name string, n int) Tuple {
	t.Helper()
	v, err := interp.RunFunc(name, n)
	if err != nil {
		t.Fatal(err)
	}
	return v.(Tuple)
}

func loadFrame(t testing.TB, mode Mode) *Interp {
	t.Helper()
	ctx := NewContext(SupportMultipleInterp | mode)
	ctx.SetLeastCallForEnablePool(0)
	interp, err := ctx.LoadInterp("frame.go", frameSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(interp.UnsafeRelease)
	return interp
}

func runFrame(t testing.TB, interp *Interp, name string, args ...interface{}) interface{} {
	t.Helper()
	v, err := interp.RunFunc(name, args...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

const tupleSource = `package main
func Pair(n int) (int, int) { return n, n + 1 }
func TwoArgPair(a, b int) (int, int) { return a + b, a - b }
func PairCalls(n int) (int, int) {
	a, b := Pair(n)
	c, d := Pair(n + 1)
	return a + c, b + d
}
func TwoArgCalls(n int) (int, int) {
	a, b := TwoArgPair(n, n + 1)
	return TwoArgPair(a, b)
}
func DifferentPair(n int) (int, int) { return n + 10, n + 20 }
func MixedPairCalls(n int) (int, int) {
	a, b := Pair(n)
	c, d := DifferentPair(n)
	e, f := Pair(n + 1)
	return a + c + e, b + d + f
}
func RefPair(n int) (*int, *int) {
	a, b := new(int), new(int)
	*a, *b = n, n + 1
	return a, b
}
func RefCalls(n int) int {
	a, b := RefPair(n)
	c, d := RefPair(n + 1)
	return *a + *b + *c + *d
}
func CapturedPair(n int) (int, int) {
	return func() (int, int) { return n, n + 1 }()
}
func CapturedArgPair(n int) (int, int) {
	return func(x int) (int, int) { return n + x, n + x + 1 }(n)
}
func PartialPair(n int) (int, int) {
	a, _ := Pair(n)
	_, b := Pair(n)
	return a, b
}
func DiscardPair(n int) int { RefPair(n); return n }
func Recursive(n int) (int, int) {
	if n <= 10 { return n, n + 1 }
	a, b := Recursive(n - 10)
	return a + 10, b + 10
}
func Recovering(n int) (a, b int) {
	defer func() {
		if recover() != nil { a, b = -1, -2 }
	}()
	if n < 0 { panic("boom") }
	return n, n + 1
}
func EnvPair(n int) (int, int) { return pairFunc(n)() }
func pairFunc(n int) func() (int, int) {
	return func() (int, int) { return n, n + 1 }
}
func Capture(n int) (func() int, func() int) {
	data := new([256]byte)
	data[0], data[1] = byte(n), byte(n + 1)
	return func() int { return int(data[0]) }, func() int { return int(data[1]) }
}
`

const frameSource = `package main
var bias int
func Add(n int) int { return n + bias }
func Work(cb func()) int {
	bias = 3
	var local struct { callbacks [1]func(); value int }
	local.callbacks[0] = cb
	local.value = 4
	if local.callbacks[0] != nil { local.callbacks[0]() }
	return Add(local.value)
}

type Data struct {
	Numbers []int
	Lookup map[string]int
	Any interface{}
}
func Values(n int) (Data, *Data, func() int) {
	var local Data
	local.Numbers = []int{n, n + 1}
	local.Lookup = map[string]int{"n": n}
	local.Any = n
	ptr := &Data{[]int{n + 2, n + 3}, map[string]int{"n": n + 2}, n + 2}
	return local, ptr, func() int { return n }
}
func LocalPointer(n int) *int {
	var local struct{ N int }
	local.N = n
	return &local.N
}

func Crash(n int) int { return crashLeaf(n) }
func crashLeaf(n int) int {
	var p *int
	if n < 0 { return *p }
	return n
}
`
