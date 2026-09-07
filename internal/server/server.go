package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

type Server struct {
	ctx    context.Context
	srv    *http.Server
	logger *log.Logger
	wg     sync.WaitGroup
}

type Config struct {
	Addr   string
	Logger *log.Logger
}

var defaultConfig = &Config{
	Addr:   ":8081",
	Logger: log.Default(),
}

func (config *Config) MergeDefaults() {
	if config == defaultConfig {
		return
	}
	if config.Addr == "" {
		config.Addr = defaultConfig.Addr
	}
	if config.Logger == nil {
		config.Logger = defaultConfig.Logger
	}
}

func New(config *Config) *Server {
	if config == nil {
		config = defaultConfig
	}
	config.MergeDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = log.WithContext(ctx, config.Logger)

	server := &Server{
		ctx:    ctx,
		logger: config.Logger,
		srv: &http.Server{
			Addr:         config.Addr,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
			BaseContext: func(_ net.Listener) context.Context {
				return ctx
			},
		},
	}
	server.RegisterOnShutdown(cancel)

	return server
}

func (srv *Server) Context() context.Context {
	if srv.ctx != nil {
		return srv.ctx
	}
	return context.Background()
}

func (srv *Server) RegisterOnShutdown(f func()) {
	// If you directly RegisterOnShutdown, the registered func is executed on a goroutine.
	// Here we'll wrap each func and connect it to the server WaitGroup to guarantee they run
	// before the server shuts down
	srv.wg.Add(1)
	wf := func() {
		f()
		srv.wg.Done()
	}
	srv.srv.RegisterOnShutdown(wf)
}
func (srv *Server) SetHandler(handler http.Handler) {
	srv.srv.Handler = handler
}

func (srv *Server) serve() {
	srv.logger.Info("starting server", "addr", srv.srv.Addr)

	err := srv.srv.ListenAndServe()
	if err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		srv.logger.Fatal("unable to start server", "error", err)
	}
}

func (srv *Server) Serve() {
	go srv.serve()

	shutdown := make(chan os.Signal, 1)
	defer close(shutdown)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	<-shutdown

	graceful := make(chan bool, 1)
	defer close(graceful)

	timeout := 5 * time.Second
	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	srv.logger.Info(
		"started shutdown, active connections will be closed after timeout",
		"timeout", timeout,
	)

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		if err := srv.srv.Shutdown(timeoutCtx); err != nil {
			srv.logger.Fatal("unable to shutdown server", "error", err)
		}
	}()

	go func() {
		srv.wg.Wait()
		graceful <- true
	}()

	select {
	case <-timeoutCtx.Done():
		srv.logger.Error("shutdown timed out")

	case <-shutdown:
		srv.logger.Error("forced active sessions to exit early")

	case <-graceful:
		srv.logger.Info("shut down gracefully")
	}
}
