package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/blocklisted/stormworks-metrics/templates"
	"github.com/blocklisted/stormworks-metrics/types"
	"golang.org/x/net/websocket"
)

var (
	ErrQueryParamNotFound = errors.New("query param not found")
)

type State struct {
	data  map[string]float64
	mu    *sync.Mutex
	notif *sync.Cond
}

func newState() State {
	l := &sync.Mutex{}

	notif := sync.NewCond(l)

	return State{
		data:  make(map[string]float64),
		mu:    l,
		notif: notif,
	}
}

var STATE State = newState()

func main() {
	http.HandleFunc("/log", func(_ http.ResponseWriter, req *http.Request) {
		parsed_url, err := url.ParseRequestURI(req.RequestURI)
		if err != nil {
			slog.Error("failed to parse request uri", "uri", req.RequestURI)
			return
		}

		params := parsed_url.Query()

		STATE.mu.Lock()
		for k := range params {
			v := params.Get(k)
			slog.Info("found value in params", "key", k, "value", v)

			parsed_v, err := strconv.ParseFloat(v, 64)
			if err != nil {
				slog.Warn("invalid value in params", "key", k, "value", v)
				continue
			}

			STATE.data[k] = parsed_v
		}
		STATE.notif.Broadcast()
		STATE.mu.Unlock()
	})

	http.HandleFunc("/status", func(resp http.ResponseWriter, _ *http.Request) {
		templates.StatusPage().Render(context.Background(), resp)
	})

	http.Handle("/status_updates", websocket.Handler(func(c *websocket.Conn) {
		slog.Info("starting websocket handler", "endpoint", c.RemoteAddr())

		for {
			STATE.mu.Lock()
			STATE.notif.Wait()
			data := STATE.data
			STATE.mu.Unlock()

			slog.Info("acquired status data", "data", data)

			buf := sendStatus(data)
			c.Write(buf)
			slog.Info("sent status update")
		}
	}))

	http.HandleFunc("/alive", func(_ http.ResponseWriter, _ *http.Request) {
		slog.Info("ALIVE")
	})

	slog.Info("listening on port 8080")

	slog.Error("error while serving http", "err", http.ListenAndServe(":8080", nil))
}

func sendStatus(data map[string]float64) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))

	sorted := types.SortedMap(data)

	err := templates.Status(sorted).Render(context.Background(), buf)
	if err != nil {
		slog.Warn("couldn't write template to buffer", "err", err)
		return []byte("<h2 id=\"status\">Internal Server Error</h2>")
	}

	return buf.Bytes()
}
