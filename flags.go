package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// crowdThresholds is how many distinct visitors must flag a block before the
// flag takes effect. Flags not listed here (e.g. "duplicate") are only
// recorded. A request carrying the admin password (FLAG_PASSWORD) applies
// its flag immediately.
var crowdThresholds = map[string]int64{
	"cool":   2, // add to the home-page clusters
	"nsfw":   2, // hide everywhere
	"boring": 3, // remove from the home-page clusters
}

type FlagData struct {
	Id   int    `json:"id"`
	Flag string `json:"flag"`
	Pw   string `json:"pw"`
}

// clientIP returns the visitor's IP. Behind nginx every connection comes from
// loopback and nginx sets X-Real-IP to the real peer, so the header is trusted
// only on loopback connections; anyone else gets their socket address.
// (echo v4.10's ExtractIPFromRealIPHeader checks the header value instead of
// the peer, which collapses every proxied visitor to 127.0.0.1.)
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := strings.TrimSpace(req.Header.Get(echo.HeaderXRealIP)); real != "" {
			return real
		}
	}
	return host
}

// voterID is a stable pseudonymous id for a visitor, used to count distinct
// flaggers without storing raw IPs.
func voterID(ip string) string {
	sum := sha256.Sum256([]byte("river-flag-voter:" + ip))
	return hex.EncodeToString(sum[:8])
}

func (s *Server) handleFlag(c echo.Context) error {
	var flagData FlagData
	if err := c.Bind(&flagData); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid flag data")
	}

	// The admin password only counts when FLAG_PASSWORD is configured; an
	// unset password must not match the empty pw that ordinary visitors send.
	password := os.Getenv("FLAG_PASSWORD")
	isAdmin := password != "" && subtle.ConstantTimeCompare([]byte(flagData.Pw), []byte(password)) == 1

	voter := voterID(c.RealIP())
	if err := s.db.Exec("INSERT INTO flags (block_id, flag, voter) VALUES (?, ?, ?)", flagData.Id, flagData.Flag, voter).Error; err != nil {
		log.Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
	}

	apply := isAdmin
	if threshold, ok := crowdThresholds[flagData.Flag]; ok && !apply {
		var voters int64
		err := s.db.Raw(
			"SELECT count(DISTINCT voter) FROM flags WHERE block_id = ? AND flag = ? AND voter IS NOT NULL",
			flagData.Id, flagData.Flag,
		).Scan(&voters).Error
		if err != nil {
			log.Error(err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
		}
		apply = voters >= threshold
	}

	if apply {
		if err := s.applyFlag(flagData.Flag, flagData.Id); err != nil {
			log.Error(err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
		}
		if err := s.refreshRandomSeeds(); err != nil {
			log.Error(err)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{
		"id":   strconv.Itoa(flagData.Id),
		"flag": flagData.Flag,
	})
}

// applyFlag performs a flag's moderation action. Each action is idempotent,
// so votes past the threshold are harmless.
func (s *Server) applyFlag(flag string, blockID int) error {
	switch flag {
	case "boring":
		return s.db.Exec("DELETE FROM clusters WHERE cluster_id IN (SELECT cluster_id FROM clusters WHERE block_id = ?)", blockID).Error
	case "nsfw":
		return s.db.Exec("UPDATE blocks SET nsfw = true WHERE id = ?", blockID).Error
	case "cool":
		return s.db.Exec(`
			INSERT INTO clusters (cluster_id, block_id)
			SELECT COALESCE((SELECT MAX(cluster_id) FROM clusters), 0) + 1, ?
			WHERE NOT EXISTS (SELECT 1 FROM clusters WHERE block_id = ?)
		`, blockID, blockID).Error
	}
	return nil
}
