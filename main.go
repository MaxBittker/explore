package main

import (
	"crypto/subtle"
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
	"golang.org/x/exp/slices"
	gorm "gorm.io/gorm"
)

var log = logging.Logger("algoz")

func main() {

	app := cli.NewApp()

	app.Flags = []cli.Flag{}
	app.Commands = []*cli.Command{
		runCmd,
	}

	app.RunAndExitOnError()
}

type imageMeta struct {
	Id     int
	Width  int
	Height int
	Thumb  string
	Phash  int64 `json:"phash,omitempty"`
}
type Server struct {
	db           *gorm.DB
	cachedRandom []imageMeta
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

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
	},
	Action: func(cctx *cli.Context) error {

		log.Info("Connecting to database")
		db, err := cliutil.SetupDatabase(cctx.String("database-url"), 40)
		if err != nil {
			return err
		}

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
		e.Use(middleware.Logger())
		e.Use(middleware.Recover())
		e.Use(middleware.CORS())
		e.Static("/", "site/dist")

		e.Renderer = &Template{
			templates: template.Must(template.ParseGlob("site/dist/*.html")),
		}
		e.GET("/", func(c echo.Context) error {
			var ogImage string
			query := c.Request().URL.Query()
			if query.Get("id") != "" {
				id := query.Get("id")

				// parse id to number:
				idInt, err := strconv.Atoi(id)
				if err != nil {
					ogImage = "https://river.maxbittker.com/river_og.png"
				} else {
					var image *imageMeta

					// Parameterized query to prevent SQL injection
					err := db.Raw(`SELECT blocks.display as Thumb FROM blocks WHERE id = ? LIMIT 1`, idInt).Scan(&image).Error

					if err != nil {
						ogImage = "https://river.maxbittker.com/river_og.png"
					}

					ogImage = image.Thumb
				}

			} else {
				ogImage = "https://river.maxbittker.com/river_og.png"
			}

			data := &PageData{
				OgImage: ogImage,
			}

			return c.Render(http.StatusOK, "index.html", data)
		})

		e.Use(sentryecho.New(sentryecho.Options{}))

		s := &Server{
			db: db,
		}

		e.GET("/neighbors", s.handleGetNeighbors)
		e.POST("/vectorNeighbors", s.handleVectorNeighbors)
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

			var port = ":6000"

			if atd != "" {
				panic(e.StartAutoTLS(port))
			} else {
				panic(e.Start(port))
			}
		}()

		err = s.db.Debug().Raw(
			fmt.Sprintf(
				`
				SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height 
				FROM blocks
				join clusters on blocks.id = clusters.block_id
				WHERE blocks.thumb IS NOT NULL
				and blocks.embedding is not null
				and nsfw is not true
				and blocks.id in (
					select max(block_id) from clusters group by cluster_id
				)
				order by clusters.id desc
				LIMIT %d
			`, 500)).Scan(&s.cachedRandom).Error

		if err != nil {
			log.Error(err)
		}
		select {}

	},
}

func (s *Server) handleGetNeighbors(e echo.Context) error {

	blockId := e.QueryParam("id")
	var limit int = 35
	if lims := e.QueryParam("limit"); lims != "" {
		v, err := strconv.Atoi(lims)
		if err != nil {
			return err
		}
		limit = v
	}
	var offset int = 0
	if off := e.QueryParam("offset"); off != "" {
		v, err := strconv.Atoi(off)
		if err != nil {
			return err
		}
		offset = v
	}

	var err error

	var images []imageMeta

	s.db.Exec("SET hnsw.ef_search = 70;")
	if blockId == "audit" {
		shouldReturn, returnValue := audit(s, limit, offset, &images)
		if shouldReturn {
			return returnValue
		}
		type neighborsResults struct {
			Images []imageMeta `json:"images"`
		}

		return e.JSON(200, neighborsResults{
			Images: images,
		})

	}

	if blockId == "" || blockId == "null" || blockId == "undefined" {

		var images2 []imageMeta

		for i := 0; i < 3; i++ {
			blockId = strconv.Itoa(s.cachedRandom[((offset*3)+i)%len(s.cachedRandom)].Id)
			miniOffset := int(10 * int(offset/len(s.cachedRandom)))
			miniLimit := int(limit / 3)
			err = s.db.Raw(
				`
				SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height, blocks.phash as Hash
				FROM blocks
				WHERE blocks.thumb IS NOT NULL
				and blocks.embedding is not null
				and nsfw is not true
				ORDER BY blocks.embedding <#> 
					(SELECT embedding FROM blocks WHERE id = ?)
				LIMIT ?
				OFFSET ?
			
			`, blockId, miniLimit, miniOffset).Scan(&images2).Error
			if err != nil {
				log.Error(err)
				return &echo.HTTPError{
					Code:    500,
					Message: fmt.Sprintf("neighbors failed: %s", err),
				}
			}
			images = append(images, images2...)
		}

	} else {

		err = s.db.Raw(
			`
		SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height, blocks.phash as Hash
		FROM blocks
		WHERE blocks.thumb IS NOT NULL
		and blocks.embedding is not null
		and nsfw is not true
		ORDER BY blocks.embedding <#> 
			(SELECT embedding FROM blocks WHERE id = ?)
		LIMIT ?
		OFFSET ?
	
	`, blockId, limit, offset).Scan(&images).Error
		if err != nil {
			log.Error(err)
			return &echo.HTTPError{
				Code:    500,
				Message: fmt.Sprintf("neighbors failed: %s", err),
			}
		}
	}

	// remove images with too similar values for distance
	var filteredImages []imageMeta
	var seenPhashes []int64

	for _, img := range images {
		// check if phash occurs in seenPhashes:
		if img.Phash == 0 || !slices.Contains(seenPhashes, img.Phash) {
			seenPhashes = append(seenPhashes, img.Phash)
			filteredImages = append(filteredImages, img)
		}

	}

	if len(filteredImages) > 0 {
		images = filteredImages
	}

	type neighborsResults struct {
		Images []imageMeta `json:"images"`
	}

	return e.JSON(200, neighborsResults{
		Images: images,
	})

}

