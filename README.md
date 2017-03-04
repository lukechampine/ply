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
even. To compile this, `xs` is wrapped in a new type that has a `filter`
method. Then, that call is wrapped in a new type that has a `morph` method,
and so on.

Note that in most cases, Ply can combine these method chains into a single
"pipeline" that **does not allocate any intermediate slices**. Without
pipelining, `filter` would allocate a slice and pass it to `morph`, which
would allocate another slice and pass it to `fold`. But Ply is able to merge
these methods into a single transformation that does not require allocations,
the same way a (good) human programmer would write it.

Usage
-----

First, install the Ply compiler:

```
go get github.com/lukechampine/ply
```

The `ply` command behaves similarly to the `go` command. In fact, you can run
any `go` subcommand through `ply`, including `build`, `run`, `install`, and
even `test`.

When you run `ply run test.ply`, `ply` parses `test.ply` and generates a
`ply-impls.go` file containing the specific implementations of any generics
used in `test.ply`. It then rewrites `test.ply` as a standard Go file,
`ply-test.go`, that calls those implementations. Finally, `go run` is invoked
on `ply-test.go` and `ply-impls.go`.


Supported Functions and Methods
-------------------------------

**Builtins:** `enum`, `max`, `merge`, `min`, `not`, `zip`

- Planned: `repeat`, `compose`

**Methods:** `all`, `any`, `contains`, `drop`, `dropWhile`, `elems`, `filter`,
`fold`, `foreach`, `keys`, `morph`, `reverse`, `sort`, `take`, `takeWhile`,
`tee`, `toMap`, `toSet`, `uniq`

- Planned: `join`, `replace`, `split`

