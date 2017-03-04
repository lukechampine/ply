package main

import (
	"flag"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lukechampine/ply/codegen"
)

var (
	// to be supplied at build time
	version   = "?"
	githash   = "?"
	builddate = "?"
)

func isFileList(args []string) bool {
	return len(args) > 0 && (strings.HasSuffix(args[0], ".go") || strings.HasSuffix(args[0], ".ply"))
}

// adhoc parses args as a set of files comprising an ad-hoc package. All files
// must be in the same directory.
func adhoc(args []string) (dir string, files []string, _ error) {
	dir = filepath.Dir(args[0])
	for _, arg := range args {
		if !(strings.HasSuffix(arg, ".go") || strings.HasSuffix(arg, ".ply")) {
			return "", nil, fmt.Errorf("named files must be .go or .ply files: %s", arg)
		} else if filepath.Dir(arg) != dir {
			return "", nil, fmt.Errorf("named files must all be in one directory; have %s and %s", dir, filepath.Dir(arg))
		}
		files = append(files, arg)
	}
	return dir, files, nil
}

// packages parses args as a set of package files, keyed by their directory.
// If xtest is true, files ending in _test are excluded.
func packages(args []string, xtest bool) (map[string][]string, error) {
	pkgs := make(map[string][]string)
	if len(args) == 0 {
		args = []string{"."} // current directory
	}
	for _, arg := range args {
		pkg, err := build.Import(arg, ".", build.FindOnly)
		if err != nil {
			return nil, err
		}
		dir, err := os.Open(pkg.Dir)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
		filenames, err := dir.Readdirnames(0)
		if err != nil {
			return nil, err
		}
		for _, file := range filenames {
			if strings.HasPrefix(file, "ply-") {
				// don't include previous codegen; it will cause redefinition
				// errors
				continue
			}
			if xtest && (strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, "_test.ply")) {
				// exclude test files if xtest is set
				continue
			}
			if strings.HasSuffix(file, ".go") || strings.HasSuffix(file, ".ply") {
				pkgs[pkg.Dir] = append(pkgs[pkg.Dir], filepath.Join(pkg.Dir, file))
			}
		}
	}
	return pkgs, nil
}

func main() {
	log.SetFlags(0)
	goFlags := flag.String("goflags", "", "Flags to be supplied to the Go compiler")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 || args[0] == "version" {
		fmt.Printf("ply v%s\nCommit: %s\nBuild Date: %s\n", version, githash, builddate)
		return
	}

	if isFileList(args[1:]) {
		dir, pkg, err := adhoc(args[1:])
		if err != nil {
			log.Fatal(err)
		}

		// delete .ply files from args
		noply := args[:0]
		for _, arg := range args {
			if filepath.Ext(arg) != ".ply" {
				noply = append(noply, arg)
			}
		}
		args = noply

		plyFiles, err := codegen.Compile(pkg)
		if err != nil {
			log.Fatal(err)
		}
		for name, code := range plyFiles {
			// .go -> .ply
			filename := filepath.Join(dir, "ply-"+strings.Replace(filepath.Base(name), ".ply", ".go", -1))
			err = ioutil.WriteFile(filename, code, 0666)
			if err != nil {
				log.Fatal(err)
			}
			// add compiled .ply files to args
			args = append(args, filename)
		}
	} else if args[0] == "run" {
		log.Fatal("ply run: no .ply or .go files listed")
	} else {
		xtest := args[0] != "test"
		pkgs, err := packages(args[1:], xtest)
		if err != nil {
			log.Fatal(err)
		}

		// for each package, compile the .ply files and write them to the
		// package directory.
		for dir, pkg := range pkgs {
			plyFiles, err := codegen.Compile(pkg)
			if err != nil {
				log.Fatal(err)
			}
			for name, code := range plyFiles {
				// .go -> .ply
				filename := filepath.Join(dir, "ply-"+strings.Replace(filepath.Base(name), ".ply", ".go", -1))
				err = ioutil.WriteFile(filename, code, 0666)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
	}

	// if just compiling, exit early
	if args[0] == "compile" {
		return
	}

	// invoke the Go compiler, passing on any flags
	args = append(append([]string{args[0]}, strings.Fields(*goFlags)...), args[1:]...)
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if _, ok := err.(*exec.ExitError); !ok && err != nil {
		log.Fatal(err)
	}
}
