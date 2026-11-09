package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Grid engagement: the site beacons the ids of blocks that were on screen
// (impressions) and clicked, batched every few seconds. Counts accumulate in
// memory and are added to block_stats in one upsert per statsFlushInterval,
// so the database sees one statement a minute however busy the site is.
// Blocks themselves are never updated (that would reinsert rows into HNSW).
const (
	statsFlushInterval = time.Minute
	// maxStatIds bounds one beacon; the site sends at most ~200.
	maxStatIds  = 1000
	maxStatBody = 32 << 10
)

type statCounts struct{ impressions, clicks int32 }

type blockStats struct {
	mu      sync.Mutex
	pending map[int32]statCounts
}

func newBlockStats() *blockStats {
	return &blockStats{pending: map[int32]statCounts{}}
}

type statsBeacon struct {
	Impressions []int32 `json:"i"`
	Clicks      []int32 `json:"c"`
}

// handleStats accepts a beacon. navigator.sendBeacon posts text/plain (no CORS
// preflight), so the body is decoded as JSON regardless of content type.
func (s *Server) handleStats(c echo.Context) error {
	var b statsBeacon
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxStatBody)
	if err := json.NewDecoder(body).Decode(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid stats")
	}
	if len(b.Impressions)+len(b.Clicks) > maxStatIds {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "too many ids")
	}
	s.stats.add(b)
	return c.NoContent(http.StatusNoContent)
}

// add counts each id at most once per list, so one beacon can't inflate a
// block by repeating it.
func (st *blockStats) add(b statsBeacon) {
	seen := make(map[int32]uint8, len(b.Impressions)+len(b.Clicks))
	for _, id := range b.Impressions {
		if id > 0 {
			seen[id] |= 1
		}
	}
	for _, id := range b.Clicks {
		if id > 0 {
			seen[id] |= 2
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, bits := range seen {
		n := st.pending[id]
		n.impressions += int32(bits & 1)
		n.clicks += int32(bits >> 1)
		st.pending[id] = n
	}
}

func (st *blockStats) merge(m map[int32]statCounts) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, c := range m {
		n := st.pending[id]
		n.impressions += c.impressions
		n.clicks += c.clicks
		st.pending[id] = n
	}
}

// flushStats writes pending counts to block_stats. On failure they're put
// back to retry on the next flush.
func (s *Server) flushStats() error {
	st := s.stats
	st.mu.Lock()
	batch := st.pending
	st.pending = map[int32]statCounts{}
	st.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}

	ids := make([]int32, 0, len(batch))
	for id := range batch {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	var idArr, impArr, clkArr strings.Builder
	for i, id := range ids {
		sep := ","
		if i == 0 {
			sep = "{"
		}
		idArr.WriteString(sep + strconv.Itoa(int(id)))
		impArr.WriteString(sep + strconv.Itoa(int(batch[id].impressions)))
		clkArr.WriteString(sep + strconv.Itoa(int(batch[id].clicks)))
	}
	idArr.WriteByte('}')
	impArr.WriteByte('}')
	clkArr.WriteByte('}')

	// Array literals rather than Go slices: gorm would expand a slice into a
	// parameter list. Ids that aren't blocks are dropped.
	err := s.db.Exec(`
		INSERT INTO block_stats (block_id, impressions, clicks)
		SELECT u.id, u.imp, u.clk
		FROM unnest(?::int[], ?::int[], ?::int[]) AS u(id, imp, clk)
		WHERE EXISTS (SELECT 1 FROM blocks WHERE blocks.id = u.id)
		ON CONFLICT (block_id) DO UPDATE SET
			impressions = block_stats.impressions + EXCLUDED.impressions,
			clicks = block_stats.clicks + EXCLUDED.clicks`,
		idArr.String(), impArr.String(), clkArr.String(),
	).Error
	if err != nil {
		st.merge(batch)
	}
	return err
}

func (s *Server) flushStatsLoop() {
	for range time.Tick(statsFlushInterval) {
		if err := s.flushStats(); err != nil {
			log.Errorf("flushing block stats: %v", err)
		}
	}
}
