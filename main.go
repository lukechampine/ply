package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"

	"github.com/lukechampine/ply/importer"
	"github.com/lukechampine/ply/types"
)

const mergeTempl = `package ply
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

// A specializer is an ast.Visitor that inserts specialized versions of each
// generic ply function, and rewrites the callsites to use their corresponding
// specialized function.
type specializer struct {
	types    map[ast.Expr]types.TypeAndValue
	newDecls map[string]ast.Decl
}

func (s specializer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.CallExpr:
		fn, ok := n.Fun.(*ast.Ident)
		if !ok {
			// ply functions are always idents
			return s
		}
		if fn.Name == "merge" {
			mt := s.types[n.Args[0]].Type.(*types.Map)
			fn.Name += mt.Key().String() + mt.Elem().String()
			if _, ok := s.newDecls[fn.Name]; !ok {
				// check for existence first, because constructing a new decl
				// is expensive
				fset := token.NewFileSet()
				code := fmt.Sprintf(mergeTempl, mt.Key().String(), mt.Elem().String())
				// TODO: is there an easier way than ParseFile?
				f, err := parser.ParseFile(fset, "", code, 0)
				if err != nil {
					panic(err)
				}
				s.newDecls[fn.Name] = f.Decls[0]
			}
		}
	}
	return s
}

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.ply", nil, 0)
	if err != nil {
		fmt.Println(err)
		return
	}

	info := types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	var conf types.Config
	conf.Importer = importer.Default()
	_, err = conf.Check("ply", fset, []*ast.File{f}, &info)
	if err != nil {
		fmt.Println(err)
		return
	}

	// modify the AST as needed
	spec := specializer{
		types:    info.Types,
		newDecls: make(map[string]ast.Decl),
	}
	ast.Walk(spec, f)
	for _, decl := range spec.newDecls {
		f.Decls = append(f.Decls, decl)
	}

	// output modified AST
	printer.Fprint(os.Stdout, fset, f)
}
