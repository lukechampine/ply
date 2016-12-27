package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Parse()
	// compile ply binary
	err := exec.Command("go", "build", "-o", "ply-test").Run()
	if err != nil {
		panic(err)
	}
	e := m.Run()
	os.RemoveAll("ply-test")
	os.Exit(e)
}

func run(code string) (string, error) {
	plyPath, _ := filepath.Abs("./ply-test")
	dir, err := ioutil.TempDir("", "ply")
	if err != nil {
		return "", err
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)
	if err = ioutil.WriteFile("test.ply", []byte(code), 0666); err != nil {
		return "", err
	}
	cmd := exec.Command(plyPath, "test.ply")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	cmd = exec.Command("./" + filepath.Base(dir))
	output, err = cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func TestPly(t *testing.T) {
	tests := map[string]struct {
		code   string
		output string
	}{
		"barebones": {`
package main
func main() {
	println(0)
}`, `0`},

		"simple merge": {`
package main
func main() {
	m := merge(map[int]int{1: 1}, map[int]int{2: 2})
	println(m[1], m[2])
}`, `1 2`},

		"multi merge": {`
package main
func main() {
	m := merge(nil, map[int]int{1: 1}, nil, map[int]int{2: 2})
	println(m[1], m[2])
}`, `1 2`},

		"merge merge": {`
package main
func main() {
	m := merge(merge(nil, map[int]int{1: 1}), merge(nil, map[int]int{2: 2}))
	println(m[1], m[2])
}`, `1 2`},

		"simple filter": {`
package main
func main() {
	xs := []int{1, 2, 3}.filter(func(i int) bool { return i > 1 })
	println(xs[0], xs[1])
}`, `2 3`},

		"simple morph": {`
package main
func main() {
	xs := []int{1, 2, 3}.morph(func(i int) bool { return i%2 == 0 })
	println(xs[0], xs[1], xs[2])
}`, `false true false`},

		"simple reduce": {`
package main
func main() {
	product := func(x, y int) int { return x * y }
	println([]int{1, 2, 3}.reduce(product, 1))
}`, `6`},

		"reduce1": {`
package main
func main() {
	product := func(x, y int) int { return x * y }
	println([]int{1, 2, 3}.reduce(product))
}`, `6`},

		"named type": {`
package main
func main() {
	type ints []int
	product := func(x, y int) int { return x * y }
	println(ints{1, 2, 3}.reduce(product))
}`, `6`},

		"method override": {`
package main;
type ints []int
func (ints) filter(func(x int) bool) ints { return ints{7} }
func main() {
	xs := ints{1, 2, 3}.filter(func(i int) bool { return i > 1 })
	println(xs[0])
}`, `7`},

		"struct literal": {`
package main
func main() {
	type abs []struct{ a, b int }
	xs := abs{{a: 3, b: 4}}.morph(func(c struct{ a, b int }) int { return c.a + c.b })
	println(xs[0])
}`, `7`},

		"selector": {`
package main
func main() {
	type foo struct{ a []int }
	type bar struct{ f foo }
	b := bar{foo{[]int{1, 2, 3}}}
	xs := b.f.a.filter(func(i int) bool { return i > 1 })
	println(xs[0], xs[1])
}`, `2 3`},

		"array type": {`
package main
func main() {
	xs := [][3]int{{}, {}}
	n := xs.reduce(func(acc int, a [3]int) int { return acc + len(a) }, 0)
	println(n)
}`, `6`},

		"pointer type": {`
package main
func main() {
	xs := []*int{nil, nil}
	n := xs.reduce(func(b bool, i *int) bool { return b && i == nil }, true)
	println(n)
}`, `true`},

		"simple chain": {`
package main
func main() {
	gt3 := func(x int) bool { return x > 3 }
	even := func(x int) bool { return x%2 == 0 }
	all := func(acc, x bool) bool { return acc && x }
	xs := []int{1, 2, 3, 4, 6, 20}
	println(xs.filter(gt3).morph(even).reduce(all))
}`, `true`},

		"reverse": {`
package main
func main() {
	xs := []int{1, 2, 3}.reverse()
	println(xs[0], xs[1], xs[2])
}`, `3 2 1`},

		"re-reverse": {`
package main
func main() {
	xs := []int{1, 2, 3}.reverse().reverse()
	println(xs[0], xs[1], xs[2])
}`, `1 2 3`},

		"simple takeWhile": {`
package main
func main() {
	xs := []int{2, 4, 5, 6}.takeWhile(func(i int) bool { return i % 2 == 0 })
	println(len(xs), xs[0], xs[1])
}`, `2 2 4`},

		"simple zip": {`
package main
func main() {
	xs := []int32{0, 2, 4, 6, 100}
	ys := []int64{1, 3, 5, 7}
	add := func(x int32, y int64) int { return int(x) + int(y) }
	zs := zip(add, xs, ys)
	println(len(zs), zs[0], zs[1], zs[2], zs[3])
}`, `4 1 5 9 13`},

		"simple any": {`
package main
func main() {
	even := func(x int) bool { return x%2 == 0 }
	xs := []int{1, 3, 4, 7, 9}
	println(xs.any(even))
}`, `true`},

		"short-circuit any": {`
package main
func main() {
	loudeven := func(x int) bool {
		print(x, " ")
		return x%2 == 0
	}
	xs := []int{1, 3, 4, 7, 9}
	println(xs.any(loudeven))
}`, `1 3 4 true`},

		"simple all": {`
package main
func main() {
	odd := func(x int) bool { return x%2 == 1 }
	xs := []int{1, 3, 4, 7, 9}
	println(xs.all(odd))
}`, `false`},

		"short-circuit all": {`
package main
func main() {
	loudodd := func(x int) bool {
		print(x, " ")
		return x%2 == 1
	}
	xs := []int{1, 3, 4, 7, 9}
	println(xs.all(loudodd))
}`, `1 3 4 false`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			output, err := run(test.code)
			if err != nil {
				t.Errorf("%v: %v", err, output)
			} else if output != test.output {
				t.Errorf("wrong output:\n%q\n\r\texpected:\n%q", output, test.output)
			}
		})
	}
}
