package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rig-avanade/genesys-copilot-studio/internal/blob"
	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
	"github.com/rig-avanade/genesys-copilot-studio/internal/copilot"
	"github.com/rig-avanade/genesys-copilot-studio/internal/genesys"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

type azureToken struct{}

func (azureToken) Token(context.Context, string) (string, error) { return "azure-token", nil }

func TestConnectorHTTPRoundTripPersistsSessionAcrossServiceInstances(t *testing.T) {
	var mu sync.Mutex
	blobs := map[string][]byte{}
	leases := map[string]bool{}
	starts, turns, polls := 0, 0, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/sessions/") {
			if r.Header.Get("Authorization") != "Bearer azure-token" {
				t.Error("missing storage identity")
			}
			name := r.URL.Path
			switch {
			case r.URL.Query().Get("comp") == "lease":
				switch r.Header.Get("x-ms-lease-action") {
				case "acquire":
					if leases[name] {
						w.WriteHeader(http.StatusConflict)
						return
					}
					leases[name] = true
					w.Header().Set("x-ms-lease-id", "lease-id")
				case "release":
					leases[name] = false
				}
				w.WriteHeader(http.StatusCreated)
			case r.Method == http.MethodPut:
				if r.Header.Get("If-None-Match") == "*" {
					if _, exists := blobs[name]; exists {
						w.WriteHeader(http.StatusPreconditionFailed)
						return
					}
				} else if r.Header.Get("x-ms-lease-id") != "lease-id" || !leases[name] {
					t.Error("session write without lease")
				}
				data, _ := io.ReadAll(r.Body)
				blobs[name] = data
				w.WriteHeader(http.StatusCreated)
			case r.Method == http.MethodGet:
				w.Write(blobs[name])
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer bot-secret" {
			t.Error("missing Direct Line credential")
		}
		switch {
		case r.URL.Path == "/directline/conversations":
			starts++
			fmt.Fprint(w, `{"conversationId":"copilot-1"}`)
		case r.Method == http.MethodPost:
			turns++
			var pending copilot.State
			for _, data := range blobs {
				json.Unmarshal(data, &pending)
			}
			if !pending.Pending {
				t.Error("message posted before persisting pending marker")
			}
			fmt.Fprintf(w, `{"id":"activity-%d"}`, turns)
		case r.Method == http.MethodGet:
			polls++
			expected := ""
			if polls > 1 {
				expected = fmt.Sprintf("cursor-%d", polls-1)
			}
			if r.URL.Query().Get("watermark") != expected {
				t.Errorf("poll cursor %q, want %q", r.URL.Query().Get("watermark"), expected)
			}
			fmt.Fprintf(w, `{"activities":[
				{"id":"reply-%d","type":"message","text":"approved answer","from":{"id":"bot"}},
				{"id":"event-%d","type":"event","name":"genesys.turn.complete","from":{"id":"bot"}}
			],"watermark":"cursor-%d"}`, turns, turns, polls)
		}
	}))
	defer api.Close()

	body := `{"integrationId":"integration","botId":"bot","botVersion":"v1","botSessionId":"session","conversationId":"conversation","languageCode":"en-US","inputMessage":{"type":"Text","text":"question"}}`
	for range 2 {
		app := testServer()
		client := cloud.NewHTTPClient()
		app.Genesys.Sessions = &session.BlobStore{Blobs: &blob.Client{
			Endpoint: api.URL, Container: "sessions", HTTP: client, Identity: azureToken{},
		}}
		app.Genesys.Bot = &copilot.Client{Endpoint: api.URL + "/directline", Secret: "bot-secret", HTTP: client}
		service := httptest.NewServer(app.Handler())
		req, _ := http.NewRequest(http.MethodPost, service.URL+"/genesys/utterance", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer genesys-key")
		resp, err := service.Client().Do(req)
		if err != nil {
			service.Close()
			t.Fatal(err)
		}
		var result genesys.Response
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		service.Close()
		if err != nil || result.BotState != "MoreData" || len(result.ReplyMessages) != 1 || result.ReplyMessages[0].Text != "approved answer" {
			t.Fatalf("connector response %+v err=%v", result, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || turns != 2 || polls != 2 {
		t.Fatalf("conversation not reused: starts=%d turns=%d polls=%d", starts, turns, polls)
	}
	for _, data := range blobs {
		if bytes.Contains(data, []byte("question")) || bytes.Contains(data, []byte("approved answer")) || bytes.Contains(data, []byte("bot-secret")) {
			t.Fatal("session storage contains message text or credentials")
		}
	}
}
