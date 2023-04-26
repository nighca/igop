/*
 * Copyright (c) 2022 The GoPlus Authors (goplus.org). All rights reserved.
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

package igop

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/visualfc/funcval"
	"golang.org/x/tools/go/ssa"
)

func init() {
	RegisterExternal("os.Exit", func(fr *frame, code int) {
		interp := fr.interp
		if atomic.LoadInt32(&interp.goexited) == 1 {
			//os.Exit(code)
			interp.chexit <- code
		} else {
			panic(exitPanic(code))
		}
	})
	RegisterExternal("runtime.Goexit", func(fr *frame) {
		interp := fr.interp
		// main goroutine use panic
		if goroutineID() == interp.mainid {
			atomic.StoreInt32(&interp.goexited, 1)
			panic(goexitPanic(0))
		} else {
			runtime.Goexit()
		}
	})
	RegisterExternal("runtime.Caller", runtimeCaller)
	RegisterExternal("runtime.FuncForPC", runtimeFuncForPC)
	RegisterExternal("runtime.Callers", runtimeCallers)
	RegisterExternal("(*runtime.Frames).Next", runtimeFramesNext)
	RegisterExternal("(*runtime.Func).FileLine", runtimeFuncFileLine)
	RegisterExternal("runtime.Stack", runtimeStack)
	RegisterExternal("runtime/debug.Stack", debugStack)
	RegisterExternal("runtime/debug.PrintStack", debugPrintStack)

	if funcval.IsSupport {
		RegisterExternal("(reflect.Value).Pointer", func(v reflect.Value) uintptr {
			if v.Kind() == reflect.Func {
				if fv, n := funcval.Get(v.Interface()); n == 1 {
					pc := (*makeFuncVal)(unsafe.Pointer(fv)).pfn.base
					return uintptr(pc)
				}
			}
			return v.Pointer()
		})
	}
}

func runtimeFuncFileLine(fr *frame, f *runtime.Func, pc uintptr) (file string, line int) {
	entry := f.Entry()
	if isInlineFunc(f) && pc > entry {
		interp := fr.interp
		if pfn := findFuncByEntry(interp, int(entry)); pfn != nil {
			// pc-1 : fn.instr.pos
			pos := pfn.PosForPC(int(pc - entry - 1))
			if !pos.IsValid() {
				return "?", 0
			}
			fpos := interp.ctx.FileSet.Position(pos)
			if fpos.Filename == "" {
				return "??", fpos.Line
			}
			file, line = filepath.ToSlash(fpos.Filename), fpos.Line
			return
		}
	}
	return f.FileLine(pc)
}

func runtimeCaller(fr *frame, skip int) (pc uintptr, file string, line int, ok bool) {
	if skip < 0 {
		return runtime.Caller(skip)
	}
	rpc := make([]uintptr, 1)
	n := runtimeCallers(fr, skip+1, rpc[:])
	if n < 1 {
		return
	}
	frame, _ := runtimeFramesNext(fr, runtime.CallersFrames(rpc))
	return frame.PC, frame.File, frame.Line, frame.PC != 0
}

//go:linkname runtimePanic runtime.gopanic
func runtimePanic(e interface{})

// runtime.Callers => runtime.CallersFrames
// 0 = runtime.Caller
// 1 = frame
// 2 = frame.caller
// ...
func runtimeCallers(fr *frame, skip int, pc []uintptr) int {
	if len(pc) == 0 {
		return 0
	}
	pcs := make([]uintptr, 1)

	// runtime.Caller itself
	runtime.Callers(0, pcs)
	pcs[0] -= 1

	caller := fr
	for caller.valid() {
		link := caller._panic
		for link != nil {
			pcs = append(pcs, uintptr(reflect.ValueOf(runtimePanic).Pointer()))
			pcs = append(pcs, link.pcs...)
			link = link.link
		}
		pcs = append(pcs, caller.pc())
		caller = caller.caller
	}
	var rpc []uintptr
	for _, pc := range pcs {
		// skip wrapper method func
		if fn := findFuncByPC(fr.interp, int(pc)); fn != nil && isWrapperFuncName(fn.Fn.String()) {
			continue
		}
		rpc = append(rpc, pc)
	}
	if skip < 0 {
		skip = 0
	} else if skip > len(rpc)-1 {
		return 0
	}
	return copy(pc, rpc[skip:])
}

func runtimeFuncForPC(fr *frame, pc uintptr) *runtime.Func {
	if pfn := findFuncByPC(fr.interp, int(pc)); pfn != nil {
		return runtimeFunc(pfn)
	}
	return runtime.FuncForPC(pc)
}

func findFuncByPC(interp *Interp, pc int) *function {
	if pc == 0 {
		return nil
	}
	for _, pfn := range interp.funcs {
		if pc >= pfn.base && pc <= pfn.base+len(pfn.ssaInstrs) {
			return pfn
		}
	}
	return nil
}

func findFuncByEntry(interp *Interp, entry int) *function {
	for _, pfn := range interp.funcs {
		if entry == pfn.base {
			return pfn
		}
	}
	return nil
}

func isWrapperFuncName(name string) bool {
	return strings.HasSuffix(name, "$bound") || strings.HasSuffix(name, "$thunk")
}

func runtimeFunc(pfn *function) *runtime.Func {
	fn := pfn.Fn
	f := inlineFunc(uintptr(pfn.base))
	var autogen bool
	f.name, autogen = fixedFuncName(pfn.Fn)
	if autogen {
		f.file = "<autogenerated>"
		f.line = 1
	} else {
		if pos := fn.Pos(); pos != token.NoPos {
			fpos := pfn.Interp.ctx.FileSet.Position(pos)
			f.file = filepath.ToSlash(fpos.Filename)
			f.line = fpos.Line
		}
	}
	return (*runtime.Func)(unsafe.Pointer(f))
}

/*
	type Frames struct {
		// callers is a slice of PCs that have not yet been expanded to frames.
		callers []uintptr

		// frames is a slice of Frames that have yet to be returned.
		frames     []Frame
		frameStore [2]Frame
	}
*/
type runtimeFrames struct {
	callers    []uintptr
	frames     []runtime.Frame
	frameStore [2]runtime.Frame
}

