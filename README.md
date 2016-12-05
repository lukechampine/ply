ply
===

`ply` is an experimental compile-to-Go language. Its syntax and semantics are
basically identical to Go's, but with more builtin functions for manipulating
generic containers (slices, arrays, maps). This is accomplished by forking
Go's type-checker, running it on the `.ply` file, and using the resolved types
to generate specific versions of the generic function. For example, given the
following code:

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


FAQ
---

**Why wouldn't you just use [existing generics solution]?**

There are basically two options: runtime generics (via reflection) and
compile-time generics (via codegen). They both suck for different reasons:
reflection is slow, and codegen is cumbersome. Ply is an attempt at making
codegen suck a bit less. You don't need to grapple with magic annotations or
`go generate`; you can just start using `filter` and `reduce` as though Go had
always supported them.

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
