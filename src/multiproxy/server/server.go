package server

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

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
	timeout         time.Duration
	remote          []string
	backup          []string
)

func initServer() {
	once.Do(func() {
		var (
			r, b, t string
			err     error
		)

		enableAccessLog, err = config.Instance().Bool("http", "access_log")
		cli.CheckError(err)

		r, err = config.Instance().String("remote", "hosts")
		cli.CheckError(err)

		b, err = config.Instance().String("remote", "backup")
		cli.CheckError(err)

		t, err = config.Instance().String("remote", "timeout")
		if err != nil {
			t = "1s"
		}

		timeout, err = time.ParseDuration(t)
		cli.CheckError(err)

		remote = string_slice_unique(strings.Split(r, ";"))
		backup = string_slice_unique(strings.Split(b, ";"))
		client = http.Client{
			Timeout: timeout,
		}
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

		response = doUpstreams(r)
		if response == nil {
			response = doBackups(r)
		}

		if response != nil {
			defer response.Body.Close()

			for k, v := range response.Header {
				for _, h := range v {
					w.Header().Set(k, h)
				}
			}

			io.Copy(w, response.Body)
		} else {
			logger.Instance().Error("Response was nil")

			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		}
	})

	return m
}

// Request upstreams in parallel
func doUpstreams(r *http.Request) *http.Response {
	var response *http.Response
	resChan := make(chan *http.Response)

	for _, host := range remote {
		go func(h string, c chan *http.Response) {
			rwMutext.Lock()
			defer rwMutext.Unlock()

			var req *http.Request

			if req = requestSetHost(h, r); req != nil {
				c <- makeRequest(req)

				return
			} else {
				c <- nil

				return
			}

		}(host, resChan)
	}

	for i := 0; i < len(remote); i++ {
		select {
		case r := <-resChan:
			if response == nil && r != nil {
				response = r
			}
		}
	}

	return response
}

// Request backups in serial
func doBackups(r *http.Request) *http.Response {
	for _, host := range backup {
		var req *http.Request
		if req = requestSetHost(host, r); req != nil {
			return makeRequest(req)
		} else {
			return nil
		}
	}

	return nil
}

func makeRequest(req *http.Request) *http.Response {
	resp, err := client.Do(req)

	if resp != nil && resp.StatusCode >= 400 {
		defer resp.Body.Close()
	}

	if err == nil && resp.StatusCode < 400 {
		logger.Instance().WithFields(log.Fields{
			"url":    resp.Request.URL,
			"status": resp.StatusCode,
		}).Debug("Request success")

		return resp
	}

	logger.Instance().WithFields(log.Fields{
		"url":   req.URL.String(),
		"error": err,
	}).Error("Request error")

	return nil
}

func requestSetHost(h string, r *http.Request) *http.Request {
	u, err := url.Parse(h)
	if err != nil {
		logger.Instance().WithFields(log.Fields{
			"url":   h,
			"error": err,
		}).Error("Remote parse error")

		return nil
	}

	req := r
	req.URL.Host = u.Host
	req.URL.Scheme = u.Scheme

	return req
}

func string_slice_unique(slice []string) (result []string) {
	m := make(map[string]bool)

	for _, v := range slice {
		m[v] = true
	}

	for k, _ := range m {
		result = append(result, k)
	}

	return
}
