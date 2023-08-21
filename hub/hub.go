package hub

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/redis/go-redis/v9"
)

type Hub struct {
	*echo.Echo
	rdb *redis.Client
}

var _ http.Handler = (*Hub)(nil)

func New(rdb *redis.Client) *Hub {
	app := echo.New()
	hub := &Hub{
		Echo: app,
		rdb:  rdb,
	}
	hub.initRoute()
	return hub
}
