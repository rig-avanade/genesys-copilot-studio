package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
	"github.com/rig-avanade/genesys-copilot-studio/internal/config"
	"github.com/rig-avanade/genesys-copilot-studio/internal/copilot"
	"github.com/rig-avanade/genesys-copilot-studio/internal/genesys"
	"github.com/rig-avanade/genesys-copilot-studio/internal/knowledge"
	"github.com/rig-avanade/genesys-copilot-studio/internal/server"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("middleware stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return err
	}
	identity, err := cloud.NewIdentity()
	if err != nil {
		return err
	}
	client := cloud.NewHTTPClient()
	app := &server.Server{
		GenesysKey: cfg.GenesysKey, KnowledgeKey: cfg.KnowledgeKey, TurnTimeout: cfg.TurnTimeout, Log: log,
		Genesys: &genesys.Handler{
			IntegrationID: cfg.IntegrationID, BotID: cfg.BotID, BotVersion: cfg.BotVersion,
			Sessions: &session.BlobStore{Blobs: &blob.Client{
				Endpoint: cfg.StorageEndpoint, Container: cfg.SessionContainer, HTTP: client, Identity: identity,
			}},
			Bot: &copilot.Client{Endpoint: cfg.DirectLineEndpoint, Secret: cfg.DirectLineSecret, HTTP: client},
		},
		Search: &knowledge.Client{Endpoint: cfg.SearchEndpoint, Index: cfg.SearchIndex, HTTP: client, Identity: identity},
	}
	httpServer := &http.Server{
		Addr: ":" + cfg.Port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: cfg.TurnTimeout + 10*time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- httpServer.ListenAndServe() }()
	log.Info("middleware listening", "port", cfg.Port)
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.TurnTimeout+5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
