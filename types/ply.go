package types

import (
	"go/ast"
	"go/constant"
	"go/token"
)

// A plyId is the id of a ply function or method.
type plyId int

const (
	// funcs
	_Max plyId = iota
	_Merge
	_Min
	_Not
	_Zip
	// methods
	_All
	_Any
	_Contains
	_DropWhile
	_Filter
	_Fold
	_Morph
	_Reverse
	_Sort
	_TakeWhile
	_ToMap
	_ToSet
)

var predeclaredPlyFuncs = [...]struct {
	name     string
	nargs    int
	variadic bool
	kind     exprKind
}{
	_Max:   {"max", 2, false, expression},
	_Merge: {"merge", 2, true, expression},
	_Min:   {"min", 2, false, expression},
	_Not:   {"not", 1, false, expression},
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
	_DropWhile: {"dropWhile", 1, false},
	_Filter:    {"filter", 1, false},
	_Fold:      {"fold", 1, true}, // 1 optional argument
	_Morph:     {"morph", 1, false},
	_Reverse:   {"reverse", 0, false},
	_Sort:      {"sort", 0, true}, // 1 optional argument
	_TakeWhile: {"takeWhile", 1, false},
	_ToMap:     {"toMap", 1, false},
	_ToSet:     {"toSet", 0, false},
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

	case _Max, _Min:
		// max(x, y T) T
		// min(x, y T) T
		var y operand
		arg(&y, 1)
		if y.mode == invalid {
			return
		}

		// convert or check untyped arguments
		switch xu, yu := isUntyped(x.typ), isUntyped(y.typ); {
		case !xu && !yu:
			// x and y are typed => nothing to do
		case xu && !yu:
			// only x is untyped => convert to type of y
			check.convertUntyped(x, y.typ)
		case !xu && yu:
			// only y is untyped => convert to type of x
			check.convertUntyped(&y, x.typ)
		case xu && yu:
			// x and y are untyped => check for invalid shift
			if x.mode != y.mode && (x.mode == constant_ || y.mode == constant_) {
				// if x xor y is not constant (possible because it contains a
				// shift that is yet untyped), convert both of them to float64
				// (this will result in an error because shifts of floats are
				// not permitted)
				check.convertUntyped(x, Typ[Float64])
				check.convertUntyped(&y, Typ[Float64])
			}
		}
		if x.mode == invalid || y.mode == invalid {
			return
		}

		// the argument types must be ordered
		if !isOrdered(x.typ) {
			check.invalidArg(x.pos(), "%s is not orderable", x.typ)
			return
		}

		// the argument types must be identical
		if !Identical(x.typ, y.typ) {
			check.invalidArg(x.pos(), "mismatched types %s and %s", x.typ, y.typ)
			return
		}

		// if both arguments are constants, the result is a constant
		if x.mode == constant_ && y.mode == constant_ {
			ylarger := constant.Compare(y.val, token.GTR, x.val)
			if ylarger && id == _Max || !ylarger && id == _Min {
				x.val = y.val
			}
		} else {
			x.mode = value
		}

	case _Not:
		// not(f func(...) bool) func(...) bool

		// f must be a function with a single boolean return value
		fn, ok := x.typ.Underlying().(*Signature)
		if !ok || fn.Results().Len() != 1 || !Identical(fn.Results().At(0).Type(), Typ[Bool]) {
			check.invalidArg(x.pos(), "cannot use %s as func(...) bool value in argument to not", x)
			return
		}
		x.mode = value

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

		fn, ok := x.typ.Underlying().(*Signature)
		if !ok || fn.Results().Len() != 1 || !Identical(fn, makeSig(fn.Results().At(0).Type(), T, U)) {
			check.invalidArg(x.pos(), "cannot use %s as func(%s, %s) T value in argument to zip", x, T, U)
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
	case _Contains:
		// NOTE: contains isn't all that special; we just want to give the
		// user a nice message if they use a non-comparable type. If we tried
		// to handle this in lookupPlyMethod, they'd just see "foo has no
		// method contains".

		switch recv := recv.Underlying().(type) {
		case *Slice:
			// ([]T).contains(T) bool
			T := recv.Elem()
			check.assignment(x, T, check.sprintf("argument to contains"))
			if x.mode == invalid {
				return
			}
			// T must be comparable or nil-able; if the latter, x must be nil
			if !Comparable(T) && !hasNil(T) {
				check.errorf(call.Pos(), "contains is only valid for comparable types (%s does not support ==)", T)
				return
			} else if hasNil(T) && !x.isNil() {
				check.invalidArg(x.pos(), "%s can only be compared to nil", T)
				return
			}

		case *Map:
			// (map[T]U).contains(T) bool
			T := recv.Elem()
			check.assignment(x, T, check.sprintf("argument to contains"))
			if x.mode == invalid {
				return
			}
		default:
			unreachable()
		}

		x.mode = value
		x.typ = Typ[Bool]
		if check.Types != nil {
			// TODO: record here?
		}

	case _Fold:
		// ([]T).fold(func(U, T) U) U
		// ([]T).fold(func(U, T) U, U) U
		if nargs > 2 {
			check.errorf(call.Pos(), "fold expects 1 or 2 arguments; got %v", nargs)
			return
		}
		T := recv.Underlying().(*Slice).Elem() // enforced by lookupPlyMethod
		fn, ok := x.typ.Underlying().(*Signature)
		if !ok || fn.Params().Len() != 2 || fn.Results().Len() != 1 {
			check.invalidArg(x.pos(), "cannot use %s as func(T, %s) T value in argument to fold", x, T)
			return
		}
		U := fn.Results().At(0).Type()
		if !Identical(fn.Params().At(0).Type(), U) || !Identical(fn.Params().At(1).Type(), T) || !Identical(fn.Results().At(0).Type(), U) {
			check.invalidArg(x.pos(), "cannot use %s as func(%s, %s) %s value in argument to fold", x, U, T, U)
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
				check.invalidArg(y.pos(), "cannot use %s as initial %s value of fold func(%s, %s) %s", &y, U, U, T, U)
				return
			}
		} else {
			// if no initial value is provided, then T and U must be identical
			if !Identical(T, U) {
				check.invalidArg(x.pos(), "cannot use %s as func(%s, %s) %s value in argument to fold", x, T, T, T)
				return
			}
		}

		x.mode = value
		x.typ = U
		if check.Types != nil {
			// TODO: record here?
		}

	case _Morph:
		switch recv := recv.Underlying().(type) {
		case *Slice:
			// ([]T).morph(func(T) U) []U
			T := recv.Elem()
			fn, ok := x.typ.Underlying().(*Signature)
			if !ok || fn.Params().Len() != 1 || fn.Results().Len() != 1 || !Identical(fn.Params().At(0).Type(), T) {
				check.invalidArg(x.pos(), "cannot use %s as func(%s) T value in argument to morph", x, T)
				return
			}

			x.mode = value
			x.typ = NewSlice(fn.Results().At(0).Type())
			if check.Types != nil {
				// TODO: record here?
			}

		case *Map:
			// (map[T]U).morph(func(T, U) (V, W) map[V]W
			T, U := recv.Key(), recv.Elem()
			fn, ok := x.typ.Underlying().(*Signature)
			if !ok || fn.Params().Len() != 2 || fn.Results().Len() != 2 || !Identical(fn.Params().At(0).Type(), T) || !Identical(fn.Params().At(1).Type(), U) {
				check.invalidArg(x.pos(), "cannot use %s as func(%s, %s) (T, U) value in argument to morph", x, T, U)
				return
			}
			V := fn.Results().At(0).Type()
			W := fn.Results().At(1).Type()
			// V must be a valid map key type
			if !Comparable(V) {
				check.invalidArg(x.pos(), "cannot morph map key type %s to %s: %s is not a comparable type", T, V, V)
				return
			}

			x.mode = value
			x.typ = NewMap(V, W)
			if check.Types != nil {
				// TODO: record here?
			}

		default:
			unreachable()
		}

	case _Sort:
		// ([]T).sort() []T
		// ([]T).sort(func(T, T) bool) []T
		if nargs > 1 {
			check.errorf(call.Pos(), "sort expects 0 or 1 argument; got %v", nargs)
			return
		}
		T := recv.Underlying().(*Slice).Elem() // enforced by lookupPlyMethod

		// sortfn is optional
		if nargs == 1 {
			arg(x, 0)
			if x.mode == invalid {
				return
			}
			if !Identical(x.typ, makeSig(Typ[Bool], T, T)) {
				check.invalidArg(x.pos(), "cannot use %s as func(%s, %s) bool value in argument to sort", x, T, T)
				return
			}
		}
		x.mode = value
		x.typ = recv
		if check.Types != nil {
			// TODO: record here?
		}

	case _ToMap:
		// ([]T).toMap(func(T) U) map[T]U
		T := recv.Underlying().(*Slice).Elem() // enforced by lookupPlyMethod
		fn, ok := x.typ.Underlying().(*Signature)
		if !ok || fn.Params().Len() != 1 || fn.Results().Len() != 1 || !Identical(fn.Params().At(0).Type(), T) {
			check.invalidArg(x.pos(), "cannot use %s as func(%s) T value in argument to toMap", x, T)
			return
		}

		x.mode = value
		x.typ = NewMap(fn.Params().At(0).Type(), fn.Results().At(0).Type())
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
	type method struct {
		args    []Type
		ret     Type
		special bool
	}
	var methods map[string]method
	switch t := T.Underlying().(type) {
	case *Slice:
		side := makeSig(nil, t.Elem())       // func(T)
		pred := makeSig(Typ[Bool], t.Elem()) // func(T) bool
		empty := NewStruct(nil, nil)         // struct{}
		methods = map[string]method{
			"all":       {[]Type{pred}, Typ[Bool], false},      // ([]T).all(func(T) bool) bool
			"any":       {[]Type{pred}, Typ[Bool], false},      // ([]T).any(func(T) bool) bool
			"drop":      {[]Type{Typ[Int]}, T, false},          // ([]T).drop(int) []T
			"dropWhile": {[]Type{pred}, T, false},              // ([]T).dropWhile(func(T) bool) []T
			"filter":    {[]Type{pred}, T, false},              // ([]T).filter(func(T) bool) []T
			"foreach":   {[]Type{side}, nil, false},            // ([]T).foreach(func(T))
			"reverse":   {nil, T, false},                       // ([]T).reverse() []T
			"take":      {[]Type{Typ[Int]}, T, false},          // ([]T).take(int) []T
			"takeWhile": {[]Type{pred}, T, false},              // ([]T).takeWhile(func(T) bool) []T
			"tee":       {[]Type{side}, T, false},              // ([]T).tee(func(T)) []T
			"toSet":     {nil, NewMap(t.Elem(), empty), false}, // ([]T).toSet() map[T]struct{}
			"uniq":      {nil, T, false},                       // ([]T).uniq() []T

			// special methods
			"contains": {nil, nil, true}, // ([]T).contains(T) bool
			"fold":     {nil, nil, true}, // ([]T).fold(func(U, T) U, U) U
			"morph":    {nil, nil, true}, // ([]T).morph(func(T) U) []U
			"sort":     {nil, nil, true}, // ([]T).sort(func(T, T) bool) []T
			"toMap":    {nil, nil, true}, // ([]T).toMap(func(T) U) map[T]U
		}

	case *Map:
		pred := makeSig(Typ[Bool], t.Key(), t.Elem()) // func(T, U) bool
		methods = map[string]method{
			"elems":  {nil, NewSlice(t.Elem()), false}, // (map[T]U).elems() []U
			"filter": {[]Type{pred}, T, false},         // (map[T]U].filter(func(T, U) bool) map[T]U
			"keys":   {nil, NewSlice(t.Key()), false},  // (map[T]U).keys() []T

			// special methods
			"contains": {nil, nil, true}, // (map[T]U).contains(T) bool
			"morph":    {nil, nil, true}, // (map[T]U).morph(func(T, U) (V, W)) map[V]W
		}
	}

	if m, ok := methods[name]; ok {
		if m.special {
			return makeSpecialPlyMethod(name, T)
		}
		return makePlyMethod(name, m.ret, m.args...)
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
