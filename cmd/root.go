package cmd

import (
	"net"
	"net/http"
	"os"

	"github.com/labstack/echo/v5"
	"github.com/lainio/err2"
	"github.com/lainio/err2/try"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"remoon.net/xhe-hub/hub"
)

var cfg struct {
	redis   string
	addr    string
	version string
}

const listenAddrSavedFile = ".xhe-hub-addr"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "xhe-hub",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		opts := try.To1(redis.ParseURL(cfg.redis))
		rdb := redis.NewClient(opts)
		e := hub.New(rdb)
		e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				path := c.Request().URL.Path
				if path == "/health" {
					return HealthCheckHandle(rdb, c)
				}
				return next(c)
			}
		})
		l := try.To1(net.Listen("tcp", cfg.addr))
		defer l.Close()
		try.To(os.WriteFile(listenAddrSavedFile, []byte(l.Addr().String()), os.ModePerm))
		try.To(http.Serve(l, e))
	},
}

func Execute(version string) {
	rootCmd.Version = version
	cfg.version = version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&cfg.redis, "redis", "redis://localhost/1", "redis connect url")
	rootCmd.Flags().StringVar(&cfg.addr, "addr", ":8090", "listen addr")
}

func HealthCheckHandle(rdb *redis.Client, c echo.Context) (err error) {
	defer err2.Handle(&err)
	ctx := c.Request().Context()
	eg := new(errgroup.Group)
	eg.Go(func() error {
		return rdb.Ping(ctx).Err()
	})
	try.To(eg.Wait())
	return c.JSON(http.StatusOK, map[string]string{
		"version": cfg.version,
	})
}