All functions and methods are documented in the [`ply` pseudo-package](https://godoc.org/github.com/lukechampine/ply/doc).


Supported Optimizations
-----------------------

In many cases we can reduce allocations when using Ply functions and methods.
The Ply compiler will automatically apply these optimizations when it is safe
to do so. However, all optimizations have trade-offs. If performance is
important, you should always read the docstring of each method in order to
understand what optimizations may be applied. Depending on your use case, it
may be necessary to write your own implementation to squeeze out maximum
performance.

**Pipelining:**

Pipelining means chaining together multiple Ply functions and/or methods.
Currently only method chaining is supported. For example:

```go
xs := []int{1, 2, 3, 4, 6, 20}
b := xs.filter(func(x int) bool { return x > 3 }).
        morph(func(x int) bool { return x % 2 == 0 }).
        fold(func(acc, x bool) bool { return acc && x })
```

As written, this chain requires allocating a new slice for the `filter` and a
new slice for the `morph`. But if we were writing this transformation by hand,
we could optimize it like so:

```go
b := true
for _, x := range xs {
	if x > 3 {
		b = b && (x % 2 == 0)
	}
}
```

(A good rule of thumb is that, for most chains, only the allocations in the
final method are required. `fold` doesn't require any allocations, but if the
chain stopped at `morph`, then of course we would still need to allocate
memory in order to return the morphed slice.)

Ply is able to perform the above optimization automatically. The bodies of
`filter`, `morph`, and `fold` are combined into a single method, `pipe`, and
the callsite is rewritten to supply the arguments of each chained function:

```go
xs := []int{1, 2, 3, 4, 6, 20}
b := filtermorphfold(xs).pipe(
		func(x int) bool { return x > 3 },
		func(x int) bool { return x % 2 == 0 },
		func(x, y bool) bool { return x && y })
```

However, not all methods can be pipelined. `reverse` is a good example. If
`reverse` is the first method in the chain, then we can eliminate an
allocation by reversing the order in which we iterate through the slice. We
can also eliminate an allocation if `reverse` is the last method in the chain,
since we can reverse the result in-place. But what do we do if `reverse` is in
the middle? Consider this chain:

```go
xs.takeWhile(even).reverse().morph(square)
```

Since we don't know what `takeWhile` will return, there is no way to pass its
reversed elements to `morph` without allocating an intermediate slice. So we
resort to a less-efficient form, splitting the chain into `takeWhile(even)`
and `reverse().morph(square)`, each of which will perform an allocation.

Fortunately, it is usually possible to reorder the chain such that `reverse`
is the first or last method. In the above, we know that `morph` doesn't affect
the length or order of the slice, so we can move `reverse` to the end and the
result will be the same. Ply can't perform this reordering automatically
though: methods may have side effects that the programmer is relying upon.

Side effects are also problematic because pipelining can change the number of
times a function is called. For example, in this expression:

```go
[]int{1, 2, 3, 4, 6, 20}.morph(fn).take(3)
```

Without pipelining, `fn` is called on every element of the slice. But with
pipelining, it is only called 3 times. So the best practice is to avoid side
effects in functions passed to `morph`, `filter`, etc.

Lastly, it's worth pointing out that pipelining cannot eliminate any
allocations performed inside function arguments. For example, in this chain:

```go
myEnum := func(n int) []int {
	r := make([]int, n)
	for i := range r {
		r[i] = i
	}
	return r
}
concat := func(x, y []int) []int { return append(x, y...) }
list := xs.morph(myEnum).fold(concat)
```

A handwritten version of this chain could eliminate the allocations performed
by `myEnum`, but there is no way to do so programmatically.


**Parallelization (planned):**

Functor operations like `morph` can be trivially parallelized, but this
optimization should not be applied automatically. For small lists, the
overhead is probably not worth it. More importantly, if the function has side
effects, parallelizing may cause a race condition. So this optimization must
be specifically requested by the caller via separate identifiers, e.g.
`pmorph`, `pfilter`, etc.

**Reassignment (planned):**

It is a common pattern to reassign the result of a transformation to the
original variable, for example when filtering or reversing a slice. In such
cases, we would like to reuse the existing slice's memory instead of
allocating a new one. At one time, Ply did this automatically (by detecting
reassignment), but the feature was later removed because it is not provably
safe. If the underlying slice memory is referenced by a different variable,
then silently performing this optimization would affect that memory as well,
which is surprising behavior.

However, this optimization remains important. It is directly in line with
Ply's goal of generating code that is as good as the hand-written version. We
just need a different approach; probably a more explicit one. This could take
the form of separate identifiers (e.g. `rfilter`), similar to parallelization.
But this leads to an unfortunate bifurcation: what if you want both
reassignment and parallelization? So now we need four different forms:
standard, parallel, reassigned, and parallel reassigned, each with its own
identifier. More identifiers means more burden on the programmer, so I'm
hesistant to implement this approach until I've given it more thought.

**Compile-time evaluation:**

A few functions (currently just `max` and `min`) can be evaluated at compile
time if their arguments are also known at compile time. This is similar to how
the builtin `len` and `cap` functions work:

```go
len([3]int) // known at compile-time; compiles to 3

max(1, min(2, 3)) // known at compile time; compiles to 2
```

In theory, it is also possible to perform compile-time evaluation on certain
literals. For example:

```go
[]int{1, 2, 3}.contains(3) // compile to true?
```

We could even go further and support arbitrary compile-time execution. But
that seems a little dangerous. At best, it's useful for things like computing
a large table instead of including it in the source. But I don't think that
single case warrants such a powerful feature.

**Function hoisting (planned):**

`not` currently returns a function that wraps its argument. Instead, `not`
could generate a new top-level function definition, and replace the callsite
wholesale. For example, given these definitions:

```go
even := func(i int) bool { return i % 2 == 0 }
odd := not(even)
```

The compiled code currently looks like this:

```go
func not_int(fn func(int) bool) func(int) bool {
	return func(i int) bool {
		return !fn(i)
	}
}

even := func(i int) bool { return i % 2 == 0 }
odd := not_int(even)
```

But we could improve upon this by generating a top-level `not_even` function:

```go
func not_even(i int) bool {
	return !even(i)
}

even := func(i int) bool { return i % 2 == 0 }
odd := not_even
```

This is non-trivial, though, because `even` is not in the top-level scope; we
would need to hoist its definition into the function body of `not_even`.
Alternatively, we could simply not consider local functions for this
optimization -- but we'd still need a way to distinguish global functions from
local functions.

The motivation for this optimization is that the Go compiler is more likely to
inline top-level functions (AFAIK). Eliminating the overhead of a function
call could be significant when, say, filtering a large slice. Benchmarks are
needed to confirm that this would actually result in a significant speedup.


FAQ
---

**Why wouldn't you just use [existing generics solution]?**

There are basically two options: runtime generics (via reflection) and
compile-time generics (via codegen). They both suck for different reasons:
reflection is slow, and codegen is cumbersome. Ply is an attempt at making
codegen suck a bit less. You don't need to grapple with magic annotations or
custom types; you can just start using `filter` and `fold` as though Go had
always supported them.

**What are the downsides of this approach?**

The most obvious is that it's less flexible; you can only use the functions
and methods that Ply provides. Another annoyance is that since they behave
like builtins, you can't pass them around as first-class values. Fortunately
this is a pretty rare thing to do, and it's possible to work around it in most
cases. (For example, you can wrap the call in a `func`.)

Generating a specific implementation of every generic function call produces
very fast code, at the cost of slower compilation, larger binaries, and less
helpful error messages. Your build process will also be more complicated,
though hopefully not as complicated as writing template code and using `go
generate`. The fact of the matter is that *there is no silver bullet*: every
implementation of generics has its downsides. Do your research before deciding
whether Ply is the right approach for your project.

**What if I want to define my own generic functions?**

Sorry, that's not in the cards. The purpose of Ply is to make polymorphism as
painless as possible. Supporting custom generics would mean defining some kind
of template syntax, and that adds a lot of complexity to the language.
Restricting the set of generic functions also allows the Ply compiler to apply
deep optimizations, such as pipelining.

I understand that this is a controversial position, and Ply's set of functions
may not suit everyone's needs. My rationale is that by adding a small set of
new functions, Go can be made much more productive without becoming any harder
to parse (by computer or by human). If you have suggestions for new functions,
[open an issue](https://github.com/lukechampine/ply/issues) and I'll consider
adding them.

**What about generic data structures?**

Go seems pretty productive without them. Slices and maps are sufficient for
the vast majority of programs. Adding new generic data structures would
complicate Go's syntax (do we overload `make` for our new `RedBlackTree`
type?) and I really want to avoid that. Go's simplicity is one of its biggest
strengths.

**How does Ply interact with the existing Go toolchain?**

One nice thing about Ply is that because it has the same syntax as Go, many
tools built for Go will "just work" with Ply. For example, you can run `gofmt`
and `golint` on `.ply` files. Other tools (like `go vet`) are pickier about
their input filenames ending in `.go`, but will work if you rename your `.ply`
files. Lastly, tools that require type information will fail, because Go's
type-checker does not understand Ply builtins.

One current deficiency is that Ply will not automatically compile imported
`.ply` files. So you can't write pure-Ply packages (yet).

**Will you add support for feature X?**

Open an issue and I will gladly consider it.
