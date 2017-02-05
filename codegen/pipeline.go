package codegen

// Pipelining
//
// Pipelining is the process of translating this:
//
//    xs.takeWhile(even).morph(square).filter(lessThan100)
//
// into this:
//
//    var filtered []int
//    for _, e1 := range xs {
//        if !even(e1) {
//            break
//        }
//        e2 := square(e1)
//        if lessThan100(e2) {
//            filtered = append(filtered, e2)
//        }
//    }
//    return filtered
//
// Essentially, it is the problem combining fragments of each transformation
// into a coherent whole. The approach taken is thus: first, split each
// transformation into sections. (See the transformation type for a
// description of each section.) Each section contains a "#next" directive
// which indicates where successive sections should be inserted. Begin with
// the "outline" section of the last transformation. In the above example, we
// use the outline of filter(lessThan100), which looks like this:
//
//    var filtered []int
//    #next
//    return filtered
//
// We then insert the "setup" section of each transformation. Most
// transformations do not require additional setup, so nothing is added in
// this example.
//
// Next, we form the loop body with the "loop" section of the first
// transformation (takeWhile). For most transformations, this is a simple
// value loop over xs:
//
//    var filtered []int
//    for _, #e := range xs {
//        #next
//    }
//    return filtered
//
// We then insert the "op" section of each transformation, moving from first
// to last. Each op references the loop variable #e, and may transform it into
// a new variable #e. When wiring the ops together, #e is replaced with the
// current loop variable, and #+e increments the current loop variable. Thus,
// the ops in the example look like this:
//
//    // takeWhile
//    if !even(#e) { // #e -> e1
//        break
//    }
//    #next
//
//    // morph
//    #+e := square(#e) // #e -> e1, #+e -> e2
//    #next
//
//    // filter
//    if !lessThan100(#e) { // #e -> e2
//        continue
//    }
//    #next
//
//
// To complete the function body, we insert the "cons" section of the last
// transformation (filter). This is the section that interacts with the
// outline in order to produce the final return value:
//
//    filtered = append(filtered, #e)
//
// Yielding the finished pipeline:
//
//    var filtered []int
//    for _, e1 := range xs {
//        if !even(e1) {
//            break
//        }
//        e2 := square(e1)
//        if !lessThan100(e2) {
//            continue
//        }
//        filtered = append(filtered, e2)
//    }
//    return filtered
//
// Lastly, we must rewrite the callsite. The chained methods are replaced
// with a single call that combines the arguments to each of the calls. In our
// example:
//
//    __plypipe(xs).pipeline(even, square, lessThan100)
//
// And we are done.

import (
	"go/ast"
	"strconv"
	"strings"

	"github.com/lukechampine/ply/types"
)

type transformation struct {
	// recv is the type of the transformation's receiver.
	recv string
	// params are the types of the transformation's parameters.
	params []string
	// ret is the return type of the transformation.
	ret string

	// outline initializes the value to be returned and ends with a return
	// statement. Only the outline of the primary transformation is inserted.
	outline string
	// setup contains any declarations required by 'op'. This is only needed
	// by transformations whose 'op' is not stateless, such as dropWhile. If
	// empty, setup is assumed to equal "#next".
	setup string
	// loop is the for statement used by the transformation. It must
	// contain the declaration of the variable x.
	loop string
	// op is the meat of the transformation. It may declare new variables or
	// issue control statements (e.g. break, continue). op should not contain
	// a return statement. If empty, op is assumed to equal "#next".
	op string
	// cons is the statement that folds the final variable into the
	// accumulated value to be returned. Only the cons of the primary
	// transformation is inserted. cons does not contain a #next directive.
	cons string

	// typeFn returns the types of the transformation (T, U, etc.) given its
	// calling context.
	typeFn func(*ast.SelectorExpr, []ast.Expr, map[ast.Expr]types.TypeAndValue) []types.Type
}

func (t transformation) specify(call *ast.CallExpr, nargs int, exprTypes map[ast.Expr]types.TypeAndValue) transformation {
	// make a copy of t
	s := t
	s.params = append([]string(nil), t.params...)

	templs := []*string{&s.recv, &s.ret, &s.outline, &s.setup, &s.loop, &s.op, &s.cons}
	for i := range s.params {
		templs = append(templs, &s.params[i])
	}
	typs := s.typeFn(call.Fun.(*ast.SelectorExpr), call.Args, exprTypes)
	for _, templ := range templs {
		// replace types
		for i, typ := range typs {
			typVar := 'T' + byte(i) // T, U, V, etc.
			*templ = strings.Replace(*templ, "#"+string(typVar), typ.String(), -1)
		}
		// replace args
		for i := range call.Args {
			*templ = strings.Replace(*templ, "#arg"+strconv.Itoa(i+1), "__plyarg_"+strconv.Itoa(i+nargs), -1)
		}
		// trim whitespace
		*templ = strings.TrimSpace(*templ)
	}
	return s
}

