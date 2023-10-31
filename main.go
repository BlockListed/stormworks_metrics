package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/blocklisted/stormworks-metrics/templates"
	"github.com/blocklisted/stormworks-metrics/types"
)

var (
	ErrQueryParamNotFound = errors.New("query param not found")
)

var (
	logRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_requests_received",
	})
	logRequestsCompleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_requests_completed",
	})
	logRequestTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "log_requests_processing_time",
		Buckets: []float64{
			0.000001,
			0.00001,
			0.0001,
			0.001,
			0.01,
			0.1,
			1.0,
			10.0,
		},
	})
)

type State struct {
	data map[uint64]types.StatusInfo
	mu   *sync.Mutex
}

func newState() State {
	l := &sync.Mutex{}

	return State{
		data: make(map[uint64]types.StatusInfo, 64),
		mu:   l,
	}
}

var STATE State = newState()

func main() {
	http.HandleFunc("/log", func(_ http.ResponseWriter, req *http.Request) {
		logRequests.Inc()

		start := time.Now()

		parsed_url, err := url.ParseRequestURI(req.RequestURI)
		if err != nil {
			slog.Error("failed to parse request uri", "uri", req.RequestURI)
			return
		}

		params := parsed_url.Query()

		id_str := params.Get("id")

		id, err := strconv.ParseUint(id_str, 10, 64)
		if err != nil {
			slog.Warn("invalid id", "id", id_str)
			id = 0
		}

		STATE.mu.Lock()
		data := STATE.data[id]
		for k := range params {
			v := params.Get(k)

			if k == "id" {
				data.Id = id
				continue
			}

			// Pollutes output, don't know how to silence yet
			//slog.Info("found value in params", "key", k, "value", v)

			parsed_v, err := strconv.ParseFloat(v, 64)
			if err != nil {
				slog.Warn("invalid value in params", "key", k, "value", v)
				continue
			}

			switch k {
			case "fuel":
				data.Fuel = parsed_v
			case "gps_x":
				data.GpsX = parsed_v
			case "gps_y":
				data.GpsY = parsed_v
			case "gps_z":
				data.GpsZ = parsed_v
			case "pitch_lookahead_secs":
				data.PitchLookaheadSeconds = parsed_v
			case "target_dir":
				data.TargetDir = parsed_v
			case "target_dist":
				data.TargetDist = parsed_v
			case "vehicle_speed":
				data.VehicleSpeed = parsed_v
			default:
				// See above comment
				slog.Warn("unknown key found", "key", k)
			}

		}
		data.LastUpdate = time.Now()
		STATE.data[id] = data
		STATE.mu.Unlock()

		elapsed := time.Since(start)
		logRequestTime.Observe(elapsed.Seconds())
		logRequestsCompleted.Inc()
	})

	http.HandleFunc("/status", func(resp http.ResponseWriter, _ *http.Request) {
		templates.StatusPage().Render(context.Background(), resp)
	})

	http.Handle("/status_updates", websocket.Handler(func(c *websocket.Conn) {
		ticker := time.NewTicker(time.Duration(100 * 1000 * 1000))
		defer ticker.Stop()
		slog.Info("starting websocket handler", "endpoint", c.RemoteAddr())

		for {
			STATE.mu.Lock()
			data := STATE.data

			slog.Info("acquired status data", "len", len(data))

			slice_data := make([]types.StatusInfo, 0, len(data))
			for _, v := range data {
				slice_data = append(slice_data, v)
			}
			STATE.mu.Unlock()

			buf := sendStatus(slice_data)
			_, err := c.Write(buf)
			if err != nil {
				return
			}
			slog.Info("sent status update")

			<-ticker.C
		}
	}))

	http.HandleFunc("/alive", func(_ http.ResponseWriter, _ *http.Request) {
		slog.Info("ALIVE")
	})

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	http.Handle("/metrics", promhttp.Handler())

	slog.Info("listening on port 8080")

	slog.Error("error while serving http", "err", http.ListenAndServe("0.0.0.0:8080", nil))
}

func sendStatus(data []types.StatusInfo) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))

	median_speed := medianSpeed(data)
	mean_speed := meanSpeed(data)
	delta_dist := deltaDist(data)
	active_missile_count := activeMissileCount(data)

	sort.Slice(data, func(i, j int) bool {
		return data[i].Id < data[j].Id
	})

	err := templates.Status(data, median_speed, mean_speed, delta_dist, active_missile_count).Render(context.Background(), buf)
	if err != nil {
		slog.Warn("couldn't write template to buffer", "err", err)
		return []byte("<h2 id=\"status\">Internal Server Error</h2>")
	}

	return buf.Bytes()
}

func medianSpeed(data []types.StatusInfo) float64 {
	if len(data) == 0 {
		return 0.0
	}

	sort.Slice(data, func(i, j int) bool {
		return data[i].VehicleSpeed < data[j].VehicleSpeed
	})

	median_index := len(data) / 2

	return data[median_index].VehicleSpeed
}

func meanSpeed(data []types.StatusInfo) float64 {
	if len(data) == 0 {
		return 0.0
	}

	accum := 0.0
	for _, v := range data {
		accum += v.VehicleSpeed
	}

	return accum / float64(len(data))
}

// Calculate the difference between the largest speed and the smallest speed
func deltaDist(data []types.StatusInfo) float64 {
	max := 0.0
	min := 0.0
	for _, v := range data {
		dist := v.TargetDist

		if dist > max {
			max = dist
		}
		if dist < min {
			min = dist
		}
	}

	return max - min
}

func activeMissileCount(data []types.StatusInfo) int64 {
	active_speed := 80.0
	var count int64 = 0
	for _, v := range data {
		if v.VehicleSpeed >= active_speed {
			count += 1
		}
	}

	return count
}