func runtimeFramesNext(fr *frame, frames *runtime.Frames) (frame runtime.Frame, more bool) {
	ci := (*runtimeFrames)(unsafe.Pointer(frames))
	for len(ci.frames) < 2 {
		// Find the next frame.
		// We need to look for 2 frames so we know what
		// to return for the "more" result.
		if len(ci.callers) == 0 {
			break
		}
		pc := ci.callers[0]
		ci.callers = ci.callers[1:]
		f := runtimeFuncForPC(fr, pc)
		if f == nil {
			continue
		}
		ci.frames = append(ci.frames, runtime.Frame{
			PC:       pc,
			Func:     f,
			Function: f.Name(),
			Entry:    f.Entry(),
			// Note: File,Line set below
		})
	}

	// Pop one frame from the frame list. Keep the rest.
	// Avoid allocation in the common case, which is 1 or 2 frames.
	switch len(ci.frames) {
	case 0: // In the rare case when there are no frames at all, we return Frame{}.
		return
	case 1:
		frame = ci.frames[0]
		ci.frames = ci.frameStore[:0]
	case 2:
		frame = ci.frames[0]
		ci.frameStore[0] = ci.frames[1]
		ci.frames = ci.frameStore[:1]
	default:
		frame = ci.frames[0]
		ci.frames = ci.frames[1:]
	}
	more = len(ci.frames) > 0
	if frame.Func != nil {
		frame.File, frame.Line = runtimeFuncFileLine(fr, frame.Func, frame.PC)
	}
	return
}

func extractGoroutine() (string, bool) {
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	s := string(buf[:n])
	if strings.HasPrefix(s, "goroutine") {
		if pos := strings.Index(s, "\n"); pos != -1 {
			return s[:pos+1], true
		}
	}
	return "", false
}

func runtimeStack(fr *frame, buf []byte, all bool) int {
	if len(buf) == 0 {
		return 0
	}
	var w bytes.Buffer
	if s, ok := extractGoroutine(); ok {
		w.WriteString(s)
	} else {
		w.WriteString("goroutine 1 [running]:\n")
	}
	rpc := make([]uintptr, 64)
	n := runtimeCallers(fr, 1, rpc)
	fs := runtime.CallersFrames(rpc[:n])
	for {
		f, more := runtimeFramesNext(fr, fs)
		if f.Function == "runtime.gopanic" {
			w.WriteString("panic()")
		} else {
			w.WriteString(f.Function + "()")
		}
		w.WriteByte('\n')
		w.WriteByte('\t')
		w.WriteString(fmt.Sprintf("%v:%v", f.File, f.Line))
		if f.PC != f.Entry {
			w.WriteString(fmt.Sprintf(" +0x%x", f.PC-f.Entry))
		}
		w.WriteByte('\n')
		if !more {
			break
		}
	}
	return copy(buf, w.Bytes())
}

// PrintStack prints to standard error the stack trace returned by runtime.Stack.
func debugPrintStack(fr *frame) {
	os.Stderr.Write(debugStack(fr))
}

// Stack returns a formatted stack trace of the goroutine that calls it.
// It calls runtime.Stack with a large enough buffer to capture the entire trace.
func debugStack(fr *frame) []byte {
	buf := make([]byte, 1024)
	for {
		n := runtimeStack(fr, buf, false)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

var (
	reFuncName = regexp.MustCompile("\\$(\\d+)")
)

func fixedFuncName(fn *ssa.Function) (name string, autogen bool) {
	name = fn.String()
	name = reFuncName.ReplaceAllString(name, ".func$1")
	if strings.HasPrefix(name, "(") {
		if pos := strings.LastIndex(name, ")"); pos != -1 {
			line := name[1:pos]
			if strings.HasPrefix(line, "*") {
				if dot := strings.LastIndex(line, "."); dot != -1 {
					line = line[1:dot+1] + "(*" + line[dot+1:] + ")"
				}
			}
			name = line + name[pos+1:]
		}
	}
	if strings.HasSuffix(name, "$bound") {
		return name[:len(name)-6] + "-fm", bound_is_autogen
	} else if strings.HasSuffix(name, "$thunk") {
		name = name[:len(name)-6]
		if strings.HasPrefix(name, "struct{") {
			return name, true
		}
		if sig, ok := fn.Type().(*types.Signature); ok {
			if types.IsInterface(sig.Params().At(0).Type()) {
				return name, true
			}
		}
	}
	return name, false
}

func runtimeGC(fr *frame) {
	for fr.valid() {
		fr.gc()
		fr = fr.caller
	}
	runtime.GC()
}
