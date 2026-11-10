package copilot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

type State struct {
	ConversationID string    `json:"conversationId"`
	Watermark      string    `json:"watermark"`
	Pending        bool      `json:"pending"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Client struct {
	Endpoint     string
	Secret       string
	HTTP         *http.Client
	PollInterval time.Duration
}

type Activity struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Locale    string `json:"locale,omitempty"`
	Name      string `json:"name,omitempty"`
	ReplyToID string `json:"replyToId,omitempty"`
	From      struct {
		ID   string `json:"id"`
		Role string `json:"role,omitempty"`
	} `json:"from"`
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = cloud.JSON(body)
		if err != nil {
			return err
		}
	}
	return cloud.Do(ctx, c.HTTP, "copilot", method, cloud.Base(c.Endpoint)+path, cloud.Bearer(c.Secret), data, out)
}

func (c *Client) Start(ctx context.Context) (State, error) {
	var response struct {
		ConversationID string `json:"conversationId"`
	}
	if err := c.call(ctx, http.MethodPost, "/conversations", nil, &response); err != nil {
		return State{}, err
	}
	if response.ConversationID == "" {
		return State{}, errors.New("copilot: missing conversation identifier")
	}
	return State{ConversationID: response.ConversationID}, nil
}

// Turn consumes opaque Direct Line watermarks, never activity IDs, as polling cursors.
// The configured Q&A topic must emit genesys.turn.complete after its final message.
func (c *Client) Turn(ctx context.Context, state State, userID, text, locale string) ([]string, State, error) {
	a := Activity{Type: "message", Text: text, Locale: locale}
	a.From.ID, a.From.Role = userID, "user"
	path := "/conversations/" + url.PathEscape(state.ConversationID) + "/activities"
	var posted struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, path, a, &posted); err != nil {
		return nil, state, err
	}
	if posted.ID == "" {
		return nil, state, errors.New("copilot: missing activity identifier")
	}
	interval := c.PollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	messages := []string{}
	answerBytes := 0
	seen := map[string]bool{}
	for {
		pollPath := path
		if state.Watermark != "" {
			pollPath += "?watermark=" + url.QueryEscape(state.Watermark)
		}
		var page struct {
			Activities []Activity `json:"activities"`
			Watermark  string     `json:"watermark"`
		}
		if err := c.call(ctx, http.MethodGet, pollPath, nil, &page); err != nil {
			return nil, state, err
		}
		complete := false
		for _, activity := range page.Activities {
			if activity.From.ID == userID || activity.From.Role == "user" {
				continue
			}
			if activity.ReplyToID != "" && activity.ReplyToID != posted.ID {
				continue
			}
			if activity.ID != "" && seen[activity.ID] {
				continue
			}
			if activity.ID != "" {
				seen[activity.ID] = true
			}
			switch activity.Type {
			case "message":
				if strings.TrimSpace(activity.Text) == "" {
					return nil, state, errors.New("copilot: non-text response is not supported in v1")
				}
				answerBytes += len(activity.Text)
				if answerBytes > 64000 {
					return nil, state, errors.New("copilot: response exceeds the v1 text limit")
				}
				messages = append(messages, activity.Text)
			case "event":
				if activity.Name == "genesys.turn.complete" {
					complete = true
				}
				if activity.Name == "handoff.initiate" || activity.Name == "genesys.turn.failed" {
					return nil, state, errors.New("copilot: topic failed or requested an unsupported handoff")
				}
			case "endOfConversation":
				return nil, state, errors.New("copilot: conversation ended before completing the Q&A turn")
			}
		}
		if page.Watermark != "" {
			state.Watermark = page.Watermark
		}
		if complete {
			if len(messages) == 0 || state.Watermark == "" {
				return nil, state, errors.New("copilot: completion event without an answer or watermark")
			}
			return messages, state, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, state, ctx.Err()
		case <-timer.C:
		}
	}
}
