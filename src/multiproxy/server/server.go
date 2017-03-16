package server

import (
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	w "../worker"

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
	enableAccessLog bool = false
	once            sync.Once
	worker          *w.Worker
)

func initServer() {
	once.Do(func() {
		var (
			err error
		)

		if enableAccessLog, err = config.Instance().Bool("http", "access_log"); err != nil {
			enableAccessLog = false
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

	m.Handle(regexp.MustCompile(`^/(.*)$`), func(c web.C, w http.ResponseWriter, request *http.Request) {
		var response *http.Response

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
	})

	return m
}
