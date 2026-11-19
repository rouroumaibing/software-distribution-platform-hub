package coordination

import (
	"math/rand"
	"slices"
	"testing"
)

// Property tests: deterministic generator, no external dependency. Each property
// states an invariant that must hold for every generated input.

// Idempotence: applying defaults twice is the same as applying once.
func TestQueueOptionsFillIdempotent(t *testing.T) {
	r := rand.New(rand.NewSource(20261118))
	for i := 0; i < 600; i++ {
		o := QueueOptions{
			Capacity:       int(r.Intn(20)) - 5,
			RefillInterval: 0,
			ConsumeRate:    int(r.Intn(20)) - 5,
		}
		o.fill()
		first := o
		o.fill()
		if o != first {
			t.Fatalf("fill not idempotent: %+v -> %+v", first, o)
		}
		if o.Capacity <= 0 || o.RefillInterval <= 0 || o.ConsumeRate <= 0 {
			t.Fatalf("fill left non-positive field: %+v", o)
		}
	}
}

// Equivalence: intSliceEqual agrees with slices.Equal for all generated pairs.
func TestIntSliceEqualEquivalent(t *testing.T) {
	r := rand.New(rand.NewSource(20261119))
	for i := 0; i < 600; i++ {
		a := make([]int, r.Intn(8))
		b := make([]int, r.Intn(8))
		for j := range a {
			a[j] = r.Intn(7) - 3
		}
		// Half the cases clone a into b to exercise the equal branch.
		if r.Intn(2) == 0 {
			b = slices.Clone(a)
		} else {
			for j := range b {
				b[j] = r.Intn(7) - 3
			}
		}
		if got, want := intSliceEqual(a, b), slices.Equal(a, b); got != want {
			t.Fatalf("intSliceEqual(%v,%v)=%v, slices.Equal=%v", a, b, got, want)
		}
	}
}

// Invariants: equality is reflexive and symmetric under mutation-free copies.
func TestIntSliceEqualInvariants(t *testing.T) {
	r := rand.New(rand.NewSource(20261120))
	for i := 0; i < 600; i++ {
		a := make([]int, r.Intn(8))
		for j := range a {
			a[j] = r.Intn(11) - 5
		}
		b := slices.Clone(a)
		if !intSliceEqual(a, a) || !intSliceEqual(a, b) || !intSliceEqual(b, a) {
			t.Fatalf("reflexive/symmetric violated: %v / %v", a, b)
		}
	}
}
