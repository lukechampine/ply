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
}

var methodGenerators = map[string]genMethod{
	"filter": filterGen,
	"morph":  morphGen,
	"reduce": reduceGen,
}

// some types may have "unfriendly" names, e.g. "chan int". Need to sanitize
// these before concatenating them into a new ident.
func safeIdent(s string) string {
	return strings.NewReplacer(
		// slices/arrays
		"[", "",
		"]", "",
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
func %[1]s(m1, m2 map[%[2]s]%[3]s) map[%[2]s]%[3]s {
	m3 := make(map[%[2]s]%[3]s)
	for k, v := range m1 {
		m3[k] = v
	}
	for k, v := range m2 {
		m3[k] = v
	}
	return m3
}
`

func mergeGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	mt := exprTypes[args[0]].Type.(*types.Map)
	key, elem := mt.Key().String(), mt.Elem().String()
	name = safeIdent("merge" + key + elem)
	code = fmt.Sprintf(mergeTempl, name, key, elem)
	r = rewriteFunc(name)
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
	if len(reassign) >= len(xs) {
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

func reduceGen(fn *ast.SelectorExpr, args []ast.Expr, _ ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// determine arg types
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	U := exprTypes[args[1]].Type.String()
	name = safeIdent("reduce" + T + U + "slice")
	code = fmt.Sprintf(reduceTempl, name, T, U)
	r = rewriteMethod(name)
	return
}
