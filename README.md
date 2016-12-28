ply
===

`ply` is an experimental compile-to-Go language. Its syntax and semantics are
basically identical to Go's, but with more builtin functions for manipulating
generic containers (slices, arrays, maps). This is accomplished by forking
Go's type-checker, running it on the `.ply` file, and using the resolved types
to generate specific versions of the generic function. For example, given the
following Ply code:

```go
m1 := map[int]int{1: 1}
m2 := map[int]int{2: 2}
m3 := merge(m1, m2)
```

`merge` is a generic function. After type-checking, the Ply compiler knows the
types of `m1` and `m2`, so it can generate a specific function for these types:

```go
func mergeintint(m1, m2 map[int]int) map[int]int {
	m3 := make(map[int]int)
	for k, v := range m1 {
		m3[k] = v
	}
	for k, v := range m2 {
		m3[k] = v
	}
	return m3
}
```

`mergeintint` is then substituted for `merge` in the relevant expression, and
the modified source can then be passed to the Go compiler.

A similar approach is used to implement generic methods:

```go
xs := []int{1, 2, 3, 4, 6, 20}
b := xs.filter(func(x int) bool { return x > 3 }).
        morph(func(x int) bool { return x % 2 == 0 }).
        fold(func(x, y bool) bool { return x && y })
```

In the above, `b` is true because all the integers in `xs` greater than 3 are
even. The specific implementation of `morph` looks like:

```go
type morphintboolslice []int

func (xs morphintboolslice) morph(fn func(int) bool) []bool {
	morphed := make([]bool, len(xs))
	for i := range xs {
		morphed[i] = fn(xs[i])
	}
	return morphed
}
```

And to use it, we simply type-cast `xs` to a `morphintboolslice` at the
callsite.

Usage
-----

First, install the Ply compiler:

```
go get github.com/lukechampine/ply
```

`ply test.ply` will compile `test.ply` to `test.ply.go` and generate a
`ply_impls.go` file containing the specific implementations of any generics
used in `test.ply`. It will then invoke `go build` in the directory to
generate an executable.

I'm aware that this isn't very ergonomic. Once most of the generic functions
are implemented, I will make `ply` a more complete build tool. Ideally, it
will function identically to the `go` command.


Supported Functions and Methods
-------------------------------

**Builtins:** `max`, `merge`, `min`, `zip`

- Planned: `sort`, `repeat`, `compose`

**Methods:** `all`, `any`, `contains`, `dropWhile`, `elems`, `filter`, `fold`, `keys`, `morph`, `reverse`, `takeWhile`

- Planned: `join`, `replace`, `split`, `uniq`

