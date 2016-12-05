package types

import (
	"go/ast"
	"go/token"
)

// A plyId is the id of a ply function.
type plyId int

const (
	_Merge plyId = iota
)

var predeclaredPlyFuncs = [...]struct {
	name     string
	nargs    int
	variadic bool
	kind     exprKind
}{
	_Merge: {"merge", 2, false, expression},
}

func defPredeclaredPlyFuncs() {
	for i := range predeclaredPlyFuncs {
		id := plyId(i)
		def(newPly(id))
	}
}

// ply type-checks a call to a ply function or method.
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

func lookupPlyMethod(T Type, name string) (obj Object, index []int, indirect bool) {
	switch name {
	case "filter":
		// T must be a slice
		s, ok := T.Underlying().(*Slice)
		if !ok {
			break
		}
		return makeFilter(s), []int{1}, false
	}
	// not a ply method
	return nil, nil, false
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
