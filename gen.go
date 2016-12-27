package main

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/lukechampine/ply/types"
)

type rewriter func(*ast.CallExpr)

func rewriteFunc(name string) rewriter {
	return func(c *ast.CallExpr) {
		c.Fun.(*ast.Ident).Name = name
	}
}

func rewriteMethod(name string) rewriter {
	return func(c *ast.CallExpr) {
		fn := c.Fun.(*ast.SelectorExpr)
		fn.X = &ast.CallExpr{
			Fun:  ast.NewIdent(name),
			Args: []ast.Expr{fn.X},
		}
	}
}

func rewriteReassign(reassign ast.Expr) rewriter {
	return func(c *ast.CallExpr) {
		c.Args = append(c.Args, reassign)
	}
}

func rewriteMethodReassign(name string, reassign ast.Expr) rewriter {
	return func(c *ast.CallExpr) {
		rewriteMethod(name)(c)
		rewriteReassign(reassign)(c)
	}
}

func rewriteFuncReassign(name string, reassign ast.Expr) rewriter {
	return func(c *ast.CallExpr) {
		rewriteFunc(name)(c)
		rewriteReassign(reassign)(c)
	}
}

type genFunc func(*ast.Ident, []ast.Expr, ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string, rewriter)

type genMethod func(*ast.SelectorExpr, []ast.Expr, ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string, rewriter)

var funcGenerators = map[string]genFunc{
	"merge": mergeGen,
	"zip":   zipGen,
}

var methodGenerators = map[string]genMethod{
	"all":       allGen,
	"any":       anyGen,
	"contains":  containsGen,
	"filter":    filterGen,
	"morph":     morphGen,
	"reduce":    reduceGen,
	"reverse":   reverseGen,
	"takeWhile": takeWhileGen,
}

// some types may have "unfriendly" names, e.g. "chan int". Need to sanitize
// these before concatenating them into a new ident.
func safeIdent(s string) string {
	return strings.NewReplacer(
		// slices/arrays
		"[", "",
		"]", "",
		// pointers
		"*", "ptr",
		// channels
		"chan<-", "chan_in",
		"<-chan", "chan_out",
		" ", "_",
		// structs
		"{", "",
		"}", "",
		";", "",
		// imports
		".", "",
	).Replace(s)
}

const mergeTempl = `
func %[1]s(recv map[%[2]s]%[3]s, rest ...map[%[2]s]%[3]s) map[%[2]s]%[3]s {
	if len(rest) == 0 {
		return recv
	} else if recv == nil {
		recv = make(map[%[2]s]%[3]s, len(rest[0]))
	}
	for _, m := range rest {
		for k, v := range m {
			recv[k] = v
		}
	}
	return recv
}
`

func mergeGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// seek until we find a non-nil arg
	var key, elem string
	for _, arg := range args {
		if mt, ok := exprTypes[arg].Type.(*types.Map); ok {
			key, elem = mt.Key().String(), mt.Elem().String()
			break
		}
	}
	name = safeIdent("merge" + key + elem)
	code = fmt.Sprintf(mergeTempl, name, key, elem)
	r = rewriteFunc(name)
	return
}

const zipTempl = `
func %[1]s(fn func(a %[2]s, b %[3]s) %[4]s, a []%[2]s, b []%[3]s) []%[4]s {
	var zipped []%[4]s
	if len(a) < len(b) {
		zipped = make([]%[4]s, len(a))
	} else {
		zipped = make([]%[4]s, len(b))
	}
	for i := range zipped {
		zipped[i] = fn(a[i], b[i])
	}
	return zipped
}
`

const zipReassignTempl = `
func %[1]s(fn func(a %[2]s, b %[3]s) %[4]s, a []%[2]s, b []%[3]s, reassign []%[4]s) []%[4]s {
	var n int = len(a)
	if len(b) < len(a) {
		n = len(b)
	}
	var zipped []%[4]s
	if cap(reassign) >= n {
		zipped = reassign[:n]
	} else {
		zipped = make([]%[4]s, n)
	}
	for i := range zipped {
		zipped[i] = fn(a[i], b[i])
	}
	return zipped
}
`

func zipGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// determine arg types
	sig := exprTypes[args[0]].Type.(*types.Signature)
	T := sig.Params().At(0).Type().String()
	U := sig.Params().At(1).Type().String()
	V := sig.Results().At(0).Type().String()
	name = safeIdent("zip" + T + U + V)
	if reassign != nil {
		name += "reassign"
		code = fmt.Sprintf(zipReassignTempl, name, T, U, V)
		r = rewriteFuncReassign(name, reassign)
	} else {
		code = fmt.Sprintf(zipTempl, name, T, U, V)
		r = rewriteFunc(name)
	}
	return
}

const allTempl = `
type %[1]s []%[2]s

func (xs %[1]s) all(pred func(%[2]s) bool) bool {
	for _, x := range xs {
		if !pred(x) {
			return false
		}
	}
	return true
}
`

func allGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent("all" + T + "slice")
	code = fmt.Sprintf(allTempl, name, T)
	r = rewriteMethod(name)
	return
}

const anyTempl = `
type %[1]s []%[2]s

func (xs %[1]s) any(pred func(%[2]s) bool) bool {
	for _, x := range xs {
		if pred(x) {
			return true
		}
	}
	return false
}
`

func anyGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent("any" + T + "slice")
	code = fmt.Sprintf(anyTempl, name, T)
	r = rewriteMethod(name)
	return
}

const containsSliceTempl = `
type %[1]s []%[2]s

func (xs %[1]s) contains(e %[2]s) bool {
	for _, x := range xs {
		if x == e {
			return true
		}
	}
	return false
}
`

