package server

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"../logger"

	log "github.com/Sirupsen/logrus"
	"github.com/endeveit/go-snippets/cli"
	"github.com/endeveit/go-snippets/config"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

type Result struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

var (
	enableAccessLog bool = false
	once            sync.Once
	rwMutext        sync.RWMutex
	client          http.Client
	remote          []string
)

func initServer() {
	once.Do(func() {
		var (
			r   string
			err error
		)

		enableAccessLog, err = config.Instance().Bool("http", "access_log")
		cli.CheckError(err)

		r, err = config.Instance().String("remote", "hosts")
		cli.CheckError(err)

		remote = strings.Split(r, ";")
		client = http.Client{}
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

	m.Handle(regexp.MustCompile(`^/(.*)$`), func(c web.C, w http.ResponseWriter, r *http.Request) {
		var response *http.Response
		resChan := make(chan *http.Response)

		r.RequestURI = ""
		r.URL.Scheme = "http"

		for _, host := range remote {
			go func(h string) {
				rwMutext.Lock()

				req := r
				req.URL.Host = h
				resp, err := client.Do(req)

				if resp != nil {
					defer resp.Body.Close()
				}

				if err == nil {
					logger.Instance().WithFields(log.Fields{
						"url":    resp.Request.URL,
						"status": resp.StatusCode,
					}).Debug("Remote request")

					resChan <- resp
				} else {
					logger.Instance().WithFields(log.Fields{
						"url":   req.URL.String(),
						"error": err,
					}).Error("Remote request error")

					resChan <- nil
				}

				rwMutext.Unlock()
			}(host)
		}

		for i := 0; i < len(remote); i++ {
			select {
			case r := <-resChan:
				if r != nil {
					response = r
				}
			}
		}

		io.Copy(w, response.Body)
	})

	return m
}