var safePipeName = func() func() string {
	count := 0
	return func() string {
		count++
		return "__plypipe_" + strconv.Itoa(count)
	}
}()

type pipeline struct {
	kn  int // k1, k2, k3...
	en  int // e1, e2, e3...
	fns []*ast.CallExpr
	ts  []transformation
}

// addSector replaces the #next directive in outer with inner. It also sets
// the value of #k and #e variable directives.
func (p *pipeline) addSector(outer, inner string) string {
	if inner == "" {
		return outer // same result as setting inner = "#next"
	}
	// insert inner at #next directive of outer
	code := strings.Replace(outer, "#next", inner, 1)

	// replace #e ("element var") with current ident
	code = strings.Replace(code, "#e", "e"+strconv.Itoa(p.en), -1)

	// if #e2 is present, increment current ident and replace it
	if strings.Contains(code, "#+e") {
		p.en++
		code = strings.Replace(code, "#+e", "e"+strconv.Itoa(p.en), -1)
	}

	// ditto for #k ("key var")
	code = strings.Replace(code, "#k", "k"+strconv.Itoa(p.kn), -1)
	if strings.Contains(code, "#+k") {
		p.kn++
		code = strings.Replace(code, "#+k", "k"+strconv.Itoa(p.en), -1)
	}

	return code
}

// gen generates a type, method, and rewriter for the given pipeline.
func (p *pipeline) gen() (name, code string, r rewriter) {
	first, last := p.ts[0], p.ts[len(p.ts)-1]

	// begin with outline of last fn
	code = last.outline
	// add setup of each fn
	for _, fn := range p.ts {
		code = p.addSector(code, fn.setup)
	}
	// insert loop of first fn
	code = p.addSector(code, first.loop)
	// add op of each fn
	for _, fn := range p.ts {
		code = p.addSector(code, fn.op)
	}
	// add cons of last fn
	code = p.addSector(code, last.cons)

	// add type and method signature
	var params []string
	for _, t := range p.ts {
		for _, paramType := range t.params {
			param := "__plyarg_" + strconv.Itoa(len(params)) + " " + paramType
			params = append(params, param)
		}
	}
	name = safePipeName()
	code = strings.NewReplacer(
		"#name", name,
		"#T", first.recv,
		"#params", strings.Join(params, ", "),
		"#ret", last.ret,
		"#body", code,
	).Replace(`
type #name #T

func (recv #name) pipeline(#params) #ret {
	#body
}
`)

	// collect args
	var args []ast.Expr
	for _, fn := range p.fns {
		args = append(args, fn.Args...)
	}

	// rewriter
	X := p.fns[0].Fun.(*ast.SelectorExpr).X
	r = func(c *ast.CallExpr) ast.Node {
		c.Fun = &ast.SelectorExpr{
			X: &ast.CallExpr{
				Fun:  ast.NewIdent(name),
				Args: []ast.Expr{X},
			},
			Sel: ast.NewIdent("pipeline"),
		}
		c.Args = args
		return c
	}
	return
}

func buildPipeline(chain []*ast.CallExpr, exprTypes map[ast.Expr]types.TypeAndValue) *pipeline {
	p := &pipeline{kn: 1, en: 1}

	// iterate through chain, which will be in reverse order. Lookup the
	// transformation corresponding to each call in the chain. Stop if no
	// transformation is found, or if certain special conditions are
	// satisfied (e.g. reverse).
	haveReverse := false
	for _, call := range chain {
		e := call.Fun.(*ast.SelectorExpr)
		if _, ok := exprTypes[e.X]; !ok {
			break
		}
		_, isSlice := exprTypes[e.X].Type.Underlying().(*types.Slice)
		_, isMap := exprTypes[e.X].Type.Underlying().(*types.Map)
		if !(isSlice || isMap) {
			// pipelines are only supported on slices and maps
			break
		}
		methodName := e.Sel.Name
		if isSlice {
			methodName += "_slice"
		} else if isMap {
			methodName += "_map"
		}

		if hasMethod(e.X, methodName, exprTypes) {
			// method name override
			break
		}
		if methodName == "fold_slice" && len(call.Args) == 1 {
			methodName = "fold1_slice"
		}

		// lookup the transformation
		t, ok := transformations[methodName]
		if !ok {
			break
		}
		// un-reverse the chain
		p.ts = append([]transformation{t}, p.ts...)
		p.fns = append([]*ast.CallExpr{call}, p.fns...)

		// only one reverse is allowed per pipeline, and it must be at either
		// the beginning or the end
		if methodName == "reverse_slice" {
			if haveReverse {
				// we already have a reverse at the end of the chain, so
				// delete the one we just added
				p.ts = p.ts[1:]
				p.fns = p.fns[1:]
				break
			} else if call == chain[0] {
				// reverse at end of chain
				haveReverse = true
			} else {
				// reverse at beginning of chain
				break
			}
		}
	}

	// pipeline must have at least two methods
	if len(p.ts) < 2 {
		return nil
	}

	// fully specify each transformation (can't be done in previous loop
	// because order matters)
	nargs := 0
	for i := range p.ts {
		p.ts[i] = p.ts[i].specify(p.fns[i], nargs, exprTypes)
		nargs += len(p.fns[i].Args)
	}

	return p
}

