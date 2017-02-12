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

// W is a generic type.
type W int

// SliceT is a slice with element type T. This includes named types whose
// underlying type is []T.
type SliceT int

// MapTU is a map with element type T and key type U. This includes named
// types whose underlying type is map[T]U.
type MapTU int

// Contains returns true if m contains e. It is shorthand for:
//
//    _, ok := m[e]
//    return ok
func (m MapTU) Contains(e T) bool

// Elems returns the elements of m. The order of the elements is not
// specified.
func (m MapTU) Elems() []U

// Filter returns a new map containing only the key/value pairs of m that
// satisfy pred.
func (m MapTU) Filter(pred func(T, U) bool) MapTU

// Keys returns the keys of m. The order of the keys is not specified.
func (m MapTU) Keys() []T

// Morph returns a new map containing the result of applying fn to each
// key/value pair of m. V must be a valid map key type, i.e. a comparable
// type.
func (m MapTU) Morph(fn func(T, U) (V, W)) map[V]W

// All returns true if all elements of s satisfy pred. It returns as soon as
// it encounters an element that does not satisfy pred.
func (s SliceT) All(pred func(T) bool) bool

// Any returns true if any elements of s satisfy pred. It returns as soon as
// it encounters an element that satisfies pred.
func (s SliceT) Any(pred func(T) bool) bool

// Contains returns true if s contains e. T must be a comparable type; see
// https://golang.org/ref/spec#Comparison_operators
//
// As a special case, T may be a slice, map, or function if e is nil.
func (s SliceT) Contains(e T) bool

// Drop returns a slice omitting the first n elements of s. The returned slice
// shares the same underlying memory as s. If n is greater than len(s), the
// latter is used. In other words, Drop is short for:
//
//    s2 := s[min(n, len(s)):]
//
// Note that is s is nil, the returned slice will also be nil, whereas if s is
// merely empty (but non-nil), the returned slice will also be non-nil.
func (s SliceT) Drop(n int) SliceT

// DropWhile returns a new slice omitting the initial elements of s that
// satisfy pred. That is, unlike Filter, the slice returned by DropWhile is
// guaranteed to be a contiguous subset of s beginning at the first element
// that does not satisfy pred.
func (s SliceT) DropWhile(pred func(T) bool) SliceT

// Filter returns a new slice containing only the elements of s that satisfy
// pred.
func (s SliceT) Filter(pred func(T) bool) SliceT

// Fold returns the result of repeatedly applying fn to an initial
// "accumulator" value and each element of s. If no initial value is provided,
// Fold uses the first element of s. Note that this implies that T and U are
// the same type, and that s is not empty. If s is empty and no initial value
// is provided, Fold panics.
//
// Fold is implemented as a "left fold," which may affect the result if fn is
// not associative. Given the example below:
//
//    xs := []int{1, 2, 3, 4}
//    sub := func(x, y int) int { return x - y }
//    xs.fold(sub)
//
// Fold yields ((1 - 2) - 3) - 4 == -8, whereas a "right fold" would instead
// yield 1 - (2 - (3 - 4)) == -2.
func (s SliceT) Fold(fn func(U, T) U, acc U) U

// Foreach calls fn on each element of s.
func (s SliceT) Foreach(fn func(T))

// Morph returns a new slice containing the result of applying fn to each
// element of s.
func (s SliceT) Morph(fn func(T) U) []U

// Reverse returns a new slice containing the elements of s in reverse order.
func (s SliceT) Reverse() SliceT

// Sort returns a new slice containing the elements of s in sorted order,
// according to the less function. If less is not supplied, s must either be
// an ordered type or implement sort.Interface. In the former case, the <
// operator is used as the less function. See
// https://golang.org/ref/spec#Comparison_operators
func (s SliceT) Sort(less func(T, T) bool) SliceT

// Take returns a slice containing the first n elements of s. The returned
// slice shares the same underlying memory as s. If n is greater than len(s),
// the latter is used. In other words, Take is short for:
//
//    s2 := s[:min(n, len(s))]
//
// Note that is s is nil, the returned slice will also be nil, whereas if s is
// merely empty (but non-nil), the returned slice will also be non-nil.
func (s SliceT) Take(n int) SliceT

// TakeWhile returns a new slice containing the initial elements of s that
// satisfy pred. That is, unlike Filter, the slice returned by TakeWhile is
// guaranteed to be a contiguous subset of s beginning at the first element.
func (s SliceT) TakeWhile(pred func(T) bool) SliceT

// Tee calls fn on each element of s and returns s unmodified.
func (s SliceT) Tee(fn func(T)) SliceT

// ToMap returns a map in which each element of s is mapped to a corresponding
// value as computed by fn. Note that if s contains duplicate elements,
// earlier elements will be overwritten in the map. fn is called on every
// element, regardless of the number of duplicates.
func (s SliceT) ToMap(fn func(T) U) map[T]U

// ToSet returns a map containing the elements of s as keys, each mapped to
// the empty struct.
func (s SliceT) ToSet() map[T]struct{}

// Uniq returns a new slice containing the unique elements of s. The order of
// elements is preserved.
func (s SliceT) Uniq() SliceT

// Max returns the larger of x or y, as determined by the > operator. T must
// be an ordered type; see https://golang.org/ref/spec#Comparison_operators
//
// If x and y are constants, then the result of Max is also a constant.
func Max(x, y T) T

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
func Merge(recv map[T]U, rest ...map[T]U) map[T]U

// Min returns the smaller of x or y, as determined by the > operator. T must
// be an ordered type; see https://golang.org/ref/spec#Comparison_operators
//
// If x and y are constants, then the result of Min is also a constant.
func Min(x, y T) T

// Not returns a function with the same signature as fn, but with a negated
// return value. For example, given an "even" function, not(even) returns an
// "odd" function. fn may have any number of arguments, but must have a single
// boolean return value.
func Not(fn T) T

// Zip calls fn on each successive pair of values in xs and ys and appends the
// result to a new slice, terminating when either xs or ys is exhausted. That is,
// if len(xs) == 3 and len(ys) == 4, then the result is equal to:
//
//    []V{
//        fn(xs[0], ys[0]),
//        fn(xs[1], ys[1]),
//        fn(xs[2], ys[2]),
//    }
func Zip(fn func(T, U) V, xs []T, ys []U) []V
