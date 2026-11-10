package ingest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

const Prefix = "approved/"

type Destination interface {
	List(context.Context, string) ([]blob.Item, error)
	Put(context.Context, string, []byte, http.Header) error
	Delete(context.Context, string) error
}

type Summary struct {
	Uploaded  int `json:"uploaded"`
	Unchanged int `json:"unchanged"`
	Deleted   int `json:"deleted"`
}

type Sync struct {
	Source      Source
	Destination Destination
	DriveID     string
}

// Run never prunes until the entire source enumeration and every upload succeed.
func (s *Sync) Run(ctx context.Context) (Summary, error) {
	var summary Summary
	files, err := s.Source.List(ctx)
	if err != nil {
		return summary, err
	}
	blobs, err := s.Destination.List(ctx, Prefix)
	if err != nil {
		return summary, err
	}
	existing := map[string]blob.Item{}
	for _, item := range blobs {
		if !strings.HasPrefix(item.Name, Prefix) {
			return summary, errors.New("refusing to modify blobs outside the approved prefix")
		}
		existing[item.Name] = item
	}
	desired := map[string]bool{}
	for _, file := range files {
		name := Prefix + session.Key(s.DriveID, file.ID) + strings.ToLower(path.Ext(file.Name))
		if desired[name] {
			return summary, errors.New("duplicate source file in sync snapshot")
		}
		desired[name] = true
		etag := base64.StdEncoding.EncodeToString([]byte(file.ETag))
		if old, ok := existing[name]; ok && old.Metadata.SourceETag == etag {
			summary.Unchanged++
			continue
		}
		data, err := s.Source.Download(ctx, file)
		if err != nil {
			return summary, err
		}
		headers := make(http.Header)
		headers.Set("Content-Type", "application/octet-stream")
		headers.Set("x-ms-meta-sourceetag", etag)
		headers.Set("x-ms-meta-title_b64", base64.StdEncoding.EncodeToString([]byte(file.Name)))
		if err := s.Destination.Put(ctx, name, data, headers); err != nil {
			return summary, err
		}
		summary.Uploaded++
	}
	for _, item := range blobs {
		if !desired[item.Name] {
			if err := s.Destination.Delete(ctx, item.Name); err != nil {
				return summary, err
			}
			summary.Deleted++
		}
	}
	return summary, nil
}

func BoundRun(ctx context.Context, control *blob.Client, binding string, run func(context.Context) error) error {
	const lock = "knowledge-sync.json"
	return control.WithLease(ctx, lock, func(ctx context.Context, lease *blob.Lease) error {
		data, err := control.Get(ctx, lock)
		if err != nil {
			return err
		}
		var manifest struct {
			Source string `json:"source"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			return errors.New("invalid knowledge source binding")
		}
		if manifest.Source != "" && manifest.Source != binding {
			return errors.New("source folder or destination changed: use new containers and a new search index")
		}
		manifest.Source = binding
		data, err = json.Marshal(manifest)
		if err != nil {
			return err
		}
		if err := lease.Put(ctx, data); err != nil {
			return err
		}
		return run(ctx)
	})
}
