package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/genesys"
	"github.com/rig-avanade/genesys-copilot-studio/internal/knowledge"
)

type fakeSearch struct{ fail bool }

func (s fakeSearch) Search(context.Context, string) ([]knowledge.Result, error) {
	if s.fail {
		return nil, errors.New("upstream failure")
	}
	return []knowledge.Result{{Content: "approved answer", Title: "guide.pdf"}}, nil
}

func testServer() *Server {
	return &Server{
		GenesysKey: "genesys-key", KnowledgeKey: "knowledge-key", TurnTimeout: time.Second,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Genesys: &genesys.Handler{IntegrationID: "integration", BotID: "bot", BotVersion: "v1"},
		Search:  fakeSearch{},
	}
}

func TestEndpointsUseSeparateCredentialsAndRejectInvalidInput(t *testing.T) {
	for _, test := range []struct {
		path, header, key, body string
		status                  int
	}{
		{"/knowledge/search", "X-API-Key", "knowledge-key", `{"query":"question"}`, 200},
		{"/knowledge/search", "X-API-Key", "genesys-key", `{"query":"question"}`, 401},
		{"/genesys/utterance", "Authorization", "Bearer knowledge-key", `{}`, 401},
		{"/genesys/utterance", "Authorization", "Bearer genesys-key", `{}`, 400},
		{"/knowledge/search", "X-API-Key", "knowledge-key", `{"query":" "}`, 400},
		{"/knowledge/search", "X-API-Key", "knowledge-key", `{"query":"question"}{}`, 400},
		{"/knowledge/search", "X-API-Key", "knowledge-key", `{"query":"` + strings.Repeat("x", 65536) + `"}`, 400},
	} {
		t.Run(test.path+test.body[:2]+test.key, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			r.Header.Set(test.header, test.key)
			w := httptest.NewRecorder()
			testServer().Handler().ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("got %d %s, expected %d", w.Code, w.Body.String(), test.status)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing response safety headers")
			}
		})
	}
}

func TestSearchFailureIsNotAnEmptySuccessfulResult(t *testing.T) {
	app := testServer()
	app.Search = fakeSearch{fail: true}
	r := httptest.NewRequest(http.MethodPost, "/knowledge/search", strings.NewReader(`{"query":"question"}`))
	r.Header.Set("X-API-Key", "knowledge-key")
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "upstream failure") {
		t.Fatalf("unexpected error response: %d %s", w.Code, w.Body.String())
	}
}
