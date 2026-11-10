package blob

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

type tokenStub struct{}

func (tokenStub) Token(context.Context, string) (string, error) { return "token", nil }

func TestLeaseRejectsConcurrentWriterAndReleasesAfterError(t *testing.T) {
	var mu sync.Mutex
	created, held, releases := false, false, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("comp") != "lease" {
			if created {
				w.WriteHeader(http.StatusPreconditionFailed)
			} else {
				created = true
				w.WriteHeader(http.StatusCreated)
			}
			return
		}
		switch r.Header.Get("x-ms-lease-action") {
		case "acquire":
			if held {
				w.WriteHeader(http.StatusConflict)
				return
			}
			held = true
			w.Header().Set("x-ms-lease-id", "one")
		case "release":
			if r.Header.Get("x-ms-lease-id") != "one" {
				t.Error("wrong lease released")
			}
			held = false
			releases++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	client := &Client{Endpoint: api.URL, Container: "sessions", HTTP: cloud.NewHTTPClient(), Identity: tokenStub{}}
	entered, finish, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	workErr := errors.New("work failed")
	go func() {
		result <- client.WithLease(context.Background(), "one.json", func(context.Context, *Lease) error {
			close(entered)
			<-finish
			return workErr
		})
	}()
	<-entered
	err := client.WithLease(context.Background(), "one.json", func(context.Context, *Lease) error {
		t.Error("concurrent writer entered the protected section")
		return nil
	})
	close(finish)
	firstErr := <-result
	if !errors.Is(err, ErrBusy) || !errors.Is(firstErr, workErr) {
		t.Fatalf("concurrent error=%v first error=%v", err, firstErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if held || releases != 1 {
		t.Fatalf("lease not released: held=%v releases=%d", held, releases)
	}
}

func TestBlobListReadsAllPagesAndRejectsOutOfScopeResults(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(fmt.Sprint(outside), func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("marker") == "" {
					fmt.Fprint(w, `<EnumerationResults><Blobs><Blob><Name>approved/one.pdf</Name><Metadata><sourceetag>encoded-etag</sourceetag></Metadata></Blob></Blobs><NextMarker>page2</NextMarker></EnumerationResults>`)
					return
				}
				name := "approved/two.pdf"
				if outside {
					name = "sessions/private.json"
				}
				fmt.Fprintf(w, `<EnumerationResults><Blobs><Blob><Name>%s</Name></Blob></Blobs></EnumerationResults>`, name)
			}))
			defer api.Close()
			client := &Client{Endpoint: api.URL, Container: "knowledge", HTTP: cloud.NewHTTPClient(), Identity: tokenStub{}}
			items, err := client.List(context.Background(), "approved/")
			if outside {
				if err == nil {
					t.Fatal("out-of-scope blob accepted")
				}
			} else if err != nil || len(items) != 2 || items[0].Metadata.SourceETag != "encoded-etag" {
				t.Fatalf("items=%v err=%v", items, err)
			}
		})
	}
}
