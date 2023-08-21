package hub

import (
	"context"
	"encoding/base64"
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
	hub.Use(extractPubkey)
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
			pubkey := c.Get("pubkey").([]byte)
			id := hex.EncodeToString(pubkey)
			c.Set("whoami", id)
			return id, nil
		},
	}))

	var app *SSE = &SSE{rdb: hub.rdb}
	hub.GET("/", app.Subscribe, auth)
	hub.POST("/", app.HandleCall)
	hub.DELETE("/", app.FinishCall)
}

type SSE struct {
	rdb *redis.Client
}

func (app *SSE) Subscribe(c echo.Context) (err error) {
	defer err2.Handle(&err)
	ctx := c.Request().Context()
	pid := "peer-" + c.Get("whoami").(string)
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

	data, _ := try.To2(verifyData(c))

	ctx := c.Request().Context()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	peerPubkey := try.To1(parseURLPubkey(c.QueryParam("peer")))
	pid := "peer-" + hex.EncodeToString(peerPubkey)
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

	data, _ := try.To2(verifyData(c))
	try.To(app.rdb.Publish(ctx, eid, data).Err())

	return c.NoContent(http.StatusNoContent)
}

func extractPubkey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		pubkey, err := parseURLPubkey(c.QueryParam("pubkey"))
		if err != nil {
			return err
		}
		c.Set("pubkey", pubkey)
		return next(c)
	}
}

func parseURLPubkey(s string) (pubkey []byte, err error) {
	pubkey = try.To1(base64.RawURLEncoding.DecodeString(s))
	if len(pubkey) != 32 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, ErrNotWireGuardPubkey)
	}
	return
}

func auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) (err error) {
		defer err2.Handle(&err)

		timestamp := parseUnixTimestamp(c.QueryParam("timestamp"))
		if time.Since(timestamp) > 30*time.Second {
			return echo.NewHTTPError(http.StatusBadRequest, ErrSignatureExpired)
		}

		pubkey := x25519.PublicKey(c.Get("pubkey").([]byte))
		signature := try.To1(base64.RawURLEncoding.DecodeString(c.QueryParam("signature")))
		msg := []byte(c.QueryParam("timestamp"))
		if verified := x25519.Verify(pubkey, msg, signature); !verified {
			return echo.NewHTTPError(http.StatusUnauthorized, ErrNotWireGuardPubkey)
		}

		return next(c)
	}
}

func verifyData(c echo.Context) (data []byte, pubkeyRaw []byte, err error) {
	defer err2.Handle(&err)

	pubkey := x25519.PublicKey(c.Get("pubkey").([]byte))
	signature := try.To1(base64.RawURLEncoding.DecodeString(c.QueryParam("signature")))
	data = try.To1(io.ReadAll(c.Request().Body))
	if verified := x25519.Verify(pubkey, data, signature); !verified {
		err2.Throwf("%w", echo.NewHTTPError(http.StatusBadRequest, ErrSignatureVerifyFailed))
	}

	return
}

var ErrNotWireGuardPubkey = errors.New("pubkey is not WireGuard pubkey")
var ErrSignatureVerifyFailed = errors.New("signature verify failed")
var ErrSignatureExpired = errors.New("signature is expired")
