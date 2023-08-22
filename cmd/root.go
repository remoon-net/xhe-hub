package cmd

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"

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
	logLv   string
}

const listenAddrSavedFile = ".xhe-hub-addr"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "xhe-hub",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		func() {
			var lv slog.Level
			b := strconv.AppendQuote(nil, cfg.logLv)
			try.To(json.Unmarshal(b, &lv))
			opts := &slog.HandlerOptions{
				Level: lv,
			}
			handler := slog.NewJSONHandler(os.Stderr, opts)
			logger := slog.New(handler)
			slog.SetDefault(logger)
		}()

		opts := try.To1(redis.ParseURL(cfg.redis))
		rdb := redis.NewClient(opts)
		e := hub.New(rdb)
		// e.GET("/health", func(c echo.Context) error {
		// 	return HealthCheckHandle(rdb, c)
		// })
		l := try.To1(net.Listen("tcp", cfg.addr))
		defer l.Close()
		// try.To(os.WriteFile(listenAddrSavedFile, []byte(l.Addr().String()), os.ModePerm))
		slog.Warn("server start",
			"addr", l.Addr().String(),
		)
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
	rootCmd.PersistentFlags().StringVar(&cfg.logLv, "log", "info", "log level")

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
