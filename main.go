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
	"golang.org/x/net/websocket"
)

type State struct {
	speed      float64
	target_dir float64
	mu         *sync.Mutex
	update     *sync.Cond
}

func newState() State {
	mu := &sync.Mutex{}

	update := sync.NewCond(mu)

	return State{
		speed:      0.0,
		target_dir: 0.0,
		mu:         mu,
		update:     update,
	}
}

var STATE State = newState()

func main() {
	http.HandleFunc("/target", func(_ http.ResponseWriter, req *http.Request) {
		target_dir, err := getFloatQueryParam(req, "dir")

		if err != nil {
			slog.Error("could not get target dir query param", "err", err)
			return
		}

		slog.Info("successfully parsed dir", "dir", target_dir)

		STATE.mu.Lock()
		STATE.target_dir = target_dir
		STATE.update.Broadcast()
		STATE.mu.Unlock()
	})

	http.HandleFunc("/vehicle", func(_ http.ResponseWriter, req *http.Request) {
		speed, err := getFloatQueryParam(req, "speed")

		if err != nil {
			slog.Error("could not get speed query param", "err", err)
			return
		}

		slog.Info("successfully parsed speed", "speed", speed)

		STATE.mu.Lock()
		STATE.speed = speed
		STATE.update.Broadcast()
		STATE.mu.Unlock()
	})

	http.HandleFunc("/status", func(resp http.ResponseWriter, _ *http.Request) {
		templates.StatusPage().Render(context.Background(), resp)
	})

	http.Handle("/status_updates", websocket.Handler(func(c *websocket.Conn) {
		slog.Info("starting websocket handler", "endpoint", c.RemoteAddr())

		for {
			STATE.mu.Lock()
			STATE.update.Wait()
			speed := STATE.speed
			target_dir := STATE.target_dir
			STATE.mu.Unlock()

			slog.Info("acquired status data", "speed", speed, "target_dir", target_dir)

			buf := sendStatus(speed, target_dir)
			c.Write(buf)
			slog.Info("sent status update")
		}
	}))

	slog.Info("listening on port 8080")

	slog.Error("error while serving http", "err", http.ListenAndServe(":8080", nil))
}

func sendStatus(speed float64, target_dir float64) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))

	err := templates.Status(str_float(speed), str_float(target_dir))
	if err != nil {
		slog.Warn("couldn't write template to buffer", "err", err)
		return []byte("<h2 id=\"status\">Internal Server Error</h2>")
	}

	return buf.Bytes()
}

func str_float(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}

func getQueryParam(req *http.Request, key string) (string, error) {
	params, err := url.ParseRequestURI(req.RequestURI)
	if err != nil {
		return "", err
	}

	query_str := params.Query().Get(key)
	if query_str == "" {
		return "", errors.New("query param not found")
	}

	return query_str, nil
}

func getFloatQueryParam(req *http.Request, key string) (float64, error) {
	query_param, err := getQueryParam(req, key)
	if err != nil {
		return 0.0, err
	}

	query_float, err := strconv.ParseFloat(query_param, 64)
	if err != nil {
		return 0.0, err
	}

	return query_float, nil
}
