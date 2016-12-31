package main

import (
	"go/ast"
	"strconv"
	"strings"
	"time"

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

var funcGenerators = map[string]func(*ast.Ident, []ast.Expr, ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string, rewriter){
	"max":   maxGen,
	"merge": mergeGen,
	"min":   minGen,
	"not":   notGen,
	"zip":   zipGen,
}

var methodGenerators = map[string]func(*ast.SelectorExpr, []ast.Expr, ast.Expr, map[ast.Expr]types.TypeAndValue) (string, string, rewriter){
	"all":       allGen,
	"any":       anyGen,
	"contains":  containsGen,
	"dropWhile": dropWhileGen,
	"elems":     elemsGen,
	"filter":    filterGen,
	"fold":      foldGen,
	"keys":      keysGen,
	"morph":     morphGen,
	"reverse":   reverseGen,
	"takeWhile": takeWhileGen,
	"toSet":     toSetGen,
}

var rand = uint32(time.Now().UnixNano())

func nextSuffix() string {
	rand = rand*1664525 + 1013904223 // constants from ioutil.nextSuffix
	return strconv.Itoa(int(1e9 + rand%1e9))[1:]
}

func randFnName(name string) string   { return "__plyfn_" + name + "_" + nextSuffix() }
func randTypeName(name string) string { return "__plytype_" + name + "_" + nextSuffix() }

func specify(templ, name string, typs ...types.Type) string {
	code := strings.Replace(templ, "#name", name, -1)
	for i, t := range typs {
		typVar := 'T' + byte(i) // T, U, V, etc.
		code = strings.Replace(code, "#"+string(typVar), t.String(), -1)
	}
	return code
}

func genFunc(templ, fnname string, typs ...types.Type) (name, code string, r rewriter) {
	name = randFnName(fnname)
	code = specify(templ, name, typs...)
	r = rewriteFunc(name)
	return
}

func genMethod(templ, methodname string, typs ...types.Type) (name, code string, r rewriter) {
	name = randFnName(methodname)
	code = specify(templ, name, typs...)
	r = rewriteMethod(name)
	return
}

const maxTempl = `
func #name(a, b #T) #T {
	if a > b {
		return a
	}
	return b
}
`

func maxGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[args[0]].Type
	return genFunc(maxTempl, "max", T)
}

const mergeTempl = `
func #name(recv map[#T]#U, rest ...map[#T]#U) map[#T]#U {
	if len(rest) == 0 {
		return recv
	} else if recv == nil {
		recv = make(map[#T]#U, len(rest[0]))
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
	var mt *types.Map
	for _, arg := range args {
		var ok bool
		if mt, ok = exprTypes[arg].Type.(*types.Map); ok {
			break
		}
	}
	return genFunc(mergeTempl, "merge", mt.Key(), mt.Elem())
}

const minTempl = `
func #name(a, b #T) #T {
	if a < b {
		return a
	}
	return b
}
`

func minGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[args[0]].Type
	return genFunc(minTempl, "min", T)
}

const notTempl = `
func #name(fn #T) #T {
	return #T {
		return !fn(#args)
	}
}
`

func notGen(fn *ast.Ident, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	sig := exprTypes[args[0]].Type.Underlying().(*types.Signature)
	callArgs := make([]string, sig.Params().Len())
	for i := range callArgs {
		callArgs[i] = sig.Params().At(i).Name()
	}
	name, code, r = genFunc(notTempl, "not", sig)
	// not requires an additional rewrite for the arguments
	code = strings.Replace(code, "#args", strings.Join(callArgs, ", "), -1)
	return
}

const zipTempl = `
func #name(fn func(a #T, b #U) #V, a []#T, b []#U) []#V {
	var zipped []#V
	if len(a) < len(b) {
		zipped = make([]#V, len(a))
	} else {
		zipped = make([]#V, len(b))
	}
	for i := range zipped {
		zipped[i] = fn(a[i], b[i])
	}
	return zipped
}
`

const zipReassignTempl = `
func #name(fn func(a #T, b #U) #V, a []#T, b []#U, reassign []#V) []#V {
	var n int = len(a)
	if len(b) < len(a) {
		n = len(b)
	}
	var zipped []#V
	if cap(reassign) >= n {
		zipped = reassign[:n]
	} else {
		zipped = make([]#V, n)
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
	T := sig.Params().At(0).Type()
	U := sig.Params().At(1).Type()
	V := sig.Results().At(0).Type()
	if reassign == nil {
		return genFunc(zipTempl, "zip", T, U, V)
	}
	name, code, _ = genFunc(zipReassignTempl, "zip_reassign", T, U, V)
	r = rewriteFuncReassign(name, reassign)
	return
}

const allTempl = `
type #name []#T

func (xs #name) all(pred func(#T) bool) bool {
	for _, x := range xs {
		if !pred(x) {
			return false
		}
	}
	return true
}
`

func allGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	return genMethod(allTempl, "all_slice", T)
}

const anyTempl = `
type #name []#T

func (xs #name) any(pred func(#T) bool) bool {
	for _, x := range xs {
		if pred(x) {
			return true
		}
	}
	return false
}
`

func anyGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	return genMethod(anyTempl, "any_slice", T)
}

