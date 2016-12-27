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
	_Zip
	// methods
	_All
	_Any
	_Contains
	_Filter
	_Morph
	_Reduce
	_Reverse
	_TakeWhile
)

var predeclaredPlyFuncs = [...]struct {
	name     string
	nargs    int
	variadic bool
	kind     exprKind
}{
	_Merge: {"merge", 2, true, expression},
	_Zip:   {"zip", 3, false, expression},
}

var predeclaredPlyMethods = [...]struct {
	name     string
	nargs    int
	variadic bool
}{
	_All:       {"all", 1, false},
	_Any:       {"any", 1, false},
	_Contains:  {"contains", 1, false},
	_Filter:    {"filter", 1, false},
	_Morph:     {"morph", 1, false},
	_Reduce:    {"reduce", 1, true}, // 1 optional argument
	_Reverse:   {"reverse", 0, false},
	_TakeWhile: {"takeWhile", 1, false},
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
	// NOTE: we can always use the standard getter unless one of the
	// function's arguments is a type expression, as in make/new.
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
		// merge(x map[T]U, y ...map[T]U) map[T]U

		// all args must have the same type, or nil
		var T, U Type
		for i := range call.Args {
			var y operand
			arg(&y, i)
			if y.mode == invalid {
				return
			}
			if y.typ == Typ[UntypedNil] {
				// untyped; assume same as others
				continue
			}
			// get type
			t, ok := y.typ.Underlying().(*Map)
			if !ok {
				check.invalidArg(y.pos(), "merge expected map type; found %s", &y)
				return
			}
			if T == nil {
				// don't know T or U; set them
				T, U = t.key, t.elem
				x.typ = y.typ
			} else {
				// T and U are known
				if !Identical(T, t.key) || !Identical(U, t.elem) {
					check.invalidArg(y.pos(), "merge expected all args to be of type map[%s]%s; found %s", T, U, t)
					return
				}
			}
		}

		x.mode = value
		if check.Types != nil {
			//check.recordPlyType(call.Fun, makeSig(x.typ, x.typ, x.typ))
		}

	case _Zip:
		// zip(func(x T, y U) V, xs []T, ys []U) []V

		// y and z must be slices
		var y operand
		arg(&y, 1)
		if y.mode == invalid {
			return
		}
		var z operand
		arg(&z, 2)
		if z.mode == invalid {
			return
		}

		ts, ok := y.typ.Underlying().(*Slice)
		if !ok {
			check.invalidArg(y.pos(), "zip expects slice arguments; found %s", &y)
			return
		}
		us, ok := z.typ.Underlying().(*Slice)
		if !ok {
			check.invalidArg(z.pos(), "zip expects slice arguments; found %s", &z)
			return
		}
		// derive T and U from slices rather than function; user is more
		// likely to have passed the wrong function than the wrong slice
		T := ts.Elem()
		U := us.Elem()

		fn := x.typ.Underlying().(*Signature)
		if fn == nil || fn.Results().Len() != 1 || !Identical(fn, makeSig(fn.Results().At(0).Type(), T, U)) {
			check.errorf(x.pos(), "cannot use %s as func(%s, %s) T value in argument to reduce", x, T, U)
			return
		}
		x.mode = value
		x.typ = NewSlice(fn.Results().At(0).Type())
		if check.Types != nil {
			//check.recordPlyType(call.Fun, makeSig(x.typ, x.typ, x.typ))
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
				if y.mode == invalid {
					return
				}
			} else if !Identical(y.typ, U) {
				check.errorf(y.pos(), "cannot use %s as initial %s value of reducer func(%s, %s) %s", &y, U, U, T, U)
				return
			}
		} else {
			// if no initial value is provided, then T and U must be identical
			if !Identical(T, U) {
				check.errorf(x.pos(), "cannot use %s as func(%s, %s) %s value in argument to reduce", x, T, T, T)
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
// methods are special; specifically, their signature depends on their
// arguments. In this case, a special sentinel signature is returned instead
// of the typical full signature. These calls will be handled later by
// plySpecialMethod.
func lookupPlyMethod(T Type, name string) (obj Object, index []int, indirect bool) {
	switch name {
	case "all", "any":
		// T must be a slice
		if s, ok := T.Underlying().(*Slice); ok {
			// func(T) bool
			pred := makeSig(Typ[Bool], s.Elem())
			// ([]T).any(func(T) bool) bool
			return makePlyMethod(name, Typ[Bool], pred)
		}

	case "contains":
		switch t := T.Underlying().(type) {
		case *Slice:
			// T must be comparable
			if Comparable(t.Elem()) {
				// ([]T).contains(T) bool
				return makePlyMethod(name, Typ[Bool], t.Elem())
			}
		case *Map:
			// (map[T]U).contains(T) bool
			return makePlyMethod(name, Typ[Bool], t.Elem())
		}

	case "filter", "takeWhile":
		// T must be a slice
		if s, ok := T.Underlying().(*Slice); ok {
			// func(T) bool
			pred := makeSig(Typ[Bool], s.Elem())
			// ([]T).filter(func(T) bool) []T
			return makePlyMethod(name, s, pred)
		}

	case "reverse":
		// ([]T).reverse() []T
		return makePlyMethod(name, T)

	// special methods
	case "morph", "reduce":
		return makeSpecialPlyMethod(name, T)
	}

	// not a ply method
	return nil, nil, false
}

func makePlyMethod(name string, res Type, args ...Type) (*Func, []int, bool) {
	f := NewFunc(token.NoPos, nil, name, makeSig(res, args...))
	var i int
	for i = range predeclaredPlyMethods {
		if predeclaredPlyMethods[i].name == name {
			break
		}
	}
	return f, []int{i}, false
}

func makeSpecialPlyMethod(name string, typ Type) (*Func, []int, bool) {
	var id int
	for id = range predeclaredPlyMethods {
		if predeclaredPlyMethods[id].name == name {
			break
		}
	}
	f := NewFunc(token.NoPos, nil, predeclaredPlyMethods[id].name, &Signature{
		// HACK: hide the recv type in the first param. This is because
		// check.selector will later set recv = nil. (why?)
		params: NewTuple(NewVar(token.NoPos, nil, "", typ)),
		ply:    plyId(id),
	})
	return f, []int{id}, false
}
