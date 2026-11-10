package main

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
	"github.com/rig-avanade/genesys-copilot-studio/internal/config"
	"github.com/rig-avanade/genesys-copilot-studio/internal/ingest"
	"github.com/rig-avanade/genesys-copilot-studio/internal/knowledge"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("ingestion failed; no successful sync claimed", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	values := map[string]string{}
	var errs []error
	for name, loader := range map[string]func(string) (string, error){
		"STORAGE_ENDPOINT": config.URL, "SEARCH_ENDPOINT": config.URL,
		"KNOWLEDGE_CONTAINER": config.ResourceName, "SESSION_CONTAINER": config.ResourceName, "SEARCH_INDEX": config.ResourceName,
		"SHAREPOINT_DRIVE_ID": config.Required, "SHAREPOINT_FOLDER_ID": config.Required, "SHAREPOINT_HOST": config.Required,
	} {
		value, err := loader(name)
		values[name] = value
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	host, err := url.Parse("https://" + values["SHAREPOINT_HOST"])
	if err != nil || host.Host != values["SHAREPOINT_HOST"] || host.User != nil || host.Port() != "" {
		return errors.New("SHAREPOINT_HOST must be a hostname without a scheme, port, or path")
	}
	if values["KNOWLEDGE_CONTAINER"] == values["SESSION_CONTAINER"] {
		return errors.New("knowledge and session containers must be different")
	}
	identity, err := cloud.NewIdentity()
	if err != nil {
		return err
	}
	client := cloud.NewHTTPClient()
	target := &blob.Client{Endpoint: values["STORAGE_ENDPOINT"], Container: values["KNOWLEDGE_CONTAINER"], HTTP: client, Identity: identity}
	control := &blob.Client{Endpoint: values["STORAGE_ENDPOINT"], Container: values["SESSION_CONTAINER"], HTTP: client, Identity: identity}
	source := &ingest.Graph{
		Endpoint: "https://graph.microsoft.com/v1.0", DriveID: values["SHAREPOINT_DRIVE_ID"],
		FolderID: values["SHAREPOINT_FOLDER_ID"], SharePointHost: values["SHAREPOINT_HOST"], HTTP: client, Identity: identity,
	}
	search := &knowledge.Client{Endpoint: values["SEARCH_ENDPOINT"], Index: values["SEARCH_INDEX"], HTTP: client, Identity: identity}
	sync := &ingest.Sync{Source: source, Destination: target, DriveID: source.DriveID}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	binding := session.Key(source.DriveID, source.FolderID, target.Endpoint, target.Container, search.Endpoint, search.Index)
	return ingest.BoundRun(ctx, control, binding, func(ctx context.Context) error {
		summary, err := sync.Run(ctx)
		if err != nil {
			return err
		}
		if err := search.RunIndexer(ctx); err != nil {
			return err
		}
		log.Info("folder synchronized; indexer accepted, completion still pending",
			"uploaded", summary.Uploaded, "unchanged", summary.Unchanged, "deleted", summary.Deleted)
		return nil
	})
}
