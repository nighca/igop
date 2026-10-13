package ixgo

// callSpecializerShape identifies a call before its instruction is built.
// It is intentionally package-private: this is an internal experiment while
// the build-time extension contract is being evaluated.
type callSpecializerShape struct {
	nargs, nres int
}

type callSpecializerFactory func(*Interp, *function, []register, []register) func(*frame)

var callSpecializers = map[callSpecializerShape]callSpecializerFactory{
	{1, 2}: makeOneArgTwoResultCall,
	{2, 2}: makeTwoArgTwoResultCall,
}

func registerCallSpecializer(shape callSpecializerShape, factory callSpecializerFactory) {
	if factory == nil {
		delete(callSpecializers, shape)
		return
	}
	callSpecializers[shape] = factory
}

func makeOneArgTwoResultCall(interp *Interp, fn *function, args, dests []register) func(*frame) {
	arg, result0, result1 := args[0], dests[0], dests[1]
	return func(fr *frame) {
		child := fn.allocFrameFromCaller(fr)
		child.stack[fn.nres] = fr.reg(arg)
		for child.ipc != -1 {
			instr := child.pfn.Instrs[child.ipc]
			child.ipc++
			instr(child)
		}
		fr.setReg(result0, child.stack[0])
		fr.setReg(result1, child.stack[1])
		fn.deleteFrameToCaller(fr, child)
	}
}

func makeTwoArgTwoResultCall(interp *Interp, fn *function, args, dests []register) func(*frame) {
	arg0, arg1, result0, result1 := args[0], args[1], dests[0], dests[1]
	return func(fr *frame) {
		child := fn.allocFrameFromCaller(fr)
		child.stack[fn.nres] = fr.reg(arg0)
		child.stack[fn.nres+1] = fr.reg(arg1)
		for child.ipc != -1 {
			instr := child.pfn.Instrs[child.ipc]
			child.ipc++
			instr(child)
		}
		fr.setReg(result0, child.stack[0])
		fr.setReg(result1, child.stack[1])
		fn.deleteFrameToCaller(fr, child)
	}
}
