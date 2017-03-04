package codegen

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lukechampine/ply/importer"
	"github.com/lukechampine/ply/types"

	"github.com/tsuna/gorewrite"
	"golang.org/x/tools/go/ast/astutil"
)

// A specializer is a Rewriter that generates specialized versions of each
// generic ply function and rewrites the callsites to use their corresponding
// specialized function.
type specializer struct {
	types       map[ast.Expr]types.TypeAndValue
	fset        *token.FileSet
	pkg         *ast.Package
	fileImports map[string]string   // e.g. "math/big" -> "big"
	implImports map[string]struct{} // new imports required by impls
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

func findImports(fileImports []*ast.ImportSpec, pkgImports map[string]string) map[string]string {
	imports := make(map[string]string)
	for _, i := range fileImports {
		path := i.Path.Value[1 : len(i.Path.Value)-1] // strip surrounding quotes
		var name string
		if i.Name != nil {
			name = i.Name.String()
		} else {
			name = pkgImports[path]
		}
		imports[path] = name
	}
	return imports
}

func (s specializer) addDecl(filename, code string) {
	if _, ok := s.pkg.Files[filename]; ok {
		// check for existence first, because parsing is expensive
		return
	}

	// add package header to code
	code = "package " + s.pkg.Name + code

	// search for import qualifiers and replace them with the proper
	// identifier
	// TODO: not good enough. Needs to be done at AST-level
	for qual, ident := range s.fileImports {
		// HACK: we haven't parsed the code, so we resort to naive string
		// replacement. Thus we must minimize the risk that we accidentally
		// replace something that isn't a package identifier, including
		// strings and ast.SelectorExprs (i.e. struct accessors or method
		// calls). Generated code should never include weird strings that look
		// like package identifiers, and we append a . to filter out non-
		// SelectorExprs. Still, this feels unsafe and I'd prefer to move this
		// replacement to gen.go.
		if ident == "." {
			code = strings.Replace(code, qual+".", "", -1)
		} else {
			code = strings.Replace(code, qual+".", ident+".", -1)
		}
	}

	f, err := parser.ParseFile(s.fset, "", code, 0)
	if err != nil {
		log.Fatal(err)
	}
	s.pkg.Files[filename] = f
}

func (s specializer) Rewrite(node ast.Node) (ast.Node, gorewrite.Rewriter) {
	switch n := node.(type) {
	case *ast.CallExpr:
		var rewrote bool
		switch fn := n.Fun.(type) {
		case *ast.Ident:
			if gen, ok := funcGenerators[fn.Name]; ok {
				if v := s.types[n].Value; v != nil {
					// some functions (namely max/min) may evaluate to a
					// constant, in which case we should replace the call with
					// a constant expression.
					node = ast.NewIdent(v.ExactString())
				} else {
					name, code, rewrite := gen(fn, n.Args, s.types)
					s.addDecl(name, code)
					node = rewrite(n)
					rewrote = true
				}
			}

		case *ast.SelectorExpr:
			// Detect and construct a pipeline if possible. Otherwise,
			// generate a single method.
			var chain []*ast.CallExpr
			cur := n
			for ok := true; ok; cur, ok = cur.Fun.(*ast.SelectorExpr).X.(*ast.CallExpr) {
				if _, ok := cur.Fun.(*ast.SelectorExpr); !ok {
					break
				}
				chain = append(chain, cur)
			}
			if p := buildPipeline(chain, s.types); p != nil {
				name, code, rewrite := p.gen()
				s.addDecl(name, code)
				node = rewrite(n)
				rewrote = true
			} else if gen, ok := methodGenerators[fn.Sel.Name]; ok && !hasMethod(fn.X, fn.Sel.Name, s.types) {
				name, code, rewrite := gen(fn, n.Args, s.types)
				s.addDecl(name, code)
				node = rewrite(n)
				if fn.Sel.Name == "sort" {
					s.implImports["sort"] = struct{}{}
				}
				rewrote = true
			}
		}
		if named, ok := s.types[n].Type.(*types.Named); ok && rewrote {
			// if we rewrote a callsite that returns a named type, cast the
			// expression to the named type directly to prevent the incorrect
			// type from being inferred
			node = &ast.CallExpr{
				Fun:  ast.NewIdent(named.String()),
				Args: []ast.Expr{node.(ast.Expr)},
			}
		}
	}
	return node, s
}

func (s specializer) implBytes() []byte {
	var buf bytes.Buffer
	pcfg := &printer.Config{Tabwidth: 8, Mode: printer.RawFormat}
	pcfg.Fprint(&buf, s.fset, ast.MergePackageFiles(s.pkg, ast.FilterFuncDuplicates|ast.FilterImportDuplicates))
	return buf.Bytes()
}

func astToBytes(fset *token.FileSet, node interface{}) []byte {
	var buf bytes.Buffer
	pcfg := &printer.Config{Tabwidth: 8, Mode: printer.RawFormat | printer.SourcePos}
	pcfg.Fprint(&buf, fset, node)
	return buf.Bytes()
}

// Compile compiles the provided files as a single package. For each supplied
// .ply file, the compiled Go code is returned, keyed by the original filename.
func Compile(filenames []string) (map[string][]byte, error) {
	// parse each supplied file
	fset := token.NewFileSet()
	var files []*ast.File
	plyFiles := make(map[string]*ast.File)
	for _, arg := range filenames {
		f, err := parser.ParseFile(fset, arg, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		if filepath.Ext(arg) == ".ply" {
			plyFiles[arg] = f
		}
	}
	if len(plyFiles) == 0 {
		return nil, nil
	}

	// install each import
	for _, f := range files {
		for _, im := range f.Imports {
			out, err := exec.Command("go", "install", strings.Trim(im.Path.Value, `"`)).CombinedOutput()
			if err != nil {
				return nil, errors.New(string(out))
			}
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
		return nil, err
	}
	// create import map
	pkgImports := make(map[string]string)
	for _, i := range pkg.Imports() {
		pkgImports[i.Path()] = i.Name()
	}

	// walk the AST of each .ply file in the package, generating ply functions
	// and rewriting their callsites
	set := make(map[string][]byte)
	for name, f := range plyFiles {
		// create a specializer
		spec := specializer{
			types: info.Types,
			fset:  fset,
			pkg: &ast.Package{
				Name:  pkg.Name(),
				Files: make(map[string]*ast.File),
			},
			fileImports: findImports(f.Imports, pkgImports),
			implImports: make(map[string]struct{}),
		}

		// rewrite callsites while generating impls
		gorewrite.Rewrite(spec, f)

		// add impl imports
		for importPath := range spec.implImports {
			astutil.AddImport(fset, f, importPath)
		}
		// manually merge f with impls
		code := astToBytes(fset, f)
		impls := spec.implBytes()
		impls = impls[bytes.IndexByte(impls, '\n'):] // remove package decl
		set[name] = append(code, impls...)
	}

	return set, nil
}
