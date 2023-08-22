package hub

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/lainio/err2"
	"github.com/lainio/err2/try"
	"github.com/redis/go-redis/v9"
	"github.com/shynome/go-x25519"
)

const kb = 1 << 10
const BodyLimit = 100 * kb

func (hub *Hub) initRoute() {
	hub.Use(middleware.BodyLimit(BodyLimit))
	hub.Use(middleware.CORSWithConfig(func() middleware.CORSConfig {
		cors := middleware.DefaultCORSConfig
		cors.AllowHeaders = []string{"X-Event-Id", "Cache-Control", "Last-Event-Id", "Content-Type"}
		cors.AllowCredentials = true
		return cors
	}()))
	hub.Use(auth)
	hub.Use(middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Skipper: middleware.DefaultSkipper,
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      10,
				Burst:     1000,
				ExpiresIn: 3 * time.Minute,
			},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.QueryParam("pubkey"), nil
		},
	}))

	var app *SSE = &SSE{rdb: hub.rdb}
	hub.GET("/", app.Subscribe)
	hub.POST("/", app.HandleCall)
	hub.DELETE("/", app.FinishCall)
}

type SSE struct {
	rdb *redis.Client
}

func (app *SSE) Subscribe(c echo.Context) (err error) {
	defer err2.Handle(&err)
	ctx := c.Request().Context()
	pid := "peer-" + c.QueryParam("pubkey")
	sub := app.rdb.Subscribe(ctx, pid)
	defer sub.Close()

	res := c.Response()
	writeHeader(res)
	sendComment(res)

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sendComment(res)
		case msg := <-ch:
			io.WriteString(res, msg.Payload)
			io.WriteString(res, "\n")
			res.Flush()
		}
	}
}

func (app *SSE) HandleCall(c echo.Context) (err error) {
	defer err2.Handle(&err)
	rdb := app.rdb

	data := try.To1(io.ReadAll(c.Request().Body))

	ctx := c.Request().Context()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	try.To1(parsePubkey(c.QueryParam("peer")))
	pid := "peer-" + c.QueryParam("peer")
	countMap := try.To1(rdb.PubSubNumSub(ctx, pid).Result())
	if countMap[pid] == 0 {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	eid := uuid.NewString()
	sub := rdb.Subscribe(ctx, "call-"+eid)
	defer sub.Close()

	ev := Event{
		ID:   eid,
		Data: data,
	}
	try.To(rdb.Publish(ctx, pid, ev.String()).Err())

	msg := try.To1(sub.ReceiveMessage(ctx))

	return c.Blob(http.StatusOK, "application/octet-stream", []byte(msg.Payload))
}

func (app *SSE) FinishCall(c echo.Context) (err error) {
	defer err2.Handle(&err)
	ctx := c.Request().Context()

	eid := "call-" + c.Request().Header.Get("X-Event-Id")
	countMap := try.To1(app.rdb.PubSubNumSub(ctx, eid).Result())
	if countMap[eid] == 0 {
		return c.NoContent(http.StatusGone)
	}

	data := try.To1(io.ReadAll(c.Request().Body))
	try.To(app.rdb.Publish(ctx, eid, data).Err())

	return c.NoContent(http.StatusNoContent)
}

func auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) (err error) {
		defer err2.Handle(&err)

		timestamp := parseUnixTimestamp(c.QueryParam("timestamp"))
		if time.Since(timestamp) > 30*time.Second {
			return echo.NewHTTPError(http.StatusBadRequest, ErrSignatureExpired)
		}

		pubkey := try.To1(parsePubkey(c.QueryParam("pubkey")))
		signature := try.To1(hex.DecodeString(c.QueryParam("signature")))
		msg := []byte(c.QueryParam("timestamp"))
		if verified := x25519.Verify(pubkey, msg, signature); !verified {
			return echo.NewHTTPError(http.StatusUnauthorized, ErrNotWireGuardPubkey)
		}

		return next(c)
	}
}

func parsePubkey(s string) (pubkey x25519.PublicKey, err error) {
	if len(s) != 64 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, ErrNotWireGuardPubkey)
	}
	return hex.DecodeString(s)
}

var ErrNotWireGuardPubkey = errors.New("pubkey is not WireGuard pubkey")
var ErrSignatureVerifyFailed = errors.New("signature verify failed")
var ErrSignatureExpired = errors.New("signature is expired")