var transformations = map[string]transformation{
	// Slice methods

	"all_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) bool`},
		ret:    `bool`,

		outline: `
	#next
	return true
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		if !#arg1(#e) {
			return false
		}
`,
		typeFn: justSliceElem,
	},

	"any_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) bool`},
		ret:    `bool`,

		outline: `
	#next
	return false
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		if #arg1(#e) {
			return true
		}
`,
		typeFn: justSliceElem,
	},

	"contains_slice": transformation{
		recv:   `[]#T`,
		params: []string{`#T`},
		ret:    `bool`,

		outline: `
	#next
	return false
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		if #e == #arg1 {
			return true
		}
`,
		typeFn: justSliceElem,
	},

	"containsNil_slice": transformation{
		recv:   `[]#T`,
		params: []string{`#T`}, // unused
		ret:    `bool`,

		outline: `
	#next
	return false
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		if #e == nil {
			return true
		}
`,
		typeFn: justSliceElem,
	},

	"drop_slice": transformation{
		recv:   `[]#T`,
		params: []string{`int`},
		ret:    `[]#T`,

		outline: `
	var undropped []#T
	#next
	return undropped
`,
		setup: `
	ndropped#arg1 := 0
	#next
`,
		loop: `
	if #arg1 > len(recv) {
		#arg1 = len(recv)
	}
	ndropped#arg1 = #arg1
	for _, #e := range recv[#arg1:] {
		#next
	}
`,
		op: `
		if ndropped#arg1++; ndropped#arg1 <= #arg1 {
			continue
		}
		#next
`,
		cons: `
		undropped = append(undropped, #e)
`,
		typeFn: justSliceElem,
	},

	"dropWhile_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) bool`},
		ret:    `[]#T`,

		outline: `
	var undropped []#T
	#next
	return undropped
`,
		setup: `
	stilldropping#arg1 := true
	#next
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		stilldropping#arg1 = stilldropping#arg1 && #arg1(#e)
		if stilldropping#arg1 {
			continue
		}
		#next
`,
		cons: `
		undropped = append(undropped, #e)
`,
		typeFn: justSliceElem,
	},

	"filter_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) bool`},
		ret:    `[]#T`,

		outline: `
	var filtered []#T
	#next
	return filtered
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		if !#arg1(#e) {
			continue
		}
		#next
`,
		cons: `
		filtered = append(filtered, #e)
`,
		typeFn: justSliceElem,
	},

	"fold_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#U, #T) #U`, `#U`},
		ret:    `#U`,

		outline: `
	acc := #arg1
	#next
	return acc
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		acc = #arg1(acc, #e)
`,
		typeFn: func(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) []types.Type {
			sig := exprTypes[args[0]].Type.(*types.Signature)
			T := sig.Params().At(1).Type()
			U := sig.Params().At(0).Type()
			return []types.Type{T, U}
		},
	},

	"fold1_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T, #T) #T`},
		ret:    `#T`,

		outline: `
	var acc #T
	var accset bool
	#next
	if !accset {
		panic("fold of empty slice")
	}
	return acc
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		if !accset {
			acc = #e
			accset = true
		} else {
			acc = #arg1(acc, #e)
		}
`,
		typeFn: justSliceElem,
	},

	"foreach_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T)`},
		ret:    ``,

		outline: `
	#next
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		#arg1(#e)
`,
		typeFn: justSliceElem,
	},

	"morph_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) #U`},
		ret:    `[]#U`,

		outline: `
	var morphed []#U
	#next
	return morphed
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		#+e := #arg1(#e)
		#next
`,
		cons: `
		morphed = append(morphed, #e)
`,
		typeFn: func(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) []types.Type {
			sig := exprTypes[args[0]].Type.Underlying().(*types.Signature)
			T := sig.Params().At(0).Type()
			U := sig.Results().At(0).Type()
			return []types.Type{T, U}
		},
	},

	"reverse_slice": transformation{
		recv:   `[]#T`,
		params: nil,
		ret:    `[]#T`,

		outline: `
	var reversed []#T
	#next
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
`,
		loop: `
	for i := range recv {
		#e := recv[len(recv)-i-1]
		#next
	}
`,
		cons: `
		reversed = append(reversed, #e)
`,
		typeFn: justSliceElem,
	},

	"take_slice": transformation{
		recv:   `[]#T`,
		params: []string{`int`},
		ret:    `[]#T`,

		outline: `
	var taken []#T
	#next
	return taken
`,
		setup: `
	ntaken#arg1 := 0
	#next
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		if ntaken#arg1++; ntaken#arg1 > #arg1 {
			break
		}
		#next
`,
		cons: `
		taken = append(taken, #e)
`,
		typeFn: justSliceElem,
	},

	"takeWhile_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T) bool`},
		ret:    `[]#T`,

		outline: `
	var taken []#T
	#next
	return taken
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		if !arg1(#e) {
			break
		}
		#next
`,
		cons: `
		taken = append(taken, #e)
`,
		typeFn: justSliceElem,
	},

	"tee_slice": transformation{
		recv:   `[]#T`,
		params: []string{`func(#T)`},
		ret:    `[]#T`,

		outline: `
	#next
	return recv
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		op: `
		#arg1(#e)
		#next
`,
		typeFn: justSliceElem,
	},

	"toSet_slice": transformation{
		recv:   `[]#T`,
		params: nil,
		ret:    "map[#T]struct{}",

		outline: `
	set := make(map[#T]struct{})
	#next
	return set
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		set[#e] = struct{}{}
`,
		typeFn: justSliceElem,
	},

	// Map methods

	"elems_map": transformation{
		recv:   `map[#T]#U`,
		params: nil,
		ret:    `[]#U`,

		outline: `
	var elems []#U
	#next
	return elems
`,
		loop: `
	for _, #e := range recv {
		#next
	}
`,
		cons: `
		elems = append(elems, #e)
`,
		typeFn: justMapKeyElem,
	},

	"filter_map": transformation{
		recv:   `map[#T]#U`,
		params: []string{`func(#T, #U) bool`},
		ret:    `map[#T]#U`,

		outline: `
	filtered := make(map[#T]#U)
	#next
	return filtered
`,
		loop: `
	for #k, #e := range recv {
		#next
	}
`,
		op: `
		if !#arg1(#k, #e) {
			continue
		}
		#next
`,
		cons: `
		filtered[#k] = #e
`,
		typeFn: justMapKeyElem,
	},

	"keys_map": transformation{
		recv:   `map[#T]#U`,
		params: nil,
		ret:    `[]#T`,

		outline: `
	var keys []#T
	#next
	return keys
`,
		// special case: store map key in #e because other transformations expect
		// to operate on #e
		loop: `
	for #e := range recv {
		#next
	}
`,
		cons: `
		keys = append(keys, #e)
`,
		typeFn: justMapKeyElem,
	},

	"morph_map": transformation{
		recv:   `map[#T]#U`,
		params: []string{`func(#T, #U) (#V, #W)`},
		ret:    `map[#V]#W`,

		outline: `
	morphed := make(map[#V]#W)
	#next
	return morphed
`,
		loop: `
	for #k, #e := range recv {
		#next
	}
`,
		op: `
		#+k, #+e := #arg1(#k, #e)
		#next
`,
		cons: `
		morphed[#k] = #e
`,
		typeFn: func(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) []types.Type {
			sig := exprTypes[args[0]].Type.Underlying().(*types.Signature)
			T := sig.Params().At(0).Type()
			U := sig.Params().At(1).Type()
			V := sig.Results().At(0).Type()
			W := sig.Results().At(1).Type()
			return []types.Type{T, U, V, W}
		},
	},
}

func justSliceElem(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) []types.Type {
	T := exprTypes[fn.X].Type.Underlying().(*types.Slice).Elem()
	return []types.Type{T}
}

func justMapKeyElem(fn *ast.SelectorExpr, args []ast.Expr, exprTypes map[ast.Expr]types.TypeAndValue) []types.Type {
	m := exprTypes[fn.X].Type.Underlying().(*types.Map)
	T, U := m.Key(), m.Elem()
	return []types.Type{T, U}
}
