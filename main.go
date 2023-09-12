package main

import (
	"context"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	gif "github.com/NathanBaulch/gifx"

	cliutil "github.com/bluesky-social/indigo/util/cliutil"
	"github.com/corona10/goimagehash"
	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	// . "github.com/whyrusleeping/algoz/models"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/exp/slices"
	"gonum.org/v1/gonum/floats"

	cli "github.com/urfave/cli/v2"

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
		&cli.BoolFlag{
			Name: "feed-test",
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

		// e.HTTPErrorHandler = func(err error, c echo.Context) {
		// 	log.Error(err)
		// }

		s := &Server{
			db: db,
		}

		e.GET("/neighbors", s.handleGetNeighbors)
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

		go func() {
			// s.pollAllImagePaths(context.Background())
			// s.projectAllEmbeddings(context.Background())
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
			// log.Error(offset, (offset*3)+i)
			miniOffset := int(10 * int(offset/len(s.cachedRandom)))
			miniLimit := int(limit / 3)
			err = s.db.Debug().Raw(
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

		err = s.db.Debug().Raw(
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

	// if offset == 0 {
	// 	go func(blockId string) {
	// 		s.db.Exec("UPDATE blocks SET votes = votes + 1 where id = ?", blockId)
	// 	}(blockId)
	// }

	// remove images with too similar values for distance
	var filteredImages []imageMeta
	var seenPhashes []int64

	// sortDelta:
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

func (s *Server) handleFlag(c echo.Context) error {
	var flagData FlagData

	var password = os.Getenv("FLAG_PASSWORD")

	if err := c.Bind(&flagData); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid flag data")
	}
	if flagData.Pw == password {
		if flagData.Flag == "boring" {
			query2 := s.db.Debug().Exec("delete from clusters where cluster_id in (select cluster_id from clusters where block_id = ?)", flagData.Id)
			if query2.Error != nil {
				log.Error(query2.Error)

				return echo.NewHTTPError(http.StatusInternalServerError, "Could not flag the item")
			}
		}
		if flagData.Flag == "nsfw" {
			s.db.Debug().Exec(fmt.Sprintf("UPDATE blocks SET nsfw = true where id = %d", flagData.Id))
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

func NormalizeVector(vec []float64) {
	norm := floats.Norm(vec, 2) //calculates the L2 norm (Euclidean norm) of vec
	if norm == 0 {
		// handle it accordingly
		log.Error("Norm is zero")
	} else {
		for i := range vec {
			vec[i] /= norm
		}
	}
}

func Min(x, y int) int {
	if x > y {
		return y
	}
	return x
}

func (s *Server) projectAllEmbeddings(ctx context.Context) error {
	c := time.Tick(100 * time.Millisecond)
	// log.Error(v1.Dims())
	for range c {

		var images []imageMeta

		s.db.Raw(`
		SELECT blocks.thumb as Thumb, blocks.id as Id
		FROM blocks
		WHERE blocks.nsfw is not true
		and blocks.phash is null
		and blocks.gif is not true
		and blocks.embedding is not null
		LIMIT 30
		`).Scan(&images)

		// sum := mat.NewVecDense(512, nil)
		var wg sync.WaitGroup

		for _, img := range images {
			wg.Add(1)
			go func(imgMeta imageMeta) {
				defer wg.Done()

				file, err := http.Get(imgMeta.Thumb)
				if err != nil {
					log.Error(err)
					return
				}
				defer file.Body.Close()

				var img image.Image
				var err2 error
				// log.Error(file.Header.Get("Content-Type"))
				if file.Header.Get("Content-Type") == "image/gif" {
					img, err2 = decodeFirstFrame(file)
					s.db.Exec(fmt.Sprintf("UPDATE blocks SET gif = true where id = %d", imgMeta.Id))
					if err2 != nil {
						log.Error(err2)

						return
					}
				} else {
					img, _, err2 = image.Decode(file.Body)
					if err2 != nil {
						log.Error(err2)
						return
					}
				}

				hash, err := goimagehash.PerceptionHash(img)
				if err != nil {
					log.Error(err)
					return
				}
				// log.Error(hash.GetHash())
				s.db.Exec(fmt.Sprintf("UPDATE blocks SET phash = %d where id = %d", int64(hash.GetHash()), imgMeta.Id))
			}(img)
		}

		wg.Wait() // Wait for all goroutines to finish.
	}
	return nil
}

func decodeFirstFrame(file *http.Response) (image.Image, error) {
	gifImg, err := gif.Decode(file.Body)
	if err != nil {
		return nil, err
	}

	// gifImg.Image[0] is the first frame
	return gifImg, nil
}
