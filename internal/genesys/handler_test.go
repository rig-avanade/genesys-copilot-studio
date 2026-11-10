package genesys

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rig-avanade/genesys-copilot-studio/internal/copilot"
)

type memoryStore struct {
	mu     sync.Mutex
	states map[string]copilot.State
}

func (s *memoryStore) WithSession(ctx context.Context, key string, fn func(context.Context, *copilot.State, func() error) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[key]
	return fn(ctx, &state, func() error { s.states[key] = state; return nil })
}

type fakeBot struct {
	starts, turns int
	fail          bool
	users         []string
	states        []copilot.State
}

func (b *fakeBot) Start(context.Context) (copilot.State, error) {
	b.starts++
	return copilot.State{ConversationID: "copilot-conversation"}, nil
}

func (b *fakeBot) Turn(_ context.Context, state copilot.State, user, _, _ string) ([]string, copilot.State, error) {
	b.turns++
	b.users = append(b.users, user)
	b.states = append(b.states, state)
	if b.fail {
		return nil, state, errors.New("upstream timeout")
	}
	state.Watermark = "last-cursor"
	return []string{"answer"}, state, nil
}

func request(session string) Request {
	return Request{IntegrationID: "integration", BotID: "bot", BotVersion: "v1", BotSessionID: session,
		ConversationID: "genesys-conversation", LanguageCode: "en-US", InputMessage: Message{Type: "Text", Text: "question"}}
}

func TestConversationContinuityIsolationAndRestart(t *testing.T) {
	store := &memoryStore{states: map[string]copilot.State{}}
	bot := &fakeBot{}
	handler := &Handler{IntegrationID: "integration", BotID: "bot", BotVersion: "v1", Sessions: store, Bot: bot}
	for _, id := range []string{"one", "one", "two"} {
		if err := handler.Validate(request(id)); err != nil {
			t.Fatal(err)
		}
		response, err := handler.Handle(context.Background(), request(id))
		if err != nil || response.BotState != "MoreData" || response.ReplyMessages[0].Text != "answer" {
			t.Fatalf("response=%+v err=%v", response, err)
		}
		// Reconstruct the handler, as after a process restart, without losing the backing store.
		handler = &Handler{IntegrationID: "integration", BotID: "bot", BotVersion: "v1", Sessions: store, Bot: bot}
	}
	if bot.starts != 2 || bot.states[1].Watermark != "last-cursor" || bot.users[0] != bot.users[1] || bot.users[1] == bot.users[2] {
		t.Fatalf("conversation mapping failed: %+v", bot)
	}
}

func TestUncertainTurnIsNotAutomaticallyReposted(t *testing.T) {
	store := &memoryStore{states: map[string]copilot.State{}}
	bot := &fakeBot{fail: true}
	handler := &Handler{Sessions: store, Bot: bot}
	for range 2 {
		if _, err := handler.Handle(context.Background(), request("one")); err == nil {
			t.Fatal("expected failed turn")
		}
	}
	if bot.turns != 1 || bot.starts != 1 {
		t.Fatalf("uncertain turn was resent: %+v", bot)
	}
}
