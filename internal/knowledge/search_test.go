package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type tokenStub struct{}

func (tokenStub) Token(context.Context, string) (string, error) { return "search-token", nil }

func TestSearchReturnsGroundingRecordsAndUsesBackendIdentity(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer search-token" || r.URL.Query().Get("api-version") != APIVersion {
			t.Error("missing backend identity or API version")
		}
		var query struct {
			Search string `json:"search"`
			Select string `json:"select"`
			Top    int    `json:"top"`
		}
		if json.NewDecoder(r.Body).Decode(&query) != nil || query.Search != "opening hours" || query.Top != 5 || query.Select != "content,title_b64" {
			t.Error("unexpected search request")
		}
		fmt.Fprintf(w, `{"value":[{"content":"Open weekdays","title_b64":%q}]}`, base64.StdEncoding.EncodeToString([]byte("guide.pdf")))
	}))
	defer api.Close()
	client := &Client{Endpoint: api.URL, Index: "approved", HTTP: api.Client(), Identity: tokenStub{}}
	results, err := client.Search(context.Background(), "opening hours")
	if err != nil || len(results) != 1 || results[0].Title != "guide.pdf" || results[0].Content != "Open weekdays" {
		t.Fatalf("results=%v err=%v", results, err)
	}
}

func TestSearchDistinguishesNoMatchFromMalformedUpstream(t *testing.T) {
	for _, body := range []string{`{"value":[]}`, `{}`, `{"value":null}`, `{"value":[{"content":"text","title_b64":"invalid base64!"}]}`} {
		t.Run(body, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer api.Close()
			client := &Client{Endpoint: api.URL, Index: "approved", HTTP: api.Client(), Identity: tokenStub{}}
			results, err := client.Search(context.Background(), "question")
			if body == `{"value":[]}` {
				if err != nil || results == nil || len(results) != 0 {
					t.Fatalf("valid no-match was not an empty array: %v %v", results, err)
				}
			} else if err == nil {
				t.Fatal("malformed response was treated as successful retrieval")
			}
		})
	}
}
