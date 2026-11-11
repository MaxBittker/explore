package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"

	"github.com/labstack/echo/v4"
)

// The minimap is a fixed 2D layout (UMAP) of the collection, cut into k-means
// cells; minimap/build.py writes each cell's position and centroid to
// minimap/cells.bin. The page shows it through a fisheye lens centered on the
// current block or search, so the server's job is to say where that is.
//
// Positions never depend on the query: stepping to a similar image moves the
// lens a little and everything keeps its place. What a fixed layout of 768-d
// embeddings can't do is keep a neighborhood together (among a block's 40
// nearest cells, distance on the layout ranks them with a Spearman
// correlation of only 0.35 against true similarity, and other layouts of the
// cells score the same), so the server also returns the cells that really are
// nearest and the page inks those in wherever they lie.
type minimap struct {
	dim       int
	xy        [][2]float32 // cell position in the unit square, y down
	centroids [][]float32  // unit length
}

const minimapPath = "minimap/cells.bin"

// loadMinimap reads cells.bin: "RMAP1\0\0\0", uint32 cell count, uint32
// dimension, then count*2 float32 positions and count*dim int8 centroid
// components, little endian.
func loadMinimap(path string, wantDim int) (*minimap, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 16 || string(b[:5]) != "RMAP1" {
		return nil, fmt.Errorf("%s: not a minimap cells file", path)
	}
	n := int(binary.LittleEndian.Uint32(b[8:]))
	dim := int(binary.LittleEndian.Uint32(b[12:]))
	if dim != wantDim {
		return nil, fmt.Errorf("%s: centroids are %d-dimensional, blocks.embedding is %d", path, dim, wantDim)
	}
	if len(b) != 16+n*8+n*dim {
		return nil, fmt.Errorf("%s: wrong size for %d cells", path, n)
	}
	m := &minimap{dim: dim, xy: make([][2]float32, n), centroids: make([][]float32, n)}
	pos, cents := b[16:16+n*8], b[16+n*8:]
	for i := 0; i < n; i++ {
		m.xy[i][0] = math.Float32frombits(binary.LittleEndian.Uint32(pos[i*8:]))
		m.xy[i][1] = math.Float32frombits(binary.LittleEndian.Uint32(pos[i*8+4:]))
		c := make([]float32, dim)
		var norm float64
		for j := range c {
			c[j] = float32(int8(cents[i*dim+j]))
			norm += float64(c[j] * c[j])
		}
		scale := float32(1 / math.Sqrt(norm))
		for j := range c {
			c[j] *= scale
		}
		m.centroids[i] = c
	}
	return m, nil
}

const (
	// mapNear is how many of the truly nearest cells are reported.
	mapNear = 40
	// The query's position blends its mapBlend nearest cells, weighted by
	// exp(similarity / mapBlendTemp), so it moves a little between similar
	// images instead of sitting on one cell's dot. On 20k blocks this lands a
	// median 1.7% of the map width from the block's own layout position.
	mapBlend     = 4
	mapBlendTemp = 0.02
)

// mapPlace is where a block or vector is on the map.
type mapPlace struct {
	X    float32 `json:"x"`
	Y    float32 `json:"y"`
	Near []int   `json:"near"` // the mapNear most similar cells, nearest first
}

func (m *minimap) place(v []float32) mapPlace {
	n := len(m.centroids)
	sims := make([]float32, n)
	order := make([]int, n)
	for i, c := range m.centroids {
		sims[i] = dot(v, c)
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return sims[order[a]] > sims[order[b]] })
	// Similarities are compared on the unit sphere, whatever v's length.
	norm := math.Sqrt(float64(dot(v, v)))
	if norm == 0 {
		norm = 1
	}
	var x, y, total float64
	for j := 0; j < mapBlend && j < n; j++ {
		w := math.Exp(float64(sims[order[j]]-sims[order[0]]) / norm / mapBlendTemp)
		x += w * float64(m.xy[order[j]][0])
		y += w * float64(m.xy[order[j]][1])
		total += w
	}
	near := mapNear
	if near > n {
		near = n
	}
	return mapPlace{X: float32(x / total), Y: float32(y / total), Near: append([]int(nil), order[:near]...)}
}

// GET /api/map/place?id=<block> finds a block on the map.
func (s *Server) handleMapPlaceBlock(c echo.Context) error {
	if s.minimap == nil {
		return echo.NewHTTPError(http.StatusNotFound, "no minimap")
	}
	id, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "id must be an integer")
	}
	var raw []byte
	err = s.db.Raw(`SELECT halfvec_send(embedding) FROM blocks WHERE id = ? AND embedding IS NOT NULL`, id).Row().Scan(&raw)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "no such block")
	}
	vec, err := decodeHalfvec(raw)
	if err != nil || len(vec) != s.minimap.dim {
		return echo.NewHTTPError(http.StatusInternalServerError, "bad embedding")
	}
	// A block's place only changes when cells.bin does, so let browsers keep
	// it a while; not long, because a stale one would be drawn with the new
	// minimap.json.
	c.Response().Header().Set("Cache-Control", "public, max-age=3600")
	return c.JSON(http.StatusOK, s.minimap.place(vec))
}

// POST /api/map/place {vector} finds a search vector on the map.
func (s *Server) handleMapPlaceVector(c echo.Context) error {
	if s.minimap == nil {
		return echo.NewHTTPError(http.StatusNotFound, "no minimap")
	}
	var req VectorSearchRequest
	if err := c.Bind(&req); err != nil || len(req.Vector) != s.minimap.dim {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Vector must be %d dimensional", s.minimap.dim))
	}
	vec := make([]float32, len(req.Vector))
	for i, x := range req.Vector {
		vec[i] = float32(x)
	}
	return c.JSON(http.StatusOK, s.minimap.place(vec))
}

// mapJumpSpread is how many of a cell's most central blocks a jump picks
// among, so clicking the same spot twice doesn't always land on one image.
const mapJumpSpread = 40

// GET /api/map/jump?cell=<n> returns a block near the middle of a cell: where
// a click on the map lands.
func (s *Server) handleMapJump(c echo.Context) error {
	if s.minimap == nil {
		return echo.NewHTTPError(http.StatusNotFound, "no minimap")
	}
	cell, err := strconv.Atoi(c.QueryParam("cell"))
	if err != nil || cell < 0 || cell >= len(s.minimap.centroids) {
		return echo.NewHTTPError(http.StatusBadRequest, "no such cell")
	}
	cent := s.minimap.centroids[cell]
	vec := make([]float64, len(cent))
	for i, x := range cent {
		vec[i] = float64(x)
	}
	images, err := s.nearestBlocks(literalVector, vectorLiteral(vec), 1, rand.Intn(mapJumpSpread))
	if err != nil {
		log.Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "jump failed")
	}
	if len(images) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "nothing there")
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, images[0])
}
