package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	cliutil "github.com/bluesky-social/indigo/util/cliutil"
	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	cli "github.com/urfave/cli/v2"
	"golang.org/x/crypto/acme/autocert"
	gorm "gorm.io/gorm"
)

var log = logging.Logger("river")

func main() {

	app := cli.NewApp()

	app.Flags = []cli.Flag{}
	app.Commands = []*cli.Command{
		runCmd,
	}

	app.RunAndExitOnError()
}

type Server struct {
	db       *gorm.DB
	seeds    randomSeeds
	embedder *embedder // nil when EMBED_URL is unset; search is then disabled
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

const defaultOgImage = "https://river.maxbittker.com/river_og.png"

type PageData struct {
	OgImage string
}

var runCmd = &cli.Command{
	Name: "run",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Value:   "sqlite://data/algoz.db",
			EnvVars: []string{"DATABASE_URL"},
		},

		&cli.StringFlag{
			Name: "auto-tls-domain",
		},
		&cli.StringFlag{
			Name: "listen",
			// Browsers refuse to connect to :6000 (reserved for X11), so pick
			// another port, e.g. :8787, when hitting the API from a local frontend.
			Value:   ":6000",
			EnvVars: []string{"LISTEN_ADDR"},
		},
		&cli.StringFlag{
			Name:    "embed-url",
			Usage:   "base URL of the search encoder (embed/serve.py); enables search",
			EnvVars: []string{"EMBED_URL"},
		},
		&cli.StringFlag{
			Name:    "embed-token",
			Usage:   "optional bearer token for the search encoder",
			EnvVars: []string{"EMBED_TOKEN"},
		},
	},
	Action: func(cctx *cli.Context) error {

		log.Info("Connecting to database")
		db, err := cliutil.SetupDatabase(cctx.String("database-url"), 40)
		if err != nil {
			return err
		}
		if embeddingDim, err = loadEmbeddingDim(db); err != nil {
			return fmt.Errorf("reading embedding dimension: %w", err)
		}
		log.Infof("blocks.embedding is %d-dimensional", embeddingDim)

		log.Infof("Configuring HTTP server")
		// To initialize Sentry's handler, you need to initialize Sentry itself beforehand
		if err := sentry.Init(sentry.ClientOptions{
			EnableTracing: true,

			Dsn: "https://4c3eb0efa5f720cac18d362052b4fd12@o40136.ingest.sentry.io/4505718108717056",
			// Set TracesSampleRate to 1.0 to capture 100%
			// of transactions for performance monitoring.
			// We recommend adjusting this value in production,
			TracesSampleRate: 1.0,
		}); err != nil {
			fmt.Printf("Sentry initialization failed: %v		", err)
		}

		e := echo.New()
		e.IPExtractor = clientIP
		e.Use(middleware.Logger())
		e.Use(middleware.Recover())
		e.Use(middleware.CORS())
		e.Static("/", "site/dist")

		e.Renderer = &Template{
			templates: template.Must(template.ParseGlob("site/dist/*.html")),
		}
		e.GET("/", func(c echo.Context) error {
			ogImage := defaultOgImage
			if idInt, err := strconv.Atoi(c.QueryParam("id")); err == nil {
				var display *string
				err := db.Raw(`SELECT display FROM blocks WHERE id = ? LIMIT 1`, idInt).Scan(&display).Error
				if err != nil {
					log.Error(err)
				} else if display != nil && *display != "" {
					ogImage = *display
				}
			}

			return c.Render(http.StatusOK, "index.html", &PageData{OgImage: ogImage})
		})

		e.Use(sentryecho.New(sentryecho.Options{}))

		s := &Server{
			db:       db,
			embedder: newEmbedder(cctx.String("embed-url"), cctx.String("embed-token")),
		}
		if s.embedder == nil {
			log.Warn("EMBED_URL not set; search is disabled")
		}

		e.GET("/neighbors", s.handleGetNeighbors)
		e.POST("/vectorNeighbors", s.handleVectorNeighbors)
		e.POST("/embed", s.handleEmbed)
		e.POST("/api/flag", s.handleFlag)
		atd := cctx.String("auto-tls-domain")
		if atd != "" {
			cachedir, err := os.UserCacheDir()
			if err != nil {
				return err
			}

			e.AutoTLSManager.HostPolicy = autocert.HostWhitelist(atd)
			// Cache certificates to avoid issues with rate limits (https://letsencrypt.org/docs/rate-limits)
			e.AutoTLSManager.Cache = autocert.DirCache(filepath.Join(cachedir, "certs"))
		}
		go func() {

			port := cctx.String("listen")

			if atd != "" {
				panic(e.StartAutoTLS(port))
			} else {
				panic(e.Start(port))
			}
		}()

		if err := s.refreshRandomSeeds(); err != nil {
			log.Error(err)
		}
		select {}

	},
}

