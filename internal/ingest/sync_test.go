package ingest

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

type sourceStub struct {
	files   []File
	listErr error
	failID  string
}

func (s sourceStub) List(context.Context) ([]File, error) { return s.files, s.listErr }
func (s sourceStub) Download(_ context.Context, f File) ([]byte, error) {
	if f.ID == s.failID {
		return nil, errors.New("download failed")
	}
	return []byte("document content"), nil
}

type destinationStub struct {
	items    []blob.Item
	uploaded []string
	deleted  []string
	failPut  bool
}

func (d *destinationStub) List(context.Context, string) ([]blob.Item, error) { return d.items, nil }
func (d *destinationStub) Put(_ context.Context, name string, _ []byte, h http.Header) error {
	if d.failPut {
		return errors.New("upload failed")
	}
	if h.Get("x-ms-meta-title_b64") == "" || h.Get("x-ms-meta-sourceetag") == "" {
		return errors.New("missing search metadata")
	}
	d.uploaded = append(d.uploaded, name)
	return nil
}
func (d *destinationStub) Delete(_ context.Context, name string) error {
	d.deleted = append(d.deleted, name)
	return nil
}

func TestSyncUpdatesAndPrunesOnlyApprovedSnapshot(t *testing.T) {
	retained := blob.Item{Name: Prefix + session.Key("drive", "retained") + ".pdf"}
	retained.Metadata.SourceETag = base64.StdEncoding.EncodeToString([]byte("etag-1"))
	destination := &destinationStub{items: []blob.Item{retained, {Name: Prefix + "removed.pdf"}}}
	s := Sync{DriveID: "drive", Destination: destination, Source: sourceStub{files: []File{
		{ID: "retained", Name: "retained.pdf", ETag: "etag-1"},
		{ID: "new", Name: "new.docx", ETag: "etag-2"},
	}}}
	result, err := s.Run(context.Background())
	if err != nil || result.Uploaded != 1 || result.Unchanged != 1 || result.Deleted != 1 ||
		len(destination.deleted) != 1 || destination.deleted[0] != Prefix+"removed.pdf" {
		t.Fatalf("summary=%+v deleted=%v err=%v", result, destination.deleted, err)
	}
}

func TestSyncNeverPrunesOnPartialFailure(t *testing.T) {
	for _, stage := range []string{"list", "download", "upload"} {
		t.Run(stage, func(t *testing.T) {
			source := sourceStub{files: []File{{ID: "new", Name: "new.txt", ETag: "1"}}}
			destination := &destinationStub{items: []blob.Item{{Name: Prefix + "existing.txt"}}}
			switch stage {
			case "list":
				source.listErr = errors.New("pagination failed")
			case "download":
				source.failID = "new"
			case "upload":
				destination.failPut = true
			}
			s := Sync{DriveID: "drive", Source: source, Destination: destination}
			if _, err := s.Run(context.Background()); err == nil || len(destination.deleted) != 0 {
				t.Fatalf("failure pruned documents: deleted=%v err=%v", destination.deleted, err)
			}
		})
	}
}
