package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
	"github.com/rig-avanade/genesys-copilot-studio/internal/config"
	"github.com/rig-avanade/genesys-copilot-studio/internal/knowledge"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(); err != nil {
		log.Error("search setup failed", "error", err.Error())
		os.Exit(1)
	}
	log.Info("search resources configured; inspect indexer status after ingestion")
}

func run() error {
	endpoint, e1 := config.URL("SEARCH_ENDPOINT")
	index, e2 := config.ResourceName("SEARCH_INDEX")
	container, e3 := config.ResourceName("KNOWLEDGE_CONTAINER")
	resourceID, e4 := config.Required("STORAGE_RESOURCE_ID")
	if err := errors.Join(e1, e2, e3, e4); err != nil {
		return err
	}
	if !strings.HasPrefix(resourceID, "/subscriptions/") || !strings.Contains(resourceID, "/providers/Microsoft.Storage/storageAccounts/") ||
		strings.ContainsAny(resourceID, ";\r\n") {
		return errors.New("STORAGE_RESOURCE_ID must be the full Azure Storage account resource ID")
	}
	identity, err := cloud.NewIdentity()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := &knowledge.Client{Endpoint: endpoint, Index: index, HTTP: cloud.NewHTTPClient(), Identity: identity}
	return client.Setup(ctx, resourceID, container)
}
