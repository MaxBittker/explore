package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"

	gorm "gorm.io/gorm"
)

// Neighbor pages are diversified with maximal marginal relevance (MMR):
// fetch a pool of mmrPoolFactor*limit nearest candidates, then greedily pick
// the one maximizing
//
//	lambda*sim(anchor, c) - (1-lambda)*max sim(c, already picked)
//
// so near-duplicate clumps collapse to one or two representatives and the
// page spreads further out in dense regions. Sparse regions, where the pool
// is already varied, come out close to plain kNN.
//
// lambda 0.5 was tuned on river2m_base (SigLIP 2 base, 1.44M blocks): over
// 16-image first screens it cut pairs with cos >= 0.9 from 2.8 to 0.4 while
// mean similarity to the anchor only fell from 0.777 to 0.759, and 10-click
// random walks left their starting k=200 cluster 90% of the time vs 75%.
// Below 0.5 near-duplicates were already gone and walks spread no further.
//
// Pages tile the ranked list: the page at offset o (in returned images)
// draws from ranks [o*f, (o+limit)*f). Candidates not picked are skipped for
// good, so scrolling also moves outward f times faster than plain kNN.
//
// MMR is off by default (lambda 1 = plain kNN) because it costs ~100ms on the
// first page and ~3x on deep scrolls (2026-11-09 bench); ?mmr=0.5 opts in.
const (
	mmrPoolFactor    = 3
	mmrDefaultLambda = 1.0
)

type mmrCandidate struct {
	imageMeta
	Sim float32 // inner product with the anchor
	vec []float32
}

// mmrRow is one scanned candidate. It's flat because gorm's Raw().Scan
// leaves an embedded imageMeta's fields zero.
type mmrRow struct {
	Id        int
	Width     int
	Height    int
	Thumb     string
	Phash     int64
	Sim       float32
	Embedding []byte // halfvec_send output
}

// diverseNeighbors returns up to limit MMR-selected neighbors of blockId.
// lambda >= 1 means pure relevance, i.e. plain nearestBlocks.
func (s *Server) diverseNeighbors(blockId string, limit, offset int, lambda float32) ([]imageMeta, error) {
	if lambda >= 1 {
		return s.nearestBlocks(embeddingOfBlock, blockId, limit, offset)
	}
	var rows []mmrRow
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, set := range hnswSettings {
			if err := tx.Exec(set).Error; err != nil {
				return err
			}
		}
		// The anchor subquery is inlined twice; the planner evaluates each
		// once as an InitPlan.
		return tx.Raw(`
			SELECT `+imageColumns+`,
				-(blocks.embedding <#> `+embeddingOfBlock+`) as Sim,
				halfvec_send(blocks.embedding) as Embedding
			FROM blocks
			WHERE `+visibleBlocks+`
			ORDER BY blocks.embedding <#> `+embeddingOfBlock+`
			LIMIT ?
			OFFSET ?
		`, blockId, blockId, limit*mmrPoolFactor, offset*mmrPoolFactor).Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	pool := make([]mmrCandidate, len(rows))
	for i, r := range rows {
		vec, err := decodeHalfvec(r.Embedding)
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", r.Id, err)
		}
		pool[i] = mmrCandidate{
			imageMeta: imageMeta{Id: r.Id, Width: r.Width, Height: r.Height, Thumb: r.Thumb, Phash: r.Phash},
			Sim:       r.Sim,
			vec:       vec,
		}
	}
	pool = dedupePoolByPhash(pool)
	// The clicked block comes back as its own nearest neighbor. Keep it at the
	// front as plain kNN would, but leave it out of the MMR pool: everything
	// is at least as similar to the anchor as it is relevant, so penalizing
	// against it would never demote the anchor's near-duplicates.
	var out []imageMeta
	anchorId, _ := strconv.Atoi(blockId)
	for i, c := range pool {
		if c.Id == anchorId {
			out = append(out, c.imageMeta)
			pool = append(pool[:i], pool[i+1:]...)
			break
		}
	}
	return append(out, mmrSelect(pool, limit-len(out), lambda)...), nil
}

// mmrSelect greedily picks up to k candidates by MMR score.
func mmrSelect(pool []mmrCandidate, k int, lambda float32) []imageMeta {
	if k < 0 {
		k = 0
	}
	if k > len(pool) {
		k = len(pool)
	}
	out := make([]imageMeta, 0, k)
	picked := make([]bool, len(pool))
	// maxSim[i] is candidate i's highest similarity to anything picked so far.
	maxSim := make([]float32, len(pool))
	for i := range maxSim {
		maxSim[i] = -1
	}
	for len(out) < k {
		best, bestScore := -1, float32(0)
		for i := range pool {
			if picked[i] {
				continue
			}
			score := lambda * pool[i].Sim
			if len(out) > 0 {
				score -= (1 - lambda) * maxSim[i]
			}
			if best < 0 || score > bestScore {
				best, bestScore = i, score
			}
		}
		picked[best] = true
		out = append(out, pool[best].imageMeta)
		bv := pool[best].vec
		for i := range pool {
			if !picked[i] {
				if d := dot(pool[i].vec, bv); d > maxSim[i] {
					maxSim[i] = d
				}
			}
		}
	}
	return out
}

func dot(a, b []float32) float32 {
	var s0, s1, s2, s3 float32
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for ; i+4 <= n; i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < n; i++ {
		s0 += a[i] * b[i]
	}
	return s0 + s1 + s2 + s3
}

// decodeHalfvec decodes pgvector's binary halfvec (halfvec_send): int16
// dim, int16 unused, then dim big-endian IEEE half floats. Binary is ~4x
// smaller on the wire than the text form and much cheaper to decode.
func decodeHalfvec(b []byte) ([]float32, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("short halfvec")
	}
	dim := int(binary.BigEndian.Uint16(b))
	if len(b) != 4+2*dim {
		return nil, fmt.Errorf("halfvec of dim %d has %d bytes", dim, len(b))
	}
	out := make([]float32, dim)
	for i := range out {
		out[i] = halfToFloat(binary.BigEndian.Uint16(b[4+2*i:]))
	}
	return out, nil
}

func halfToFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch {
	case exp == 0x1f: // inf / NaN
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	case exp != 0: // normal
		return math.Float32frombits(sign | (exp+112)<<23 | mant<<13)
	case mant == 0: // signed zero
		return math.Float32frombits(sign)
	default: // subnormal: mant * 2^-24
		f := float32(mant) * (1.0 / (1 << 24))
		if sign != 0 {
			f = -f
		}
		return f
	}
}

// dedupePoolByPhash drops later candidates that share a phash with an
// earlier one, like dedupeByPhash does for plain pages.
func dedupePoolByPhash(pool []mmrCandidate) []mmrCandidate {
	seen := make(map[int64]bool)
	out := pool[:0]
	for _, c := range pool {
		if c.Phash != 0 {
			if seen[c.Phash] {
				continue
			}
			seen[c.Phash] = true
		}
		out = append(out, c)
	}
	return out
}
