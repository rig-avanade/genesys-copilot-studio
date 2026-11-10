package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

type tokenStub struct{}

func (tokenStub) Token(context.Context, string) (string, error) { return "private-token", nil }

func TestGraphEnumeratesNestedAndPaginatedFolder(t *testing.T) {
	var endpoint string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/root/children") && r.URL.Query().Get("page") == "":
			fmt.Fprintf(w, `{"value":[{"id":"nested","folder":{}}],"@odata.nextLink":%q}`, endpoint+"/drives/drive/items/root/children?page=2")
		case r.URL.Query().Get("page") == "2":
			fmt.Fprint(w, `{"value":[{"id":"a","name":"a.pdf","eTag":"1","size":20,"file":{}}]}`)
		case strings.HasSuffix(r.URL.Path, "/nested/children"):
			fmt.Fprint(w, `{"value":[{"id":"b","name":"b.docx","eTag":"2","size":30,"file":{}}]}`)
		default:
			t.Errorf("unexpected Graph path %s", r.URL.Path)
		}
	}))
	defer api.Close()
	endpoint = api.URL
	g := Graph{Endpoint: endpoint, DriveID: "drive", FolderID: "root", HTTP: api.Client(), Identity: tokenStub{}}
	files, err := g.List(context.Background())
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

func TestGraphRejectsIncompleteUnsafeAndUnsupportedListings(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"value":null}`,
		`{"value":[],"@odata.nextLink":"https://untrusted.invalid/steal-token"}`,
		`{"value":[{"id":"outside","remoteItem":{},"folder":{}}]}`,
		`{"value":[{"id":"file","name":"data.exe","size":2,"eTag":"1","file":{}}]}`,
		fmt.Sprintf(`{"value":[{"id":"file","name":"big.pdf","size":%d,"eTag":"1","file":{}}]}`, MaxFileBytes+1),
	} {
		t.Run(body, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer api.Close()
			g := Graph{Endpoint: api.URL, DriveID: "drive", FolderID: "folder", HTTP: api.Client(), Identity: tokenStub{}}
			if _, err := g.List(context.Background()); err == nil {
				t.Fatal("unsafe listing accepted")
			}
		})
	}
}

func TestDownloadDoesNotForwardGraphAuthorization(t *testing.T) {
	download := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Graph token leaked to download endpoint")
		}
		fmt.Fprint(w, "file content")
	}))
	defer download.Close()
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing Graph token")
		}
		w.Header().Set("Location", download.URL+"/document?preauthenticated=example")
		w.WriteHeader(http.StatusFound)
	}))
	defer graph.Close()
	client := cloud.NewHTTPClient()
	client.Transport = download.Client().Transport
	g := Graph{Endpoint: graph.URL, DriveID: "drive", SharePointHost: strings.TrimPrefix(download.URL, "https://"),
		HTTP: client, Identity: tokenStub{}}
	data, err := g.Download(context.Background(), File{ID: "file", Size: 12, ETag: "etag"})
	if err != nil || string(data) != "file content" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}
