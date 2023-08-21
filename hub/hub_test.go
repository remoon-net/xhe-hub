package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lainio/err2"
	"github.com/lainio/err2/assert"
	"github.com/lainio/err2/try"
	"github.com/r3labs/sse/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shynome/go-x25519"
	"golang.org/x/sync/errgroup"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/cenkalti/backoff.v1"
)

var hub *Hub
var addr net.Addr

func TestMain(m *testing.M) {
	opts := try.To1(redis.ParseURL("redis://localhost/1"))
	rdb := redis.NewClient(opts)
	hub = New(rdb)
	l := try.To1(net.Listen("tcp", "127.0.0.1:0"))
	defer l.Close()
	addr = l.Addr()
	go http.Serve(l, hub)
	m.Run()
}

func TestSubscribe(t *testing.T) {
	eg := new(errgroup.Group)
	data := []byte("hello world")
	responseText := []byte("777")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var peer []byte
	eg.Go(func() (err error) {
		defer err2.Handle(&err)
		privkey := try.To1(wgtypes.GeneratePrivateKey())
		pubkey := privkey.PublicKey()
		peer = pubkey[:]
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		u := try.To1(url.Parse("http://" + addr.String()))
		q := u.Query()
		q.Set("timestamp", timestamp)
		u.RawQuery = q.Encode()
		signURL(u, privkey[:], []byte(timestamp))
		c := sse.NewClient(u.String(), func(c *sse.Client) {
			c.ReconnectStrategy = &backoff.StopBackOff{}
			c.ResponseValidator = func(c *sse.Client, resp *http.Response) error {
				if !strings.HasPrefix(resp.Status, "20") {
					return fmt.Errorf("expect status 2xx, but got %s", resp.Status)
				}
				return nil
			}
		})
		ch := make(chan *sse.Event)
		ctx, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		try.To(c.SubscribeChanRawWithContext(ctx, ch))
		cancel()
		msg := <-ch
		assert.Equal(string(msg.Data), string(data))
		u = try.To1(url.Parse("http://" + addr.String()))
		signURL(u, privkey[:], responseText)
		req := try.To1(http.NewRequest(http.MethodDelete, u.String(), bytes.NewBuffer(responseText)))
		req.Header.Set("X-Event-Id", string(msg.ID))
		resp := try.To1(http.DefaultClient.Do(req))
		if !strings.HasPrefix(resp.Status, "2") {
			body := try.To1(io.ReadAll(resp.Body))
			err2.Throwf(string(body))
		}
		return
	})
	<-ctx.Done()
	if err := context.Cause(ctx); !errors.Is(err, context.Canceled) {
		t.Error(err)
		return
	}
	eg.Go(func() (err error) {
		defer err2.Handle(&err)
		privkey := try.To1(wgtypes.GeneratePrivateKey())
		pubkey := privkey.PublicKey()
		signature := try.To1(x25519.Sign(rand.Reader, privkey[:], data))
		u := try.To1(url.Parse("http://" + addr.String()))
		q := u.Query()
		q.Set("peer", base64.RawURLEncoding.EncodeToString(peer[:]))
		q.Set("pubkey", base64.RawURLEncoding.EncodeToString(pubkey[:]))
		q.Set("signature", base64.RawURLEncoding.EncodeToString(signature))
		u.RawQuery = q.Encode()
		req := try.To1(http.NewRequest(http.MethodPost, u.String(), bytes.NewBuffer(data)))
		resp := try.To1(http.DefaultClient.Do(req))
		if !strings.HasPrefix(resp.Status, "2") {
			err2.Throwf("expec code 2xx, but got %s", resp.Status)
		}
		body := try.To1(io.ReadAll(resp.Body))
		t.Log(body)
		assert.Equal(string(body), string(responseText))
		return
	})
	try.To(eg.Wait())
}

func signURL(u *url.URL, privkey x25519.PrivateKey, data []byte) {
	pubkey, _ := privkey.PublicKey()
	signature := try.To1(x25519.Sign(rand.Reader, privkey, data))
	q := u.Query()
	q.Set("pubkey", base64.RawURLEncoding.EncodeToString(pubkey))
	q.Set("signature", base64.RawURLEncoding.EncodeToString(signature))
	u.RawQuery = q.Encode()
}
