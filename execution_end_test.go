package ixgo

import (
	"sync"
	"testing"
	"time"

	"github.com/visualfc/gid"
)

const executionEndSource = `package main
func Make(f func()) func() { return func() { f() } }
func Reflect(f func(int) int) int { return f(7) }
func Nested(invoke func(func()), check func()) {
	invoke(func() { check() })
	check()
}
func Panic(check func()) { defer check(); panic("cancelled") }
func Recover(invoke func(func()), check func()) {
	invoke(func() { defer func() { recover() }(); panic("recovered") })
	check()
}
func Spawn(f func()) { go func() { f() }() }
func main() {}
`

func executionEndInterp(t *testing.T, ended func()) *Interp {
	t.Helper()
	ctx := NewContext(SupportMultipleInterp | EnableCachedReg)
	ctx.SetLeastCallForEnablePool(0)
	ctx.SetExecutionEnd(ended)
	i, err := ctx.LoadInterp("execution.go", executionEndSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(i.UnsafeRelease)
	return i
}

func TestExecutionEndCallbacks(t *testing.T) {
	for _, name := range []string{"Direct", "Reflect"} {
		t.Run(name, func(t *testing.T) {
			ended := make(chan uint64, 8)
			i := executionEndInterp(t, func() { ended <- gid.Get() })
			entered := make(chan uint64, 2)
			release := make(chan struct{})
			wait := func() { entered <- gid.Get(); <-release }
			var invoke func()
			if name == "Direct" {
				invoke = runFunc(t, i, "Make", wait).(func())
				<-ended // Make itself completed before invoking its retained closure.
			} else {
				fn, ok := i.GetFunc("Reflect")
				if !ok {
					t.Fatal("missing function")
				}
				invoke = func() {
					got := fn.(func(func(int) int) int)(func(n int) int { wait(); return n + 1 })
					if got != 8 {
						t.Errorf("result = %d", got)
					}
				}
			}
			var wg sync.WaitGroup
			for range 2 {
				wg.Go(invoke)
			}
			ids := map[uint64]bool{<-entered: true, <-entered: true}
			if len(ids) != 2 {
				t.Fatal("callbacks did not use different goroutines")
			}
			select {
			case <-ended:
				t.Fatal("waiting callback reported completion")
			default:
			}
			close(release)
			wg.Wait()
			for range 2 {
				select {
				case id := <-ended:
					if !ids[id] {
						t.Fatalf("unexpected completion: %d", id)
					}
					delete(ids, id)
				case <-time.After(5 * time.Second):
					t.Fatal("missing completion")
				}
			}
			select {
			case <-ended:
				t.Fatal("duplicate completion")
			default:
			}
		})
	}
}

func TestExecutionEndNestedAndPanic(t *testing.T) {
	ended := 0
	owner := gid.Get()
	i := executionEndInterp(t, func() {
		if gid.Get() != owner {
			t.Error("completion ran on a different goroutine")
		}
		ended++
	})
	for _, name := range []string{"Nested", "Recover"} {
		before := ended
		check := func() {
			if ended != before {
				t.Error("nested invocation ended the outer execution")
			}
		}
		runFunc(t, i, name, func(f func()) { f() }, check)
		if ended != before+1 {
			t.Fatalf("%s: completion count = %d", name, ended)
		}
	}
	before := ended
	_, err := i.RunFunc("Panic", func() {
		if ended != before {
			t.Error("completion preceded interpreted defer")
		}
	})
	if err == nil || ended != before+1 {
		t.Fatalf("panic: err=%v, completions=%d", err, ended)
	}
	runFunc(t, i, "main")
	if ended != before+2 {
		t.Fatal("subsequent invocation did not finish independently")
	}
}

func TestExecutionEndInterpretedGo(t *testing.T) {
	ended := make(chan uint64, 4)
	i := executionEndInterp(t, func() { ended <- gid.Get() })

	done := make(chan struct{})
	runFunc(t, i, "Spawn", func() { close(done) })
	<-done
	parent, child := <-ended, <-ended
	if parent == child {
		t.Fatal("parent and child shared execution identity")
	}
}
