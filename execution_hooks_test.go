package ixgo

import (
	"reflect"
	"sync"
	"testing"

	"github.com/visualfc/gid"
)

const executionHooksSource = `package main
func Direct(wait func()) func() { return func() { wait() } }
func Reflect(wait func()) func([]byte) int {
	return func(data []byte) int { wait(); return len(data) }
}
func Nested(invoke func(func()), mark func(string)) {
	invoke(func() { mark("inner") })
	mark("outer")
}
func Panic(mark func(string)) { defer mark("defer"); panic("cancelled") }
func Recover(invoke func(func()), mark func(string)) {
	invoke(func() {
		defer func() { recover(); mark("recovered") }()
		panic("recovered")
	})
	mark("outer")
}
func Spawn(f func()) { go func() { f() }() }
func main() {}
`

func hooksInterp(t *testing.T, ctx *Context) *Interp {
	t.Helper()
	ctx.SetLeastCallForEnablePool(0)
	i, err := ctx.LoadInterp("hooks.go", executionHooksSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(i.UnsafeRelease)
	return i
}

func TestExecutionHooksConcurrentCallbacks(t *testing.T) {
	for _, name := range []string{"Direct", "Reflect"} {
		t.Run(name, func(t *testing.T) {
			entered, left := make(chan uint64, 8), make(chan uint64, 8)
			ctx := NewContext(SupportMultipleInterp | EnableCachedReg)
			ctx.SetExecutionHooks(func() { entered <- gid.Get() }, func() { left <- gid.Get() })
			i := hooksInterp(t, ctx)
			body := make(chan uint64, 2)
			release := make(chan struct{})
			wait := func() { body <- gid.Get(); <-release }
			fn := runFunc(t, i, name, wait)
			<-entered
			<-left
			var invoke func()
			if name == "Direct" {
				invoke = fn.(func())
			} else {
				invoke = func() {
					if got := fn.(func([]byte) int)([]byte{1, 2}); got != 2 {
						t.Errorf("result = %d; want 2", got)
					}
				}
			}
			var wg sync.WaitGroup
			for range 2 {
				wg.Go(invoke)
			}
			ids := map[uint64]bool{<-body: true, <-body: true}
			for range 2 {
				if id := <-entered; !ids[id] {
					t.Error("enter hook ran on a different goroutine")
				}
			}
			select {
			case <-left:
				t.Error("waiting invocation left early")
			default:
			}
			close(release)
			wg.Wait()
			for range 2 {
				id := <-left
				if !ids[id] {
					t.Fatalf("unexpected leave for goroutine %d", id)
				}
				delete(ids, id)
			}
		})
	}
}

func TestExecutionHooksNestedAndPanic(t *testing.T) {
	ctx := NewContext(SupportMultipleInterp | EnableCachedReg)
	var events []string
	depth := 0
	ctx.SetExecutionHooks(func() { depth++; events = append(events, "enter") }, func() {
		depth--
		if depth < 0 {
			t.Error("unbalanced leave")
		}
		events = append(events, "leave")
	})
	i := hooksInterp(t, ctx)
	mark := func(s string) { events = append(events, s) }
	runFunc(t, i, "Nested", func(f func()) { f() }, mark)
	want := []string{"enter", "enter", "inner", "leave", "outer", "leave"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v; want %v", events, want)
	}
	if depth != 0 {
		t.Fatal("nested invocation did not leave")
	}

	events = nil
	if _, err := i.RunFunc("Panic", mark); err == nil {
		t.Fatal("missing panic error")
	}
	want = []string{"enter", "defer", "leave"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("panic events = %v; want %v", events, want)
	}

	events = nil
	runFunc(t, i, "Recover", func(f func()) { f() }, mark)
	if depth != 0 || events[len(events)-1] != "leave" {
		t.Fatalf("recover events = %v, depth = %d", events, depth)
	}
	if !reflect.DeepEqual(events[len(events)-2:], []string{"outer", "leave"}) {
		t.Fatal("outer invocation did not continue after recovery")
	}
}

func TestExecutionHooksInterpretedGo(t *testing.T) {
	entered, left := make(chan uint64, 4), make(chan uint64, 4)
	ctx := NewContext(SupportMultipleInterp | EnableCachedReg)
	ctx.SetExecutionHooks(func() { entered <- gid.Get() }, func() { left <- gid.Get() })
	i := hooksInterp(t, ctx)
	done := make(chan struct{})
	runFunc(t, i, "Spawn", func() { close(done) })
	<-done
	ids := map[uint64]bool{<-entered: true, <-entered: true}
	if len(ids) != 2 {
		t.Fatal("parent and child did not enter independently")
	}
	for range 2 {
		id := <-left
		if !ids[id] {
			t.Fatalf("unexpected leave for goroutine %d", id)
		}
		delete(ids, id)
	}
}

func TestExecutionHooksREPL(t *testing.T) {
	ctx := NewContext(0)
	depth, entries := 0, 0
	ctx.SetExecutionHooks(func() { depth++; entries++ }, func() { depth-- })
	r := NewRepl(ctx)
	for _, source := range []string{"n := 1", "n++", "n"} {
		before := entries
		if _, _, err := r.Eval(source); err != nil {
			t.Fatal(err)
		}
		if depth != 0 || entries == before {
			t.Fatalf("%q: depth=%d, entries=%d", source, depth, entries)
		}
	}
}
