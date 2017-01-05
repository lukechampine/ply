package main

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/lukechampine/ply/importer"
	"github.com/lukechampine/ply/types"
)

// A specializer is an ast.Visitor that generates specialized versions of each
// generic ply function and rewrites the callsites to use their corresponding
// specialized function.
type specializer struct {
	types map[ast.Expr]types.TypeAndValue
	fset  *token.FileSet
	pkg   *ast.Package
}

func hasMethod(recv ast.Expr, method string, exprTypes map[ast.Expr]types.TypeAndValue) bool {
	// TODO: use set.Lookup instead of searching manually
	set := types.NewMethodSet(exprTypes[recv].Type)
	for i := 0; i < set.Len(); i++ {
		if set.At(i).Obj().(*types.Func).Name() == method {
			return true
		}
	}
	return false
}

func (s specializer) addDecl(filename, code string) {
	if _, ok := s.pkg.Files[filename]; ok {
		// check for existence first, because parsing is expensive
		return
	}
	// add package header to code
	code = "package " + s.pkg.Name + code
	f, err := parser.ParseFile(s.fset, "", code, 0)
	if err != nil {
		log.Fatal(err)
	}
	s.pkg.Files[filename] = f
}

func (s specializer) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.CallExpr:
		switch fn := n.Fun.(type) {
		case *ast.Ident:
			if gen, ok := funcGenerators[fn.Name]; ok {
				if v := s.types[n].Value; v != nil {
					// some functions (namely max/min) may evaluate to a
					// constant, in which case we should replace the call with
					// a constant expression.
					//
					// NOTE: This is a bit of a hack, since n is still
					// technically a CallExpr. Ideally we would rewrite it to
					// a BasicLit (?), but that would require access to its
					// parent node.
					fn.Name = ""
					n.Args = []ast.Expr{ast.NewIdent(v.ExactString())}
					break
				}

				name, code, rewrite := gen(fn, n.Args, s.types)
				s.addDecl(name, code)
				rewrite(n)
			}

		case *ast.SelectorExpr:
			// Detect and construct a pipeline if possible. Otherwise,
			// generate a single method.
			var chain []*ast.CallExpr
			cur := n
			for {
				chain = append(chain, cur)
				if sel, ok := cur.Fun.(*ast.SelectorExpr); !ok {
					break
				} else if cur, ok = sel.X.(*ast.CallExpr); !ok {
					break
				}
			}
			if p := buildPipeline(chain, s.types); p != nil {
				name, code, rewrite := p.gen()
				s.addDecl(name, code)
				rewrite(n)
			} else if gen, ok := methodGenerators[fn.Sel.Name]; ok && !hasMethod(fn.X, fn.Sel.Name, s.types) {
				name, code, rewrite := gen(fn, n.Args, s.types)
				s.addDecl(name, code)
				rewrite(n)
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
	plyFiles := make(map[string]*ast.File)
	for _, arg := range flag.Args() {
		f, err := parser.ParseFile(fset, arg, nil, parser.ParseComments)
		if err != nil {
			log.Fatal(err)
		}
		files = append(files, f)
		if filepath.Ext(arg) == ".ply" {
			plyFiles[arg] = f
		}
	}

	// type-check the package
	info := types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	var conf types.Config
	conf.Importer = importer.Default()
	pkg, err := conf.Check("", fset, files, &info)
	if err != nil {
		log.Fatal(err)
	}

	// walk the AST of each .ply file in the package, generating ply functions
	// and rewriting their callsites
	spec := specializer{
		types: info.Types,
		fset:  fset,
		pkg: &ast.Package{
			Name:  pkg.Name(),
			Files: make(map[string]*ast.File),
		},
	}
	for _, f := range plyFiles {
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
	for path, f := range plyFiles {
		goFile, err := os.Create(path + ".go")
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
