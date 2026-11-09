package main

import (
	"math"
	"math/rand"
	"testing"
)

func unit(v []float32) []float32 {
	var n float32
	for _, x := range v {
		n += x * x
	}
	n = float32(math.Sqrt(float64(n)))
	for i := range v {
		v[i] /= n
	}
	return v
}

func TestMMRSkipsNearDuplicates(t *testing.T) {
	anchor := unit([]float32{1, 0, 0})
	mk := func(id int, v []float32) mmrCandidate {
		v = unit(v)
		return mmrCandidate{imageMeta: imageMeta{Id: id}, Sim: dot(anchor, v), vec: v}
	}
	// 1-3 form a tight near-duplicate clump; 4 and 5 are related but distinct.
	pool := []mmrCandidate{
		mk(1, []float32{1, 0.05, 0}),
		mk(2, []float32{1, 0.06, 0}),
		mk(3, []float32{1, 0.04, 0.01}),
		mk(4, []float32{0.8, 0.6, 0}),
		mk(5, []float32{0.8, -0.3, 0.5}),
	}
	got := mmrSelect(pool, 3, 0.3)
	clump := 0
	for _, g := range got {
		if g.Id <= 3 {
			clump++
		}
	}
	if got[0].Id != 3 || clump != 1 {
		t.Fatalf("picked %v; want the top clump member, then the two distinct candidates", got)
	}
	if plain := mmrSelect(pool, 3, 1); plain[0].Id != 3 || plain[1].Id != 1 || plain[2].Id != 2 {
		t.Fatalf("lambda=1 should be pure relevance, got %v", plain)
	}
}

func TestDecodeHalfvec(t *testing.T) {
	// Real row from river2m_base: dim 768 header, first value 0xa042.
	b := make([]byte, 4+2*768)
	b[0] = 0x03
	b[4], b[5] = 0xa0, 0x42
	b[6], b[7] = 0x3c, 0x00 // 1.0
	b[8], b[9] = 0x00, 0x01 // smallest subnormal
	v, err := decodeHalfvec(b)
	if err != nil || len(v) != 768 {
		t.Fatal(len(v), err)
	}
	if math.Abs(float64(v[0])+0.00831604) > 1e-7 || v[1] != 1 || v[2] != 1.0/(1<<24) || v[3] != 0 {
		t.Fatalf("got %v", v[:4])
	}
	if _, err := decodeHalfvec(b[:10]); err == nil {
		t.Fatal("want length error")
	}
}

func randPool(n, d int) []mmrCandidate {
	r := rand.New(rand.NewSource(1))
	pool := make([]mmrCandidate, n)
	for i := range pool {
		v := make([]float32, d)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		pool[i] = mmrCandidate{imageMeta: imageMeta{Id: i}, Sim: r.Float32(), vec: unit(v)}
	}
	return pool
}

// 9 columns: 54 picks from a 162 pool, 768-d.
func BenchmarkMMRSelect(b *testing.B) {
	pool := randPool(162, 768)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmrSelect(pool, 54, 0.7)
	}
}

func BenchmarkDecodePool(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	rows := make([][]byte, 162)
	for i := range rows {
		rows[i] = make([]byte, 4+2*768)
		r.Read(rows[i][4:])
		rows[i][0], rows[i][1] = 0x03, 0x00
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, row := range rows {
			decodeHalfvec(row)
		}
	}
}
