package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"os/exec"

	"github.com/lukechampine/ply/importer"
	"github.com/lukechampine/ply/types"
)

const mergeTempl = `package %[3]s
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
	types map[ast.Expr]types.TypeAndValue
	fset  *token.FileSet
	pkg   *ast.Package
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
			if _, ok := s.pkg.Files[fn.Name]; !ok {
				// check for existence first, because constructing a new decl
				// is expensive
				code := fmt.Sprintf(mergeTempl, mt.Key().String(), mt.Elem().String(), s.pkg.Name)
				f, err := parser.ParseFile(s.fset, "", code, 0)
				if err != nil {
					panic(err)
				}
				s.pkg.Files[fn.Name] = f
			}
		}
	}
	return s
}

func main() {
	log.SetFlags(0)
	flag.Parse()

	// parse each supplied file
	fset := token.NewFileSet()
	var files []*ast.File
	fileNames := make(map[*ast.File]string)
	for _, arg := range flag.Args() {
		f, err := parser.ParseFile(fset, arg, nil, 0)
		if err != nil {
			log.Fatal(err)
		}
		files = append(files, f)
		fileNames[f] = arg
	}

	// type-check the package
	info := types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	var conf types.Config
	conf.Importer = importer.Default()
	pkg, err := conf.Check("ply", fset, files, &info)
	if err != nil {
		log.Fatal(err)
	}

	// walk the AST of each file in the package, generating ply functions and
	// rewriting their callsites
	spec := specializer{
		types: info.Types,
		fset:  fset,
		pkg: &ast.Package{
			Name:  pkg.Name(),
			Files: make(map[string]*ast.File),
		},
	}
	for _, f := range files {
		ast.Walk(spec, f)
	}

	// combine generated ply functions into a single file and write it to the
	// current directory
	merged := ast.MergePackageFiles(spec.pkg, ast.FilterFuncDuplicates|ast.FilterImportDuplicates)
	implFile, err := os.Create("ply_impls.go")
	if err != nil {
		log.Fatal(err)
	}
	printer.Fprint(implFile, fset, merged)

	// output a .go file for each .ply file
	for _, f := range files {
		goFile, err := os.Create(fileNames[f] + ".go")
		if err != nil {
			log.Fatal(err)
		}
		printer.Fprint(goFile, fset, f)
	}

	// invoke the Go compiler
	cmd := exec.Command("go", "build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}