const containsSliceTempl = `
type #name []#T

func (xs #name) contains(e #T) bool {
	for _, x := range xs {
		if x == e {
			return true
		}
	}
	return false
}
`

const containsSliceNilTempl = `
type #name []#T

func (xs #name) contains(_ #T) bool {
	for _, x := range xs {
		if x == nil {
			return true
		}
	}
	return false
}
`

const containsMapTempl = `
type #name map[#T]#U

func (m #name) contains(e #T) bool {
	_, ok := m[e]
	return ok
}
`

func containsGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	switch typ := exprTypes[fn.X].Type.Underlying().(type) {
	case *types.Slice:
		if T := typ.Elem(); !types.Comparable(T) {
			// if type is not comparable, then the argument must be nil
			// (otherwise type-check would have failed)
			return genMethod(containsSliceNilTempl, "contains_slice_nil", T)
		} else {
			return genMethod(containsSliceTempl, "contains_slice", T)
		}
	case *types.Map:
		return genMethod(containsMapTempl, "contains_map", typ.Key(), typ.Elem())
	}
	return
}

const dropWhileTempl = `
type #name []#T

func (xs #name) dropWhile(pred func(#T) bool) []#T {
	var i int
	for i = range xs {
		if !pred(xs[i]) {
			break
		}
	}
	return append([]#T(nil), xs[i:]...)
}
`

const dropWhileReassignTempl = `
type #name []#T

func (xs #name) dropWhile(pred func(#T) bool, reassign []#T) []#T {
	var i int
	for i = range xs {
		if !pred(xs[i]) {
			break
		}
	}
	return append(reassign[:0], xs[i:]...)
}
`

func dropWhileGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	if reassign == nil {
		return genMethod(dropWhileTempl, "dropWhile_slice", T)
	}
	name, code, _ = genMethod(dropWhileReassignTempl, "dropWhile_slice_reassign", T)
	r = rewriteMethodReassign(name, reassign)
	return
}

const elemsTempl = `
type #name map[#T]#U

func (m #name) elems() []#U {
	es := make([]#U, 0, len(m))
	for _, e := range m {
		es = append(es, e)
	}
	return es
}
`

const elemsReassignTempl = `
type #name map[#T]#U

func (m #name) elems(reassign []#U) []#U {
	var es []#U
	if cap(reassign) >= len(m) {
		es = reassign[:0]
	} else {
		es = make([]#U, 0, len(m))
	}
	for _, e := range m {
		es = append(es, e)
	}
	return es
}
`

func elemsGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	mt := exprTypes[fn.X].Type.Underlying().(*types.Map)
	if reassign == nil {
		return genMethod(elemsTempl, "elems_map", mt.Key(), mt.Elem())
	}
	name, code, _ = genMethod(elemsReassignTempl, "elems_map_reassign", mt.Key(), mt.Elem())
	r = rewriteMethodReassign(name, reassign)
	return
}

const filterTempl = `
type #name []#T

func (xs #name) filter(pred func(#T) bool) []#T {
	var filtered []#T
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
`

const filterReassignTempl = `
type #name []#T

func (xs #name) filter(pred func(#T) bool, reassign []#T) []#T {
	filtered := reassign[:0]
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
`

const filterMapTempl = `
type #name map[#T]#U

func (m #name) filter(pred func(#T, #U) bool) map[#T]#U {
	if m == nil {
		return nil
	}
	filtered := make(map[#T]#U)
	for k, e := range m {
		if pred(k, e) {
			filtered[k] = e
		}
	}
	return filtered
}
`

func filterGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	switch typ := exprTypes[fn.X].Type.Underlying().(type) {
	case *types.Slice:
		if reassign == nil {
			return genMethod(filterTempl, "filter_slice", typ.Elem())
		}
		name, code, _ = genMethod(filterReassignTempl, "filter_slice_reassign", typ.Elem())
		r = rewriteMethodReassign(name, reassign)
		return
	case *types.Map:
		return genMethod(filterMapTempl, "filter_map", typ.Key(), typ.Elem())
	}
	return
}

const foldTempl = `
type #name []#T

func (xs #name) fold(fn func(#U, #T) #U, acc #U) #U {
	for _, x := range xs {
		acc = fn(acc, x)
	}
	return acc
}
`

