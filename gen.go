package main

import (
	"fmt"
	"go/ast"

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

const mergeTempl = `
func merge%[1]s%[2]s(m1, m2 map[%[1]s]%[2]s) map[%[1]s]%[2]s {
	m3 := make(map[%[1]s]%[2]s)
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
	name = fn.Name + mt.Key().String() + mt.Elem().String()
	code = fmt.Sprintf(mergeTempl, mt.Key().String(), mt.Elem().String())
	return
}

const filterTempl = `
type filter%[1]sslice []%[1]s

func (xs filter%[1]sslice) filter(pred func(%[1]s) bool) []%[1]s {
	var filtered []%[1]s
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
`

func filterGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	st := exprTypes[fn.X].Type.Underlying().(*types.Slice)
	name = fn.Sel.Name + st.Elem().String() + "slice"
	code = fmt.Sprintf(filterTempl, st.Elem().String())
	return
}

const morphTempl = `
type morph%[1]s%[2]sslice []%[1]s

func (xs morph%[1]s%[2]sslice) morph(fn func(%[1]s) %[2]s) []%[2]s {
	morphed := make([]%[2]s, len(xs))
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
`

func morphGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	// determine arg types
	morphFn := args[0].(*ast.FuncLit).Type
	origT := exprTypes[morphFn.Params.List[0].Type].Type
	morphedT := exprTypes[morphFn.Results.List[0].Type].Type
	name = fn.Sel.Name + origT.String() + morphedT.String() + "slice"
	code = fmt.Sprintf(morphTempl, origT.String(), morphedT.String())
	return
}

const reduceTempl = `
type reduce%[1]s%[2]sslice []%[1]s

func (xs reduce%[1]s%[2]sslice) reduce(fn func(%[2]s, %[1]s) %[2]s, acc %[2]s) %[2]s {
	for _, x := range xs {
		acc = fn(acc, x)
	}
	return acc
}
`

func reduceGen(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string) {
	// determine arg types
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	U := exprTypes[args[1]].Type
	name = fn.Sel.Name + T.String() + U.String() + "slice"
	code = fmt.Sprintf(reduceTempl, T.String(), U.String())
	return
}