func audit(s *Server, limit int, offset int, images *[]imageMeta) (bool, error) {
	err := s.db.Debug().Raw(
		fmt.Sprintf(
			`
			SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height 
			FROM blocks
			WHERE blocks.thumb IS NOT NULL
			and blocks.embedding is not null
			and nsfw is not true
			and blocks.id in (
				select max(block_id) from clusters group by cluster_id
			)
			LIMIT %d
			OFFSET %d
		
		`, limit, offset)).Scan(&images).Error
	if err != nil {
		log.Error(err)
		return true, &echo.HTTPError{
			Code:    500,
			Message: fmt.Sprintf("audit failed: %s", err),
		}
	}
	return false, nil
}

type FlagData struct {
	Id   int    `json:"id"`
	Flag string `json:"flag"`
	Pw   string `json:"pw"`
}

type VectorSearchRequest struct {
	Vector []float64 `json:"vector"`
	Limit  int       `json:"limit,omitempty"`
	Offset int       `json:"offset,omitempty"`
}

func (s *Server) handleFlag(c echo.Context) error {
	var flagData FlagData

	var password = os.Getenv("FLAG_PASSWORD")

	if err := c.Bind(&flagData); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid flag data")
	}
	// Moderation actions require FLAG_PASSWORD to be configured; an unset
	// password must not match the empty pw that ordinary visitors send.
	isAdmin := password != "" && subtle.ConstantTimeCompare([]byte(flagData.Pw), []byte(password)) == 1
	if isAdmin {
		if flagData.Flag == "boring" {
			query2 := s.db.Debug().Exec("delete from clusters where cluster_id in (select cluster_id from clusters where block_id = ?)", flagData.Id)
			if query2.Error != nil {
				log.Error(query2.Error)

				return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
			}
		}
		if flagData.Flag == "nsfw" {
			if err := s.db.Exec("UPDATE blocks SET nsfw = true WHERE id = ?", flagData.Id).Error; err != nil {
				log.Error(err)
				return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
			}
		}
		if flagData.Flag == "cool" {
			query2 := s.db.Debug().Exec(`
			INSERT INTO clusters (cluster_id, block_id)
			SELECT 
			   COALESCE((SELECT MAX(cluster_id) FROM clusters), 0) + 1, 
			   ?
			`, flagData.Id)
			if query2.Error != nil {
				log.Error(query2.Error)
				return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
			}
		}
		err := s.db.Debug().Raw(
			fmt.Sprintf(
				`
				SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height 
				FROM blocks
				join clusters on blocks.id = clusters.block_id
				WHERE blocks.thumb IS NOT NULL
				and blocks.embedding is not null
				and nsfw is not true
				and blocks.id in (
					select max(block_id) from clusters group by cluster_id
				)
				order by clusters.id desc
				LIMIT %d
			`, 500)).Scan(&s.cachedRandom).Error

		if err != nil {
			log.Error(err)
		}

	}
	query := s.db.Exec("INSERT INTO flags (block_id, flag) VALUES (?, ?)", flagData.Id, flagData.Flag)
	if query.Error != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"id":   strconv.Itoa(flagData.Id),
		"flag": flagData.Flag,
	})

}

func (s *Server) handleVectorNeighbors(c echo.Context) error {
	var req VectorSearchRequest

	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid vector search request")
	}

	// Validate vector dimension
	if len(req.Vector) != 512 {
		return echo.NewHTTPError(http.StatusBadRequest, "Vector must be 512 dimensional")
	}

	// Set default values if not provided
	limit := req.Limit
	if limit <= 0 {
		limit = 35
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	var images []imageMeta

	s.db.Exec("SET hnsw.ef_search = 70;")

	// Convert vector to PostgreSQL array format
	vectorStr := "["
	for i, val := range req.Vector {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%f", val)
	}
	vectorStr += "]"

	err := s.db.Raw(
		`
		SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height, blocks.phash as Hash
		FROM blocks
		WHERE blocks.thumb IS NOT NULL
		and blocks.embedding is not null
		and nsfw is not true
		ORDER BY blocks.embedding <#> ?::vector
		LIMIT ?
		OFFSET ?
	`, vectorStr, limit, offset).Scan(&images).Error

	if err != nil {
		log.Error(err)
		return &echo.HTTPError{
			Code:    500,
			Message: fmt.Sprintf("vector neighbors search failed: %s", err),
		}
	}

	// Remove images with too similar phash values for deduplication
	var filteredImages []imageMeta
	var seenPhashes []int64

	for _, img := range images {
		if img.Phash == 0 || !slices.Contains(seenPhashes, img.Phash) {
			seenPhashes = append(seenPhashes, img.Phash)
			filteredImages = append(filteredImages, img)
		}
	}

	if len(filteredImages) > 0 {
		images = filteredImages
	}

	type neighborsResults struct {
		Images []imageMeta `json:"images"`
	}

	return c.JSON(200, neighborsResults{
		Images: images,
	})
}