func (s *Server) handleGetNeighbors(e echo.Context) error {

	blockId := e.QueryParam("id")
	var limit, offset int
	if err := echo.QueryParamsBinder(e).Int("limit", &limit).Int("offset", &offset).BindError(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "limit and offset must be integers")
	}
	limit, offset = clampPage(limit, offset)

	if blockId == "audit" {
		images, err := s.clusterHeads(limit, offset)
		if err != nil {
			log.Error(err)
			return echo.NewHTTPError(http.StatusInternalServerError, "audit failed")
		}
		return e.JSON(http.StatusOK, neighborsResults{Images: images, NextOffset: offset + limit})
	}

	var images []imageMeta
	// Neighbor offsets count result ranks (MMR scales them into its pool
	// internally); the home page's offset is a page number.
	nextOffset := offset + limit
	if blockId == "" || blockId == "null" || blockId == "undefined" {
		nextOffset = offset + 1
		// Home page: blend neighbors of three cached seed blocks, rotating
		// through the seed list as offset (the page number) grows.
		seeds := s.seeds.get()
		if len(seeds) == 0 {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "random seeds not loaded")
		}
		for i := 0; i < 3; i++ {
			seed := seeds[((offset*3)+i)%len(seeds)].Id
			miniOffset := 10 * (offset / len(seeds))
			batch, err := s.nearestBlocks(embeddingOfBlock, seed, limit/3, miniOffset)
			if err != nil {
				log.Error(err)
				return echo.NewHTTPError(http.StatusInternalServerError, "neighbors failed")
			}
			images = append(images, batch...)
		}
	} else {
		// ?mmr=<lambda> overrides the diversity weight for tuning; 1 is plain kNN.
		lambda := float32(mmrDefaultLambda)
		if v := e.QueryParam("mmr"); v != "" {
			f, err := strconv.ParseFloat(v, 32)
			if err != nil || f < 0 || f > 1 {
				return echo.NewHTTPError(http.StatusBadRequest, "mmr must be between 0 and 1")
			}
			lambda = float32(f)
		}
		var err error
		images, err = s.diverseNeighbors(blockId, limit, offset, lambda)
		if err != nil {
			log.Error(err)
			return echo.NewHTTPError(http.StatusInternalServerError, "neighbors failed")
		}
	}

	return e.JSON(http.StatusOK, neighborsResults{Images: dedupeByPhash(images), NextOffset: nextOffset})
}

const (
	defaultPageSize = 35
	// maxPageSize bounds a single request; the site asks for at most 6*9.
	maxPageSize = 200
)

// clampPage applies the default page size to a missing/non-positive limit,
// caps it at maxPageSize, and floors offset at zero.
func clampPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

type VectorSearchRequest struct {
	Vector []float64 `json:"vector"`
	Limit  int       `json:"limit,omitempty"`
	Offset int       `json:"offset,omitempty"`
}

func (s *Server) handleVectorNeighbors(c echo.Context) error {
	var req VectorSearchRequest

	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid vector search request")
	}

	if len(req.Vector) != embeddingDim {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Vector must be %d dimensional", embeddingDim))
	}

	limit, offset := clampPage(req.Limit, req.Offset)

	images, err := s.nearestBlocks(literalVector, vectorLiteral(req.Vector), limit, offset)
	if err != nil {
		log.Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "vector neighbors search failed")
	}

	return c.JSON(http.StatusOK, neighborsResults{Images: dedupeByPhash(images), NextOffset: offset + limit})
}
