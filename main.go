package main

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	cliutil "github.com/bluesky-social/indigo/util/cliutil"
	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	// . "github.com/whyrusleeping/algoz/models"
	"golang.org/x/crypto/acme/autocert"
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
	Id       int
	Width    int
	Height   int
	Thumb    string
	Distance float64
}
type Server struct {
	db           *gorm.DB
	cachedRandom []imageMeta
	userLk       sync.Mutex
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
				var image *imageMeta
				db.Raw(fmt.Sprintf(`
				SELECT blocks.display as Thumb
				FROM blocks
				WHERE id = %s
				LIMIT 1
				`, id)).Scan(&image)
				ogImage = image.Thumb
			} else {
				ogImage = "default_og_image.jpg"
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

		if cctx.Bool("feed-test") {
			go func() {
				// s.pollAllImagePaths(context.Background())
				// s.projectAllEmbeddings(context.Background())
			}()
			// block and don't index firehose
			select {}
		}

		err = s.db.Debug().Raw(
			fmt.Sprintf(
				`
				SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height
				FROM blocks
				WHERE blocks.thumb IS NOT NULL
				and blocks.embedding is not null
				ORDER BY random()
				LIMIT %d
			`, 1500)).Scan(&s.cachedRandom).Error

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

	s.db.Exec("SET hnsw.ef_search = 100;")
	if blockId == "" || blockId == "null" || blockId == "undefined" {

		// select 45 random items from cachedRandom
		// images = make([]imageMeta, limit)
		// for i := 0; i < limit; i++ {
		// convert int to string:
		blockId = strconv.Itoa(s.cachedRandom[rand.Intn(len(s.cachedRandom))].Id)
		// }

	}

	err = s.db.Debug().Raw(
		fmt.Sprintf(
			`
			SELECT blocks.id as Id, blocks.thumb as Thumb, blocks.width as Width, blocks.height as Height, blocks.embedding <#> 
			(SELECT embedding FROM blocks WHERE id = %s) as Distance
			FROM blocks
			WHERE blocks.thumb IS NOT NULL
			and blocks.embedding is not null
			ORDER BY blocks.embedding <#> 
				(SELECT embedding FROM blocks WHERE id = %s)
			LIMIT %d
			OFFSET %d
		
		`, blockId, blockId, limit, offset)).Scan(&images).Error

	if err != nil {
		log.Error(err)
		return &echo.HTTPError{
			Code:    500,
			Message: fmt.Sprintf("neighbors failed: %s", err),
		}
	}

	if offset == 0 {
		go func(blockId string) {
			s.db.Exec(fmt.Sprintf("UPDATE blocks SET votes = votes + 1 where id = %s", blockId))
		}(blockId)
	}

	// remove images with too similar values for distance
	var filteredImages []imageMeta
	var lastDistance = 10.0
	for _, img := range images {
		delta := math.Abs(img.Distance - lastDistance)
		log.Error(delta)

		if delta > 0.00005 {
			filteredImages = append(filteredImages, img)
		} else {
			// img.Height = 1000
			// filteredImages = append(filteredImages, img)
			// log.Error(img.Thumb)
		}
		lastDistance = img.Distance
	}
	images = filteredImages

	type neighborsResults struct {
		Images []imageMeta `json:"images"`
	}

	return e.JSON(200, neighborsResults{
		Images: images,
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
