package types

import (
	"go/ast"
	"go/token"
)

// A plyId is the id of a ply function or method.
type plyId int

const (
	// funcs
	_Merge plyId = iota
	// methods
	_Filter
	_Morph
	_Reduce
)

var predeclaredPlyFuncs = [...]struct {
	name     string
	nargs    int
	variadic bool
	kind     exprKind
}{
	_Merge: {"merge", 2, false, expression},
}

var predeclaredPlyMethods = [...]struct {
	name     string
	nargs    int
	variadic bool
}{
	_Filter: {"filter", 1, false},
	_Morph:  {"morph", 1, false},
	_Reduce: {"reduce", 1, true}, // 1 optional argument
}

func defPredeclaredPlyFuncs() {
	for i := range predeclaredPlyFuncs {
		id := plyId(i)
		def(newPly(id))
	}
}

// ply type-checks a call to a ply builtin.
func (check *Checker) ply(x *operand, call *ast.CallExpr, id plyId) (_ bool) {
	bin := predeclaredPlyFuncs[id]

	// determine arguments
	var arg getter
	nargs := len(call.Args)
	switch id {
	default:
		// make argument getter
		arg, nargs, _ = unpack(func(x *operand, i int) { check.multiExpr(x, call.Args[i]) }, nargs, false)
		if arg == nil {
			return
		}
		// evaluate first argument, if present
		if nargs > 0 {
			arg(x, 0)
			if x.mode == invalid {
				return
			}
		}
	}

	// check argument count
	{
		msg := ""
		if nargs < bin.nargs {
			msg = "not enough"
		} else if !bin.variadic && nargs > bin.nargs {
			msg = "too many"
		}
		if msg != "" {
			check.invalidOp(call.Rparen, "%s arguments for %s (expected %d, found %d)", msg, call, bin.nargs, nargs)
			return
		}
	}

	switch id {
	case _Merge:
		// merge(x, y Map) int
		var dstKey, dstElem Type
		if t, _ := x.typ.Underlying().(*Map); t != nil {
			dstKey, dstElem = t.key, t.elem
		}
		var y operand
		arg(&y, 1)
		if y.mode == invalid {
			return
		}
		var srcKey, srcElem Type
		if t, _ := y.typ.Underlying().(*Map); t != nil {
			srcKey, srcElem = t.key, t.elem
		}

		if dstKey == nil || dstElem == nil || srcKey == nil || srcElem == nil {
			check.invalidArg(x.pos(), "merge expects map arguments; found %s and %s", x, &y)
			return
		}

		if !Identical(dstKey, srcKey) {
			check.invalidArg(x.pos(), "arguments to merge %s and %s have different key types %s and %s", x, &y, dstKey, srcKey)
			return
		} else if !Identical(dstElem, srcElem) {
			check.invalidArg(x.pos(), "arguments to merge %s and %s have different element types %s and %s", x, &y, dstElem, srcElem)
			return
		}

		x.mode = value
		if check.Types != nil {
			check.recordPlyType(call.Fun, makeSig(x.typ, x.typ, y.typ))
		}

	default:
		unreachable()
	}

	return true
}

