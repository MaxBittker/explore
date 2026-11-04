package main

import (
	"fmt"
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
}

// imageColumns is the column list scanned into imageMeta by every grid query.
const imageColumns = `blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height, blocks.phash as Phash`

// visibleBlocks filters out blocks that can't be shown in the grid.
const visibleBlocks = `blocks.thumb IS NOT NULL
		and blocks.embedding is not null
		and nsfw is not true`

// SQL expressions for the query vector passed to nearestBlocks.
const (
	embeddingOfBlock = `(SELECT embedding FROM blocks WHERE id = ?)`
	literalVector    = `?::vector`
)

// hnswEfSearch widens the HNSW candidate list so a page of results isn't
// truncated at pgvector's default of 40.
const hnswEfSearch = 70

// nearestBlocks returns visible blocks ordered by inner-product distance to
// the vector produced by target (embeddingOfBlock or literalVector).
func (s *Server) nearestBlocks(target string, targetArg any, limit, offset int) ([]imageMeta, error) {
	var images []imageMeta
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// SET LOCAL pins the setting to this transaction's connection; a bare
		// SET on the pool can land on a different connection than the query.
		// SET can't take bind parameters; hnswEfSearch is a constant.
		if err := tx.Exec(fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch)).Error; err != nil {
			return err
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
