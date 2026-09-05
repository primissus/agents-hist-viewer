package cluster_test

import (
	"math"
	"math/rand"
	"testing"

	"claude-code-hist-viewer/internal/adapter/cluster"
)

func normalize(v []float32) []float32 {
	var sumSq float64
	for _, f := range v {
		sumSq += float64(f) * float64(f)
	}
	norm := float32(math.Sqrt(sumSq))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f / norm
	}
	return out
}

// syntheticBlobs builds nPerBlob points tightly clustered around each of the
// given unit-vector centers, for a controlled clustering test.
func syntheticBlobs(centers [][]float32, nPerBlob int, noise float64, seed int64) [][]float32 {
	rng := rand.New(rand.NewSource(seed))
	var points [][]float32
	for _, c := range centers {
		for i := 0; i < nPerBlob; i++ {
			p := make([]float32, len(c))
			for d := range c {
				p[d] = c[d] + float32(rng.NormFloat64()*noise)
			}
			points = append(points, normalize(p))
		}
	}
	return points
}

func TestRunSeparatesDistinctBlobs(t *testing.T) {
	centers := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}
	points := syntheticBlobs(centers, 30, 0.01, 1)

	res := cluster.Run(points, 3, 42, 50)
	if res.K != 3 {
		t.Fatalf("K = %d, want 3", res.K)
	}

	// Each blob of 30 consecutive points should land in a single cluster.
	for b := 0; b < 3; b++ {
		first := res.Assignments[b*30]
		for i := 1; i < 30; i++ {
			if got := res.Assignments[b*30+i]; got != first {
				t.Fatalf("blob %d not cohesive: point %d assigned %d, want %d", b, i, got, first)
			}
		}
	}
	// And the three blobs should map to three distinct clusters.
	seen := map[int]bool{res.Assignments[0]: true, res.Assignments[30]: true, res.Assignments[60]: true}
	if len(seen) != 3 {
		t.Fatalf("expected 3 distinct cluster assignments across blobs, got %d", len(seen))
	}
}

func TestPickKPrefersTrueClusterCount(t *testing.T) {
	centers := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	}
	points := syntheticBlobs(centers, 40, 0.01, 7)

	res := cluster.PickK(points, []int{2, 3, 4, 5, 6}, 0, 42, 50)
	if res.K != 4 {
		t.Fatalf("PickK chose K=%d, want 4", res.K)
	}
}

func TestRunHandlesKGreaterThanPoints(t *testing.T) {
	points := [][]float32{{1, 0}, {0, 1}}
	res := cluster.Run(points, 5, 1, 10)
	if res.K != 2 {
		t.Fatalf("K = %d, want clamped to 2", res.K)
	}
}
