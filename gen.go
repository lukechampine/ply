package main

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/lukechampine/ply/types"
)

type genFunc func(*ast.Ident, []ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string)

type genMethod func(*ast.SelectorExpr, []ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string)

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

func mergeGen(fn *ast.Ident, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	mt := exprTypes[args[0]].Type.(*types.Map)
	key, elem := mt.Key().String(), mt.Elem().String()
	name = safeIdent(fn.Name + key + elem)
	code = fmt.Sprintf(mergeTempl, name, key, elem)
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

func filterGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	name = safeIdent(fn.Sel.Name + T + "slice")
	code = fmt.Sprintf(filterTempl, name, T)
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

func morphGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	// determine arg types
	morphFn := args[0].(*ast.FuncLit).Type
	T := exprTypes[morphFn.Params.List[0].Type].Type.String()
	U := exprTypes[morphFn.Results.List[0].Type].Type.String()
	name = safeIdent(fn.Sel.Name + T + U + "slice")
	code = fmt.Sprintf(morphTempl, name, T, U)
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

func reduceGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	// determine arg types
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem().String()
	U := exprTypes[args[1]].Type.String()
	name = safeIdent(fn.Sel.Name + T + U + "slice")
	code = fmt.Sprintf(reduceTempl, name, T, U)
	return
}
