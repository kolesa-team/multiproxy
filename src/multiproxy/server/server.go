package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"../logger"
	w "../worker"

	log "github.com/Sirupsen/logrus"
	"github.com/endeveit/go-snippets/config"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

type Result struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type Host struct {
	host        string
	isBroken    bool
	brokenSince time.Time
}

var (
	enableAccessLog           bool = false
	once                      sync.Once
	worker                    *w.Worker
	activeClients             int64
	activeClientsMax          int64
	activeClientsReached      int64
	activeClientsReachedTimes int64
)

func initServer() {
	once.Do(func() {
		var (
			err error
		)

		if enableAccessLog, err = config.Instance().Bool("http", "access_log"); err != nil {
			enableAccessLog = false
		}

		if activeClientsSetup, err := config.Instance().Int("http", "queue_length"); err == nil {
			activeClientsMax = int64(activeClientsSetup)
		} else {
			activeClientsMax = int64(64)
		}

		worker = w.NewWorker()
	})

}

func NewMux() *web.Mux {
	initServer()

	m := web.New()

	if enableAccessLog {
		m.Use(middleware.RealIP)
		m.Use(mwLogger)
	}

	m.Use(mwRecoverer)

	m.Get("/status", handleStatus)
	m.Handle(regexp.MustCompile(`^/(.*)$`), handleRequest)

	return m
}

func handleStatus(c web.C, w http.ResponseWriter, r *http.Request) {
	response := make(map[string]interface{})

	response["proxy-workers-current"] = activeClients
	response["proxy-workers-max"] = activeClientsMax
	response["proxy-workers-reached-max"] = activeClientsReached
	response["proxy-workers-reached-times"] = activeClientsReachedTimes

	b, err := json.Marshal(response)
	if err != nil {
		logger.Instance().WithFields(log.Fields{
			"error": err,
		}).Warning("Unable marshal response")
	} else {
		w.Write(b)
	}
}

func handleRequest(c web.C, w http.ResponseWriter, request *http.Request) {
	var response *http.Response

	if !hasFreeQueueSlots() {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)

		logger.Instance().Warning("Incoming queue is full")

		return
	}

	atomic.AddInt64(&activeClients, 1)
	defer atomic.AddInt64(&activeClients, -1)

	r := &http.Request{
		URL: request.URL,
	}

	response = worker.Do(r)

	if response != nil {
		defer response.Body.Close()

		for k, v := range response.Header {
			for _, h := range v {
				w.Header().Set(k, h)
			}
		}

		io.Copy(w, response.Body)
	} else {
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}
}

// Check if we have more clients than can handle
func hasFreeQueueSlots() bool {
	if activeClients > activeClientsMax {
		atomic.AddInt64(&activeClientsReachedTimes, 1)

		return false
	}

	if activeClients >= activeClientsReached {
		activeClientsReached = activeClients
	}

	return true
}
