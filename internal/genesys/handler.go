package genesys

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/copilot"
	"github.com/rig-avanade/genesys-copilot-studio/internal/session"
)

type Message struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Request struct {
	BotID          string  `json:"botId"`
	BotVersion     string  `json:"botVersion"`
	BotSessionID   string  `json:"botSessionId"`
	IntegrationID  string  `json:"integrationId"`
	ConversationID string  `json:"conversationId"`
	LanguageCode   string  `json:"languageCode"`
	InputMessage   Message `json:"inputMessage"`
}

type ErrorInfo struct {
	Code    string `json:"errorCode"`
	Message string `json:"errorMessage"`
}

type Response struct {
	BotID         string     `json:"botId"`
	BotVersion    string     `json:"botVersion"`
	BotSessionID  string     `json:"botSessionId"`
	BotState      string     `json:"botState"`
	LanguageCode  string     `json:"languageCode"`
	ReplyMessages []Message  `json:"replyMessages"`
	ErrorInfo     *ErrorInfo `json:"errorInfo,omitempty"`
}

type Bot interface {
	Start(context.Context) (copilot.State, error)
	Turn(context.Context, copilot.State, string, string, string) ([]string, copilot.State, error)
}

type Handler struct {
	IntegrationID string
	BotID         string
	BotVersion    string
	Sessions      session.Store
	Bot           Bot
}

func (h *Handler) Validate(r Request) error {
	if r.IntegrationID != h.IntegrationID || r.BotID != h.BotID || r.BotVersion != h.BotVersion {
		return errors.New("unknown integration, bot, or bot version")
	}
	if r.BotSessionID == "" || len(r.BotSessionID) > 256 || r.ConversationID == "" || len(r.ConversationID) > 256 {
		return errors.New("botSessionId and conversationId are required and must be at most 256 bytes")
	}
	if !strings.EqualFold(r.InputMessage.Type, "text") || strings.TrimSpace(r.InputMessage.Text) == "" || len(r.InputMessage.Text) > 16000 {
		return errors.New("v1 requires a nonempty text message of at most 16000 bytes")
	}
	if r.LanguageCode == "" || len(r.LanguageCode) > 32 {
		return errors.New("languageCode is required and must be at most 32 bytes")
	}
	return nil
}

func Reply(r Request) Response {
	return Response{
		BotID: r.BotID, BotVersion: r.BotVersion, BotSessionID: r.BotSessionID,
		LanguageCode: r.LanguageCode, BotState: "MoreData", ReplyMessages: []Message{},
	}
}

func (h *Handler) Handle(ctx context.Context, r Request) (Response, error) {
	response := Reply(r)
	key := session.Key(r.IntegrationID, r.BotID, r.BotVersion, r.ConversationID, r.BotSessionID)
	err := h.Sessions.WithSession(ctx, key, func(ctx context.Context, state *copilot.State, save func() error) error {
		if !state.UpdatedAt.IsZero() && time.Since(state.UpdatedAt) > 24*time.Hour {
			*state = copilot.State{}
		}
		if state.Pending {
			return errors.New("previous turn has an uncertain outcome; start a new Genesys bot session")
		}
		if state.ConversationID == "" {
			started, err := h.Bot.Start(ctx)
			if err != nil {
				return err
			}
			*state = started
		}
		// Persist before posting. Never automatically replay a possibly accepted user message.
		state.Pending, state.UpdatedAt = true, time.Now().UTC()
		if err := save(); err != nil {
			return err
		}
		messages, updated, err := h.Bot.Turn(ctx, *state, "dl_"+key, r.InputMessage.Text, r.LanguageCode)
		if err != nil {
			return err
		}
		total := 0
		for _, text := range messages {
			total += len(text)
		}
		if len(messages) == 0 || total > 64000 {
			return errors.New("copilot response is empty or exceeds the v1 text limit")
		}
		*state = updated
		state.Pending, state.UpdatedAt = false, time.Now().UTC()
		if err := save(); err != nil {
			return err
		}
		for _, text := range messages {
			response.ReplyMessages = append(response.ReplyMessages, Message{Type: "Text", Text: text})
		}
		return nil
	})
	return response, err
}
