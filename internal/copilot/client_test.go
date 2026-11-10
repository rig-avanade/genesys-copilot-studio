package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTurnCollectsPagesAndKeepsOpaqueWatermark(t *testing.T) {
	polls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing direct line authorization")
		}
		switch r.Method {
		case http.MethodPost:
			var activity Activity
			if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
				t.Fatal(err)
			}
			if activity.Text != "question" || activity.From.ID != "dl_test" || activity.Locale != "en-US" {
				t.Errorf("unexpected posted activity: %+v", activity)
			}
			fmt.Fprint(w, `{"id":"conversation|opaque-activity-id"}`)
		case http.MethodGet:
			polls++
			expected := []string{"previous +/=", "next +/=", "last-page"}[polls-1]
			if r.URL.Query().Get("watermark") != expected {
				t.Errorf("watermark %q, want %q", r.URL.Query().Get("watermark"), expected)
			}
			switch polls {
			case 1:
				fmt.Fprint(w, `{"activities":[
					{"id":"user","type":"message","text":"question","from":{"id":"dl_test","role":"user"}},
					{"id":"typing","type":"typing","from":{"id":"bot"}},
					{"id":"old","type":"message","text":"old answer","replyToId":"another-turn","from":{"id":"bot"}},
					{"id":"answer-1","type":"message","text":"first part","from":{"id":"bot","role":"bot"}}
				],"watermark":"next +/="}`)
			case 2:
				fmt.Fprint(w, `{"activities":[
					{"id":"answer-1","type":"message","text":"first part","from":{"id":"bot"}},
					{"id":"answer-2","type":"message","text":"second part","from":{"id":"bot"}}
				],"watermark":"last-page"}`)
			case 3:
				fmt.Fprint(w, `{"activities":[{"id":"end","type":"event","name":"genesys.turn.complete","from":{"id":"bot"}}],"watermark":"final-opaque"}`)
			default:
				t.Fatal("unexpected extra poll")
			}
		}
	}))
	defer api.Close()
	c := &Client{Endpoint: api.URL, Secret: "test-secret", HTTP: api.Client(), PollInterval: time.Millisecond}
	messages, state, err := c.Turn(context.Background(), State{ConversationID: "conversation", Watermark: "previous +/="}, "dl_test", "question", "en-US")
	if err != nil || strings.Join(messages, "|") != "first part|second part" || state.Watermark != "final-opaque" || polls != 3 {
		t.Fatalf("messages=%v state=%+v polls=%d err=%v", messages, state, polls, err)
	}
}

func TestTurnDoesNotTreatPartialAnswerAsCompletion(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"id":"one"}`)
			return
		}
		fmt.Fprint(w, `{"activities":[{"id":"answer","type":"message","text":"partial","from":{"id":"bot"}}],"watermark":"one"}`)
	}))
	defer api.Close()
	c := &Client{Endpoint: api.URL, HTTP: api.Client(), PollInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	messages, _, err := c.Turn(ctx, State{ConversationID: "c"}, "user", "question", "")
	if err == nil || messages != nil {
		t.Fatalf("partial response falsely succeeded: %v %v", messages, err)
	}
}

func TestTurnRejectsUnsupportedAndFailedResponses(t *testing.T) {
	for _, response := range []string{
		`{"type":"message","attachments":[{}],"from":{"id":"bot"}}`,
		`{"type":"event","name":"handoff.initiate","from":{"id":"bot"}}`,
		`{"type":"event","name":"genesys.turn.failed","from":{"id":"bot"}}`,
		`{"type":"endOfConversation","from":{"id":"bot"}}`,
		`{"type":"event","name":"genesys.turn.complete","from":{"id":"bot"}}`,
	} {
		t.Run(response, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					fmt.Fprint(w, `{"id":"one"}`)
				} else {
					fmt.Fprintf(w, `{"activities":[%s],"watermark":"cursor"}`, response)
				}
			}))
			defer api.Close()
			c := &Client{Endpoint: api.URL, HTTP: api.Client()}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, _, err := c.Turn(ctx, State{ConversationID: "c"}, "user", "question", ""); err == nil {
				t.Fatal("expected explicit failure")
			}
		})
	}
}