All functions and methods are documented in the [`ply` pseudo-package](https://godoc.org/github.com/lukechampine/ply/doc).


Supported Optimizations
-----------------------

In many cases we can reduce allocations when using Ply methods. The Ply
compiler will automatically apply these optimizations when it is safe to do
so. However, many optimizations have trade-offs. If performance is important,
you should always read the docstring of each method in order to understand
what optimizations may be applied. Depending on your use case, it may be
necessary to write your own implementation to squeeze out maximum performance.

**Reassignment:**

When the result of a slice transformation is reassigned to an existing slice,
we can often reuse that slice's memory. For example, a standard `filter` looks
something like this:

```go
func (xs filterintslice) filter(pred func(int) bool) []int {
	var filtered []int
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
```

But if we reassign the result of `filter` to an existing slice (often the same
slice being filtered), a different implementation is used:

```go
func (xs filterintslicereassign) filter(pred func(int) bool, reassign []int) []int {
	filtered := reassign[:0]
	for _, x := range xs {
		if pred(x) {
			filtered = append(filtered, x)
		}
	}
	return filtered
}
```

This comes with a caveat, though: `filtered` now holds a reference to the
underlying memory of `reassign`, so that memory can't be garbage collected. If
`reassign` is very large and `filtered` winds up being small, it may have been
more appropriate to allocate a smaller slice. Even worse, if the elements of
`reassign` hold pointers to more memory, none of that memory can be collected
either. This caveat applies to other methods as well, so read the docstrings
carefully.

**Pipelining (planned):**

Pipelining means chaining together multiple such calls. For example:

```go
xs := []int{1, 2, 3, 4, 6, 20}
b := xs.filter(func(x int) bool { return x > 3 }).
        morph(func(x int) bool { return x % 2 == 0 }).
        fold(func(acc, x bool) bool { return acc && x })
```

Currently, this sequence requires allocating a new slice for the `filter` and
a new slice for the `morph`. But if we were writing this transformation by
hand, we could optimize it like so:

```go
b := true
for _, x := range xs {
	if x > 3 {
		b = b && (x % 2 == 0)
	}
}
```

This version requires no allocations at all! I would very much like to
implement this sort of optimization, but I imagine it will be challenging.

**Parallelization (planned):**

Functor operations like `morph` can be trivially parallelized, but this
optimization should not be applied automatically. For small lists, the
overhead is probably not worth it. More importantly, if the function has side-
effects, parallelizing may cause a race condition. So this optimization must
be specifically requested by the caller via separate identifiers, e.g.
`pmorph`, `pfilter`, etc.

**Compile-time evaluation:**

A few functions (currently just `max` and `min`) can be evaluated at compile-
time if their arguments are also known at compile time. This is similar to how
the builtin `len` and `cap` functions work:

```go
len([3]int) // known at compile-time; compiles to 3

max(1, min(2, 3)) // known at compile time; compiles to 2
```

It is also possible to perform compile-time evaluation on certain literals.
For example:

```go
[]int{1, 2, 3}.contains(3) // compile to true?
```

However, this is not currently implemented, and may well be terrible idea.

FAQ
---

**Why wouldn't you just use [existing generics solution]?**

There are basically two options: runtime generics (via reflection) and
compile-time generics (via codegen). They both suck for different reasons:
reflection is slow, and codegen is cumbersome. Ply is an attempt at making
codegen suck a bit less. You don't need to grapple with magic annotations or
`go generate`; you can just start using `filter` and `fold` as though Go had
always supported them.

**What are the downsides of this approach?**

The most obvious is that it's less flexible; you can only use the functions
and methods that Ply provides. Another annoyance is that since they behave
like builtins, you can't pass them around as first-class values. Fortunately
this is a pretty rare thing to do, and it's possible to work around it in most
cases. (For example, you can wrap the call in a `func`.)

The usual codegen downsides apply as well: slower compilation, larger
binaries, less helpful error messages. Your build process will also be more
complicated, though hopefully less complicated than writing template code and
using `go generate`. The fact of the matter is that *there is no silver
bullet*: every implementation of generics has its downsides. Do your research
before deciding whether Ply is the right approach for your project.

**What if I want to define my own generic functions, though?**

Sorry, that's not in the cards. The purpose of Ply is to make polymorphism as
painless as possible. Supporting custom generics would mean defining some kind
of template syntax, and that sucks. I believe [`gen`](https://clipperhouse.github.io/gen) lets you do that, so
maybe check that out if you really can't live without your special-snowflake
function. Alternatively, [open an issue](https://github.com/lukechampine/ply/issues) for your function and I'll consider
adding it to Ply.

**What about generic data structures?**

Go seems pretty productive without them. 95% of the time, slices and maps are
gonna perform just fine for your use case. And if you're in the 5% where
performance is critical, it's probably worth it to write your own
implementation.

More importantly, adding new generic data structures would complicate Go's
syntax (do we overload `make` for our new `RedBlackTree` type?) and I really
want to avoid that. Go's simplicity is one of its biggest strengths.

**How does Ply interact with the existing Go toolchain?**

Poorly. Once the language is more mature, I'll focus on making it easier to
integrate alongside your existing Go code. Ideally, you could symlink `go` to
`ply` and transparently compile both Go and Ply source.

**Will you add support for feature X?**

Open an issue and I will gladly consider it.
