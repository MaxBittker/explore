package main

import (
	"fmt"
	"strconv"
	"sync"

	gorm "gorm.io/gorm"
)

type imageMeta struct {
	Id     int
	Width  int
	Height int
	Thumb  string
	Phash  int64 `json:"phash,omitempty"`
}

type neighborsResults struct {
	Images []imageMeta `json:"images"`
	// NextOffset is the offset to request for the following page. Pages are
	// deduped after the fact, so the client can't derive it from how many
	// images it received without drifting back over pages it already has.
	NextOffset int `json:"nextOffset"`
}

// imageColumns is the column list scanned into imageMeta by every grid query.
// imageColumns: 229K backlog blocks merged 2026-11-09 have null width/height in blocks;
// their measured dims live in block_dims (updating blocks would reinsert each row into HNSW).
// A zero or null dimension crashes the frontend masonry layout, hence the final fallback of 1.
const imageColumns = `blocks.id as Id, blocks.thumb as Thumb, COALESCE(NULLIF(blocks.width, 0), (SELECT w FROM block_dims d WHERE d.id = blocks.id), 1) as Width, COALESCE(NULLIF(blocks.height, 0), (SELECT h FROM block_dims d WHERE d.id = blocks.id), 1) as Height, blocks.phash as Phash`

// visibleBlocks filters out blocks that can't be shown in the grid.
const visibleBlocks = `blocks.thumb IS NOT NULL
		and blocks.embedding is not null
		and nsfw is not true`

// embeddingDim is the size of blocks.embedding (halfvec(N)), read from the
// database at startup so one binary serves either encoder's DB: SigLIP 2
// so400m (1152) or base (768). The search encoder (EMBED_URL) must match it.
var embeddingDim int

// loadEmbeddingDim reads N from the halfvec(N) type of blocks.embedding.
func loadEmbeddingDim(db *gorm.DB) (int, error) {
	var dim int
	err := db.Raw(`SELECT atttypmod FROM pg_attribute
		WHERE attrelid = 'blocks'::regclass AND attname = 'embedding'`).Scan(&dim).Error
	if err == nil && dim <= 0 {
		err = fmt.Errorf("blocks.embedding has no fixed dimension")
	}
	return dim, err
}

// vectorLiteral formats v as a pgvector text literal ("[1,2,3]"). Postgres
// rounds it to half precision when it's cast to halfvec.
func vectorLiteral(v []float64) string {
	buf := make([]byte, 0, len(v)*12)
	buf = append(buf, '[')
	for i, x := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = strconv.AppendFloat(buf, x, 'g', -1, 32)
	}
	return string(append(buf, ']'))
}

// SQL expressions for the query vector passed to nearestBlocks.
const (
	embeddingOfBlock = `(SELECT embedding FROM blocks WHERE id = ?)`
	literalVector    = `?::halfvec`
)

// hnswEfSearch is the HNSW candidate list size (pgvector's default is 40).
// 2026-11-07 on river2m_base (1.44M x halfvec(768)), cold per query: ef 100 = 28ms,
// recall 0.969 vs ef 1000; ef 200 = 39ms, 0.978.
const hnswEfSearch = 100

// hnswSettings apply to every nearest-neighbor query. Iterative scans
// (pgvector 0.8+) keep walking the graph after the first ef_search candidates
// run out. Without them, a filtered query or a deep OFFSET returns a short or
// empty page. strict_order keeps results in exact distance order, so pages
// stay consistent.
var hnswSettings = []string{
	fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch),
	"SET LOCAL hnsw.iterative_scan = strict_order",
}

// nearestBlocks returns visible blocks ordered by inner-product distance to
// the vector produced by target (embeddingOfBlock or literalVector).
func (s *Server) nearestBlocks(target string, targetArg any, limit, offset int) ([]imageMeta, error) {
	var images []imageMeta
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// SET LOCAL pins the setting to this transaction's connection; a bare
		// SET on the pool can land on a different connection than the query.
		// SET can't take bind parameters, so these are constants.
		for _, set := range hnswSettings {
			if err := tx.Exec(set).Error; err != nil {
				return err
			}
		}
		return tx.Raw(`
			SELECT `+imageColumns+`
			FROM blocks
			WHERE `+visibleBlocks+`
			ORDER BY blocks.embedding <#> `+target+`
			LIMIT ?
			OFFSET ?
		`, targetArg, limit, offset).Scan(&images).Error
	})
	return images, err
}

// clusterHeads returns one representative block per curated cluster, used by
// the hidden ?id=audit view.
func (s *Server) clusterHeads(limit, offset int) ([]imageMeta, error) {
	var images []imageMeta
	err := s.db.Raw(`
		SELECT `+imageColumns+`
		FROM blocks
		WHERE `+visibleBlocks+`
		and blocks.id in (
			select max(block_id) from clusters group by cluster_id
		)
		LIMIT ?
		OFFSET ?
	`, limit, offset).Scan(&images).Error
	return images, err
}

// randomSeeds caches the newest cluster heads that seed the home-page grid.
type randomSeeds struct {
	mu     sync.RWMutex
	images []imageMeta
}

func (r *randomSeeds) get() []imageMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.images
}

func (s *Server) refreshRandomSeeds() error {
	var images []imageMeta
	err := s.db.Raw(`
		SELECT ` + imageColumns + `
		FROM blocks
		join clusters on blocks.id = clusters.block_id
		WHERE ` + visibleBlocks + `
		and blocks.id in (
			select max(block_id) from clusters group by cluster_id
		)
		order by clusters.id desc
		LIMIT 500
	`).Scan(&images).Error
	if err != nil {
		return err
	}
	s.seeds.mu.Lock()
	s.seeds.images = images
	s.seeds.mu.Unlock()
	return nil
}

// dedupeByPhash drops images whose perceptual hash was already seen, keeping
// the first (closest) occurrence. Images without a phash are always kept.
func dedupeByPhash(images []imageMeta) []imageMeta {
	seen := make(map[int64]bool)
	var out []imageMeta
	for _, img := range images {
		if img.Phash != 0 {
			if seen[img.Phash] {
				continue
			}
			seen[img.Phash] = true
		}
		out = append(out, img)
	}
	if len(out) == 0 {
		return images
	}
	return out
}