const fold1Templ = `
type #name []#T

func (xs #name) fold(fn func(#U, #T) #U) #U {
	if len(xs) == 0 {
		panic("fold of empty slice")
	}
	acc := xs[0]
	for _, x := range xs[1:] {
		acc = fn(acc, x)
	}
	return acc
}
`

func foldGen(fn *ast.SelectorExpr, args []ast.Expr, _ ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	// determine arg types
	sig := exprTypes[args[0]].Type.(*types.Signature)
	T := sig.Params().At(1).Type()
	U := sig.Params().At(0).Type()
	if len(args) == 1 {
		return genMethod(fold1Templ, "fold1_slice", T, U)
	} else if len(args) == 2 {
		return genMethod(foldTempl, "fold_slice", T, U)
	}
	return
}

const keysTempl = `
type #name map[#T]#U

func (m #name) keys() []#T {
	ks := make([]#T, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
`

const keysReassignTempl = `
type #name map[#T]#U

func (m #name) keys(reassign []#T) []#T {
	var ks []#T
	if cap(reassign) >= len(m) {
		ks = reassign[:0]
	} else {
		ks = make([]#T, 0, len(m))
	}
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
`

func keysGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	mt := exprTypes[fn.X].Type.Underlying().(*types.Map)
	if reassign == nil {
		return genMethod(keysTempl, "keys_map", mt.Key(), mt.Elem())
	}
	name, code, _ = genMethod(keysReassignTempl, "keys_map_reassign", mt.Key(), mt.Elem())
	r = rewriteMethodReassign(name, reassign)
	return
}

const morphTempl = `
type #name []#T

func (xs #name) morph(fn func(#T) #U) []#U {
	morphed := make([]#U, len(xs))
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
`

const morphReassignTempl = `
type #name []#T

func (xs #name) morph(fn func(#T) #U, reassign []#U) []#U {
	var morphed []#U
	if cap(reassign) >= len(xs) {
		morphed = reassign[:len(xs)]
	} else {
		morphed = make([]#U, len(xs))
	}
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
`

const morphMapTempl = `
type #name map[#T]#U

func (m #name) morph(fn func(#T, #U) (#V, #W)) map[#V]#W {
	if m == nil {
		return nil
	}
	morphed := make(map[#V]#W, len(m))
	for k, e := range m {
		mk, me := fn(k, e)
		morphed[mk] = me
	}
	return morphed
}
`

func morphGen(fn *ast.SelectorExpr, args []ast.Expr, reassign ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	sig := exprTypes[args[0]].Type.Underlying().(*types.Signature)
	switch exprTypes[fn.X].Type.Underlying().(type) {
	case *types.Slice:
		T := sig.Params().At(0).Type()
		U := sig.Results().At(0).Type()
		if reassign == nil {
			return genMethod(morphTempl, "morph_slice", T, U)
		}
		name, code, _ = genMethod(morphReassignTempl, "morph_slice_reassign", T, U)
		r = rewriteMethodReassign(name, reassign)
		return
	case *types.Map:
		T := sig.Params().At(0).Type()
		U := sig.Params().At(1).Type()
		V := sig.Results().At(0).Type()
		W := sig.Results().At(1).Type()
		return genMethod(morphMapTempl, "morph_map", T, U, V, W)
	}
	return
}

const reverseTempl = `
type #name []#T

func (xs #name) reverse() []#T {
	reversed := make([]#T, len(xs))
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
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	return genMethod(reverseTempl, "reverse_slice", T)
}

const takeWhileTempl = `
type #name []#T

func (xs #name) takeWhile(pred func(#T) bool) []#T {
	var i int
	for i = range xs {
		if !pred(xs[i]) {
			break
		}
	}
	return append([]#T(nil), xs[:i]...)
}
`

const takeWhileReassignTempl = `
type #name []#T

func (xs #name) takeWhile(pred func(#T) bool, reassign []#T) []#T {
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
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	if reassign == nil {
		return genMethod(takeWhileTempl, "takeWhile_slice", T)
	}
	name, code, _ = genMethod(takeWhileReassignTempl, "takeWhile_slice_reassign", T)
	r = rewriteMethodReassign(name, reassign)
	return
}

const toSetTempl = `
type #name []#T

func (xs #name) toSet() map[#T]struct{} {
	set := make(map[#T]struct{})
	for _, x := range xs {
		set[x] = struct{}{}
	}
	return set
}
`

func toSetGen(fn *ast.SelectorExpr, args []ast.Expr, _ ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) (name, code string, r rewriter) {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	return genMethod(toSetTempl, "toSet_slice", T)
}
