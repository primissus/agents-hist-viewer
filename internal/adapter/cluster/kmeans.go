// Package cluster implements a pure-Go k-means++ over []float32 vectors.
// Input vectors are assumed L2-normalized (as stored by adapter/sqlite):
// squared Euclidean distance on unit vectors is a monotonic transform of
// cosine distance, so plain k-means works without a custom distance metric.
package cluster

import (
	"math"
	"math/rand"
)

type Result struct {
	K           int
	Assignments []int // cluster index per input point, same order as input
	Centroids   [][]float32
	Inertia     float64
}

// Run clusters points into k groups using k-means++ initialization and
// Lloyd's algorithm, seeded for reproducibility.
func Run(points [][]float32, k int, seed int64, maxIter int) Result {
	n := len(points)
	if n == 0 || k <= 0 {
		return Result{}
	}
	if k > n {
		k = n
	}
	if maxIter <= 0 {
		maxIter = 50
	}
	rng := rand.New(rand.NewSource(seed))
	centroids := initPlusPlus(points, k, rng)
	assignments := make([]int, n)

	for iter := 0; iter < maxIter; iter++ {
		changed := false
		for i, p := range points {
			best, bestDist := 0, math.MaxFloat64
			for c, centroid := range centroids {
				if d := sqDist(p, centroid); d < bestDist {
					bestDist, best = d, c
				}
			}
			if assignments[i] != best {
				assignments[i] = best
				changed = true
			}
		}

		dim := len(points[0])
		sums := make([][]float64, k)
		counts := make([]int, k)
		for c := range sums {
			sums[c] = make([]float64, dim)
		}
		for i, p := range points {
			c := assignments[i]
			counts[c]++
			for d, v := range p {
				sums[c][d] += float64(v)
			}
		}
		newCentroids := make([][]float32, k)
		for c := 0; c < k; c++ {
			if counts[c] == 0 {
				newCentroids[c] = centroids[c]
				continue
			}
			vec := make([]float32, dim)
			for d := 0; d < dim; d++ {
				vec[d] = float32(sums[c][d] / float64(counts[c]))
			}
			newCentroids[c] = normalize(vec)
		}
		centroids = newCentroids
		if !changed && iter > 0 {
			break
		}
	}

	var inertia float64
	for i, p := range points {
		inertia += sqDist(p, centroids[assignments[i]])
	}
	return Result{K: k, Assignments: assignments, Centroids: centroids, Inertia: inertia}
}

// PickK tries each candidate k on a sample (for speed), scores it with an
// approximate silhouette, and re-runs the best k on the full dataset.
func PickK(points [][]float32, candidates []int, sampleSize int, seed int64, maxIter int) Result {
	if len(points) == 0 || len(candidates) == 0 {
		return Result{}
	}
	sample := points
	if sampleSize > 0 && len(points) > sampleSize {
		rng := rand.New(rand.NewSource(seed))
		idx := rng.Perm(len(points))[:sampleSize]
		sample = make([][]float32, sampleSize)
		for i, ix := range idx {
			sample[i] = points[ix]
		}
	}

	bestScore := math.Inf(-1)
	bestK := 0
	for _, k := range candidates {
		if k < 2 || k > len(sample) {
			continue
		}
		res := Run(sample, k, seed, maxIter)
		if score := avgSilhouette(sample, res); score > bestScore {
			bestScore, bestK = score, k
		}
	}
	if bestK == 0 {
		bestK = candidates[0]
		if bestK > len(points) {
			bestK = len(points)
		}
	}
	return Run(points, bestK, seed, maxIter)
}

func initPlusPlus(points [][]float32, k int, rng *rand.Rand) [][]float32 {
	n := len(points)
	centroids := make([][]float32, 0, k)
	centroids = append(centroids, cloneVec(points[rng.Intn(n)]))
	dist := make([]float64, n)

	for len(centroids) < k {
		var total float64
		for i, p := range points {
			d := math.MaxFloat64
			for _, c := range centroids {
				if dd := sqDist(p, c); dd < d {
					d = dd
				}
			}
			dist[i] = d
			total += d
		}
		if total == 0 {
			centroids = append(centroids, cloneVec(points[rng.Intn(n)]))
			continue
		}
		r := rng.Float64() * total
		var cum float64
		for i, d := range dist {
			cum += d
			if cum >= r {
				centroids = append(centroids, cloneVec(points[i]))
				break
			}
		}
	}
	return centroids
}

// avgSilhouette approximates the silhouette score using only distance to the
// assigned vs. next-nearest centroid (not full pairwise distances), which is
// enough to rank candidate k values cheaply.
func avgSilhouette(points [][]float32, res Result) float64 {
	if res.K < 2 {
		return 0
	}
	var total float64
	for i, p := range points {
		a := sqDist(p, res.Centroids[res.Assignments[i]])
		b := math.MaxFloat64
		for c, centroid := range res.Centroids {
			if c == res.Assignments[i] {
				continue
			}
			if d := sqDist(p, centroid); d < b {
				b = d
			}
		}
		m := math.Max(a, b)
		if m == 0 {
			continue
		}
		total += (b - a) / m
	}
	return total / float64(len(points))
}

func sqDist(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum
}

func normalize(v []float32) []float32 {
	var sumSq float64
	for _, f := range v {
		sumSq += float64(f) * float64(f)
	}
	if sumSq == 0 {
		return v
	}
	norm := float32(math.Sqrt(sumSq))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f / norm
	}
	return out
}

func cloneVec(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	return out
}
