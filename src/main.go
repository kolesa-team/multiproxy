package main

import (
	"net/http"
	"os"
	"syscall"
	"time"

	"./multiproxy/logger"
	"./multiproxy/server"
	log "github.com/Sirupsen/logrus"
	"github.com/braintree/manners"
	"github.com/codegangsta/cli"
	hcli "github.com/endeveit/go-snippets/cli"
	"github.com/endeveit/go-snippets/config"
	gd "github.com/sevlyar/go-daemon"
)

var (
	stop = make(chan struct{})
	done = make(chan struct{})
)

func main() {
	app := cli.NewApp()

	app.Name = "multiproxy"
	app.Usage = "Proxy for duplicating requests to multiple backends"
	app.Version = "0.0.2"
	app.Author = "Igor Borodikhin"
	app.Email = "iborodikhin@gmail.com"
	app.Action = actionRun
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "daemon, d",
			Usage: "If provided, the service will be launched as daemon",
		},
		cli.BoolFlag{
			Name:  "debug, b",
			Usage: "If provided, the service will be launched in debug mode",
		},
		cli.StringFlag{
			Name:  "config, c",
			Value: "/etc/upload/config.cfg",
			Usage: "Path to the configuration file",
		},
		cli.StringFlag{
			Name:  "pid, p",
			Value: "/var/run/upload.pid",
			Usage: "Path to the file where PID will be stored",
		},
	}

	app.Run(os.Args)
}

func actionRun(c *cli.Context) error {
	isDaemon := c.Bool("daemon")
	cnf := config.Instance(c.String("config"))
	pidfile := c.String("pid")

	addr, err := cnf.String("http", "addr")
	hcli.CheckError(err)

	keepAlive, err := cnf.Int("http", "keep_alive")
	hcli.CheckError(err)

	if c.Bool("debug") {
		logger.Instance().Level = log.DebugLevel
	}

	server := manners.NewWithServer(&http.Server{
		Addr:         addr,
		Handler:      server.NewMux(),
		ReadTimeout:  time.Duration(keepAlive) * time.Second,
		WriteTimeout: time.Duration(keepAlive) * time.Second,
	})
	server.SetKeepAlivesEnabled(true)

	if !isDaemon {
		runServer(server, pidfile)
	} else {
		gd.SetSigHandler(termHandler, syscall.SIGTERM)

		dmn := &gd.Context{
			PidFileName: pidfile,
			PidFilePerm: 0644,
			WorkDir:     "/",
			Umask:       027,
		}

		child, err := dmn.Reborn()
		if err != nil {
			logger.Instance().WithFields(log.Fields{
				"error": err,
			}).Error("An error occured while trying to reborn daemon")
		}

		if child != nil {
			return err
		}

		defer dmn.Release()

		go runServer(server, pidfile)
		go func() {
			for {
				time.Sleep(time.Second)
				if _, ok := <-stop; ok {
					logger.Instance().Info("Terminating daemon")
					server.Close()
				}
			}
		}()

		err = gd.ServeSignals()
		if err != nil {
			logger.Instance().WithFields(log.Fields{
				"error": err,
			}).Error("An error occured while serving signals")

			return err
		}
	}

	return nil
}

// Запуск сервера
func runServer(server *manners.GracefulServer, pidfile string) {
	logger.Instance().Info("Starting daemon")

	server.ListenAndServe()

	done <- struct{}{}
}

// Обработчик SIGTERM
func termHandler(sig os.Signal) error {
	stop <- struct{}{}

	if sig == syscall.SIGTERM {
		<-done
	}

	return gd.ErrStop
}