// ply type-checks a call to a special ply method. A ply method is special if
// its full signature depends on one or more arguments.
func (check *Checker) plySpecialMethod(x *operand, call *ast.CallExpr, recv Type, id plyId) (_ bool) {
	bin := predeclaredPlyMethods[id]

	// determine arguments
	var arg getter
	nargs := len(call.Args)
	switch id {
	default:
		// make argument getter
		arg, nargs, _ = unpack(func(x *operand, i int) { check.multiExpr(x, call.Args[i]) }, nargs, false)
		if arg == nil {
			return
		}
		// evaluate first argument, if present
		if nargs > 0 {
			arg(x, 0)
			if x.mode == invalid {
				return
			}
		}
	}

	// check argument count
	{
		msg := ""
		if nargs < bin.nargs {
			msg = "not enough"
		} else if !bin.variadic && nargs > bin.nargs {
			msg = "too many"
		}
		if msg != "" {
			check.invalidOp(call.Rparen, "%s arguments for %s (expected %d, found %d)", msg, call, bin.nargs, nargs)
			return
		}
	}

	switch id {
	case _Morph:
		// ([]T).morph(func(T) U) []U
		s := recv.Underlying().(*Slice) // enforced by lookupPlyMethod
		fn := x.typ.Underlying().(*Signature)
		if fn == nil || fn.Params().Len() != 1 || fn.Results().Len() != 1 || !Identical(fn.Params().At(0).Type(), s.Elem()) {
			check.errorf(x.pos(), "cannot use %s as func(%s) T value in argument to morph", x, s.Elem())
			return
		}

		x.mode = value
		x.typ = NewSlice(fn.Results().At(0).Type())
		if check.Types != nil {
			// TODO: record here?
		}

	case _Reduce:
		// ([]T).reduce(func(U, T) U) U
		// ([]T).reduce(func(U, T) U, U) U
		if nargs > 2 {
			check.errorf(call.Pos(), "reduce expects 1 or 2 arguments; got %v", nargs)
			return
		}
		fn := x.typ.Underlying().(*Signature)
		T := recv.Underlying().(*Slice).Elem() // enforced by lookupPlyMethod
		if fn == nil || fn.Params().Len() != 2 || fn.Results().Len() != 1 {
			check.errorf(x.pos(), "cannot use %s as func(T, %s) T value in argument to reduce", x, T)
			return
		}
		U := fn.Results().At(0).Type()
		if !Identical(fn.Params().At(0).Type(), U) || !Identical(fn.Params().At(1).Type(), T) || !Identical(fn.Results().At(0).Type(), U) {
			check.errorf(x.pos(), "cannot use %s as func(%s, %s) %s value in argument to reduce", x, U, T, U)
			return
		}

		// initial value is optional
		if nargs == 2 {
			var y operand
			arg(&y, 1)
			if y.mode == invalid {
				return
			}
			if isUntyped(y.typ) {
				// y may be untyped; convert to U
				check.convertUntyped(&y, U)
			} else if !Identical(y.typ, U) {
				check.errorf(y.pos(), "cannot use %s as initial %s value of reducer func(%s, %s) %s", &y, U, U, T, U)
				return
			}
		}

		x.mode = value
		x.typ = U
		if check.Types != nil {
			// TODO: record here?
		}

	default:
		unreachable()
	}

	return true
}

// lookupPlyMethod returns the ply method 'name' if it exists for T. Some ply
// methods are special; in this case, a special sentinel signature is returned
// instead of the typical full signature. These calls will be handled later by
// plySpecialMethod.
func lookupPlyMethod(T Type, name string) (obj Object, index []int, indirect bool) {
	switch name {
	case "filter":
		// T must be a slice
		s, ok := T.Underlying().(*Slice)
		if !ok {
			break
		}
		return makeFilter(s), []int{1}, false

	case "morph":
		// T must be a slice
		_, ok := T.Underlying().(*Slice)
		if !ok {
			break
		}
		return makePlyMethod(_Morph, T), []int{2}, false

	case "reduce":
		// T must be a slice
		_, ok := T.Underlying().(*Slice)
		if !ok {
			break
		}
		return makePlyMethod(_Reduce, T), []int{3}, false
	}
	// not a ply method
	return nil, nil, false
}

func makePlyMethod(id plyId, typ Type) *Func {
	return NewFunc(token.NoPos, nil, predeclaredPlyMethods[id].name, &Signature{
		// HACK: hide the recv type in the first param. This is because
		// check.selector will later set recv = nil. (why?)
		params: NewTuple(NewVar(token.NoPos, nil, "", typ)),
		ply:    id,
	})
}

func makeFilter(styp *Slice) *Func {
	predSig := &Signature{
		params:  NewTuple(NewVar(token.NoPos, nil, "", styp.Elem())),
		results: NewTuple(NewVar(token.NoPos, nil, "", Typ[Bool])),
	}
	return NewFunc(token.NoPos, nil, "filter", &Signature{
		recv:    NewVar(token.NoPos, nil, "", styp),                  // []T
		params:  NewTuple(NewVar(token.NoPos, nil, "pred", predSig)), // func(T) bool
		results: NewTuple(NewVar(token.NoPos, nil, "", styp)),        // []T
	})
}
