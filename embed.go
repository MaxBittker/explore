package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	// maxQueryImageBytes caps an uploaded search image. The site shrinks
	// images to ~512px before upload, so real requests are far smaller.
	maxQueryImageBytes = 8 << 20
	maxQueryTextLen    = 500
	// On the 4-core VPS SigLIP 2 base takes ~0.1s per text query and ~0.5s
	// per image (so400m: ~1.5s and ~10s). Requests are served one at a time,
	// so leave room for a queue.
	embedTimeout      = 30 * time.Second
	textCacheCapacity = 5000
)

// embedder turns search text or images into vectors by calling the local
// encoder service, embed/serve.py (EMBED_URL, e.g. http://127.0.0.1:6100).
type embedder struct {
	url    string
	token  string
	client *http.Client

	mu    sync.Mutex
	texts map[string][]float64
}

func newEmbedder(url, token string) *embedder {
	if url == "" {
		return nil
	}
	return &embedder{
		url:    strings.TrimRight(url, "/") + "/embed",
		token:  token,
		client: &http.Client{Timeout: embedTimeout},
		texts:  make(map[string][]float64),
	}
}

func (e *embedder) embedText(ctx context.Context, text string) ([]float64, error) {
	key := strings.ToLower(text) // the model lowercases text anyway
	e.mu.Lock()
	v, ok := e.texts[key]
	e.mu.Unlock()
	if ok {
		return v, nil
	}
	v, err := e.call(ctx, map[string]string{"text": text})
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if len(e.texts) >= textCacheCapacity {
		e.texts = make(map[string][]float64)
	}
	e.texts[key] = v
	e.mu.Unlock()
	return v, nil
}

func (e *embedder) embedImage(ctx context.Context, img []byte) ([]float64, error) {
	return e.call(ctx, map[string]string{"image": base64.StdEncoding.EncodeToString(img)})
}

// errBadInput means the endpoint rejected the query itself (e.g. an
// unreadable image), as opposed to being unavailable.
var errBadInput = errors.New("embed endpoint rejected input")

func (e *embedder) call(ctx context.Context, body map[string]string) ([]float64, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge {
		return nil, errBadInput
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embed endpoint: %s: %s", resp.Status, msg)
	}
	var out struct {
		Vector []float64 `json:"vector"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Vector) != embeddingDim {
		return nil, fmt.Errorf("embed endpoint returned %d dims, want %d", len(out.Vector), embeddingDim)
	}
	return out.Vector, nil
}

type embedResponse struct {
	Vector []float64 `json:"vector"`
}

// handleEmbed embeds a search query: a multipart form with either a "text"
// field or an "image" file. The site then pages through results by posting
// the vector to /vectorNeighbors.
func (s *Server) handleEmbed(c echo.Context) error {
	if s.embedder == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "search is not configured")
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxQueryImageBytes+64<<10)
	ctx := c.Request().Context()

	var v []float64
	var err error
	if text := strings.TrimSpace(c.FormValue("text")); text != "" {
		if len(text) > maxQueryTextLen {
			text = text[:maxQueryTextLen]
		}
		v, err = s.embedder.embedText(ctx, text)
	} else if fh, ferr := c.FormFile("image"); ferr == nil {
		if fh.Size > maxQueryImageBytes {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "image too large")
		}
		f, oerr := fh.Open()
		if oerr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "unreadable upload")
		}
		img, rerr := io.ReadAll(f)
		f.Close()
		if rerr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "unreadable upload")
		}
		v, err = s.embedder.embedImage(ctx, img)
	} else {
		var tooBig *http.MaxBytesError
		if errors.As(ferr, &tooBig) {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "image too large")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "send text or an image")
	}

	if errors.Is(err, errBadInput) {
		return echo.NewHTTPError(http.StatusBadRequest, "couldn't read that image")
	}
	if err != nil {
		log.Error(err)
		return echo.NewHTTPError(http.StatusBadGateway, "search embedding failed")
	}
	return c.JSON(http.StatusOK, embedResponse{Vector: v})
}
