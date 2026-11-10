package main

// Backlinks make every block reachable by clicking alone.
//
// A block's page shows its nearest neighbors, but "nearest" isn't mutual: on
// 2026-11-09, with 18-image pages over 1.52M blocks, 11% of blocks were on no
// page that the rest of the graph leads to (7% were on nobody's page at all),
// and 8 tight clumps of near-identical images linked only to each other.
//
// The backlinks table repairs that with at most one row per host: the host's
// first page gives up its last slot to the listed block. An unreachable block
// goes on the page of its nearest neighbor that is already reachable, and a
// block in a dead-end clump gets its nearest block outside it. With those
// 172K rows the graph of 18-image first pages is strongly connected; larger
// pages only add links.
//
// The table is computed offline from the embeddings and goes stale as blocks
// are added or removed. Stale rows are harmless (a deleted block just doesn't
// join), but new unreachable blocks stay unreachable until it's rebuilt.

// backlink returns the block that must appear on anchor's first page, or nil
// if it has none.
func (s *Server) backlink(anchor string) (*imageMeta, error) {
	var images []imageMeta
	err := s.db.Raw(`
		SELECT `+imageColumns+`
		FROM backlinks
		JOIN blocks ON blocks.id = backlinks.block
		WHERE backlinks.host = ?
		and `+visibleBlocks+`
		LIMIT 1
	`, anchor).Scan(&images).Error
	if err != nil || len(images) == 0 {
		return nil, err
	}
	return &images[0], nil
}

// withBacklink puts link on a page of at most limit images, replacing the
// last (farthest) image if the page is full. The anchor, first, always stays.
func withBacklink(images []imageMeta, link imageMeta, limit int) []imageMeta {
	for _, img := range images {
		if img.Id == link.Id {
			return images
		}
	}
	if len(images) >= limit && len(images) > 1 {
		images = images[:len(images)-1]
	}
	return append(images, link)
}