const containsMapTempl = `
type %[1]s map[%[2]s]%[3]s

func (m %[1]s) contains(e %[2]s) bool {
	_, ok := m[e]
	return ok
}
`

func containsGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	switch typ := exprTypes[fn.X].Type.Underlying().(type) {
	case *types.Slice:
		e := typ.Elem().String()
		name = safeIdent("contains" + e + "slice")
		code = fmt.Sprintf(containsSliceTempl, name, e)
	case *types.Map:
		e := typ.Elem().String()
		k := typ.Key().String()
		name = safeIdent("contains" + e + k + "map")
		code = fmt.Sprintf(containsMapTempl, name, e, k)
	}
	r = rewriteMethod(name)
	return
}

const filterTempl = `
type %[1]s []%[2]s

func (xs %[1]s) filter(pred func(%[2]s) bool) []%[2]s {
	var filtered []%[2]s
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
`

const filterReassignTempl = `
type %[1]s []%[2]s

func (xs %[1]s) filter(pred func(%[2]s) bool, reassign []%[2]s) []%[2]s {
	filtered := reassign[:0]
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
`

func filterGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent("filter" + T + "slice")
	if reassign != nil {
		name += "reassign"
		code = fmt.Sprintf(filterReassignTempl, name, T)
		r = rewriteMethodReassign(name, reassign)
	} else {
		code = fmt.Sprintf(filterTempl, name, T)
		r = rewriteMethod(name)
	}
	return
}

const morphTempl = `
type %[1]s []%[2]s

func (xs %[1]s) morph(fn func(%[2]s) %[3]s) []%[3]s {
	morphed := make([]%[3]s, len(xs))
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
`

const morphReassignTempl = `
type %[1]s []%[2]s

func (xs %[1]s) morph(fn func(%[2]s) %[3]s, reassign []%[3]s) []%[3]s {
	var morphed []%[3]s
	if cap(reassign) >= len(xs) {
		morphed = reassign[:len(xs)]
	} else {
		morphed = make([]%[3]s, len(xs))
	}
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
`

func morphGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// determine arg types
	morphFn := exprTypes[args[0]].Type.Underlying().(*types.Signature)
	T := morphFn.Params().At(0).Type().String()
	U := morphFn.Results().At(0).Type().String()
	name = safeIdent("morph" + T + U + "slice")
	if reassign != nil {
		name += "reassign"
		code = fmt.Sprintf(morphReassignTempl, name, T, U)
		r = rewriteMethodReassign(name, reassign)
	} else {
		code = fmt.Sprintf(morphTempl, name, T, U)
		r = rewriteMethod(name)
	}
	return
}

const reduceTempl = `
type %[1]s []%[2]s

func (xs %[1]s) reduce(fn func(%[3]s, %[2]s) %[3]s, acc %[3]s) %[3]s {
	for _, x := range xs {
		acc = fn(acc, x)
	}
	return acc
}
`

const reduce1Templ = `
type %[1]s []%[2]s

func (xs %[1]s) reduce(fn func(%[3]s, %[2]s) %[3]s) %[3]s {
	if len(xs) == 0 {
		panic("reduce of empty slice")
	}
	acc := xs[0]
	for _, x := range xs {
		acc = fn(acc, x)
	}
	return acc
}
`

func reduceGen(fn *ast.SelectorExpr, args []ast.Expr, _ ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// determine arg types
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	U := exprTypes[args[0]].Type.(*types.Signature).Params().At(0).Type().String()
	if len(args) == 1 {
		name = safeIdent("reduce1" + T + U + "slice")
		code = fmt.Sprintf(reduce1Templ, name, T, U)
	} else if len(args) == 2 {
		name = safeIdent("reduce" + T + U + "slice")
		code = fmt.Sprintf(reduceTempl, name, T, U)
	}
	r = rewriteMethod(name)
	return
}

const reverseTempl = `
type %[1]s []%[2]s

func (xs %[1]s) reverse() []%[2]s {
	reversed := make([]%[2]s, len(xs))
	for i := range xs {
		reversed[i] = xs[len(xs)-1-i]
	}
	return reversed
}
`

func reverseGen(fn *ast.SelectorExpr, args []ast.Expr, _ ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// NOTE: we can't safely use reassign because it may be the same slice
	// that we're reversing. Since we don't have a way of knowing (slices
	// don't support ==), we unfortunately cannot ever reuse existing memory.
	//
	// However, it should be safe to reverse in-place when called on a slice
	// literal or as part of a chain.
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent("reverse" + T + "slice")
	code = fmt.Sprintf(reverseTempl, name, T)
	r = rewriteMethod(name)
	return
}

const takeWhileTempl = `
type %[1]s []%[2]s

func (xs %[1]s) takeWhile(pred func(%[2]s) bool) []%[2]s {
	var i int
	for i = range xs {
		if !pred(xs[i]) {
			break
		}
	}
	return append([]%[2]s(nil), xs[:i]...)
}
`

const takeWhileReassignTempl = `
type %[1]s []%[2]s

func (xs %[1]s) takeWhile(pred func(%[2]s) bool, reassign []%[2]s) []%[2]s {
	var i int
	for i = range xs {
		if !pred(xs[i]) {
			break
		}
	}
	return append(reassign[:0], xs[:i]...)
}
`

func takeWhileGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent("takeWhile" + T + "slice")
	if reassign != nil {
		name += "reassign"
		code = fmt.Sprintf(takeWhileReassignTempl, name, T)
		r = rewriteMethodReassign(name, reassign)
	} else {
		code = fmt.Sprintf(takeWhileTempl, name, T)
		r = rewriteMethod(name)
	}
	return
}
