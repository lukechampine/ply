// Package ply is a pseudo-package that documents the builtin functions and
// methods added by the Ply compiler.
//
// All the function and method names in this package are lowercased when
// written in Ply syntax.
//
// Ply methods do not yield method values. That is, this expression is illegal:
//
//     intFilter := ([]int).filter
//
// The provided examples are written in Ply, not Go, so they will not run.
package ply

// T is a generic type.
type T int

// U is a generic type.
type U int

// V is a generic type.
type V int

// SliceT is a slice with element type T.
type SliceT int

// SliceT is a slice with element type U.
type SliceU int

// All returns true if all elements of s satisfy pred. It returns as soon as
// it encounters an element that does not satisfy pred.
func (s SliceT) All(pred func(T) bool) SliceT

// Any returns true if any elements of s satisfy pred. It returns as soon as
// it encounters an element that satisfies pred.
func (s SliceT) Any(pred func(T) bool) SliceT

// Filter returns a new slice containing only the elements of s that satisfy
// pred. If the result is reassigned to an existing slice of the same type,
// Filter will reuse that slice's memory. The common case is reassigning to s,
// in which case Filter will not allocate any memory.
//
// Note that if the result is reassigned, the "excess element memory" cannot
// be garbage collected. For example:
//
//    xs := make([]int, 1000)
//    xs = []int{1, 2, 3}.filter(func(int) bool { return true })
//
// In the above code, xs will contain 1, 2, and 3, and will be resliced to
// have a length of 3. But since xs still holds a reference to 1000 ints, that
// memory cannot be garbage collected until xs goes out of scope. In short: be
// careful when reassigning to large slices. To avoid this optimization,
// assign the result to a new variable.
func (s SliceT) Filter(pred func(T) bool) SliceT

// Morph returns a new slice containing the result of applying fn to each
// element of s. If the result is reassigned to an existing slice whose
// capacity is at least len(s), Morph will reuse that slice's memory. As with
// Filter, be careful when reassigning to large slices.
func (s SliceT) Morph(fn func(T) U) SliceU

// Reduce returns the result of repeatedly applying fn to an initial
// "accumulator" value and each element of s. If no initial value is provided,
// Reduce uses the first element of s. (Note that this is only possible when T
// and U are the same type.)
func (s SliceT) Reduce(fn func(U, T) U, acc U) U

// Reverse returns a new slice containing the elements of s in reverse order.
// Reverse never reverses the elements in-place, as it is currently too
// difficult to detect when this optimization can be safely applied.
func (s SliceT) Reverse() SliceT

// TakeWhile returns a new slice containing the initial elements of s that
// satisfy pred. That is, unlike Filter, the slice returned by TakeWhile is
// guaranteed to be a contiguous subset of s beginning at the first element.
// If the result is reassigned to an existing slice of the same type,
// TakeWhile will reuse that slice's memory. As with Filter, be careful when
// reassigning to large slices.
func (s SliceT) TakeWhile(pred func(T) bool) SliceT

// Merge copies the contents of each map in rest into recv and returns it. If
// recv is nil, a new map will be allocated to hold the contents. Thus it is
// idiomatic to write:
//
//    m3 := merge(nil, m1, m2)
//
// to avoid modifying m1 or m2. Conversely, if it is acceptable to reuse m1's
// memory, write:
//
//    m1 = merge(m1, m2)
//
// Like append, merge is only valid as an expression, not a statement. In
// other words, you *must* make use of its return value.
func Merge(recv, rest ...map[T]U) map[T]U

// Zip calls fn on each successive pair of values in xs and ys and appends the
// result to a new slice, terminating when either xs or ys is exhausted. That is,
// if len(xs) == 3 and len(ys) == 4, then the result is equal to:
//
//    []V{
//        fn(xs[0], ys[0]),
//        fn(xs[1], ys[1]),
//        fn(xs[2], ys[2]),
//    }
//
// If the result is reassigned to an existing slice of type []V whose capacity
// is large enough to hold the resulting elements, Zip will reuse that slice's
// memory.
func Zip(fn func(T, U) V, xs []T, ys []U) []V
