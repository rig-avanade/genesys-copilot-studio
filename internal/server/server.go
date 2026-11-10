package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/genesys"
	"github.com/rig-avanade/genesys-copilot-studio/internal/knowledge"
)

type Searcher interface {
	Search(context.Context, string) ([]knowledge.Result, error)
}

type Server struct {
	GenesysKey   string
	KnowledgeKey string
	TurnTimeout  time.Duration
	Genesys      *genesys.Handler
	Search       Searcher
	Log          *slog.Logger
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /genesys/utterance", s.authenticate("Authorization", "Bearer "+s.GenesysKey, s.utterance))
	mux.HandleFunc("POST /knowledge/search", s.authenticate("X-API-Key", s.KnowledgeKey, s.search))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			s.Log.Error("request identifier generation failed")
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Request-ID", hex.EncodeToString(id[:]))
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(header, expected string, next http.HandlerFunc) http.HandlerFunc {
	expectedHash := sha256.Sum256([]byte(expected))
	return func(w http.ResponseWriter, r *http.Request) {
		actual := sha256.Sum256([]byte(r.Header.Get(header)))
		if subtle.ConstantTimeCompare(actual[:], expectedHash[:]) != 1 {
			s.Log.Warn("authentication rejected", "path", r.URL.Path, "request_id", w.Header().Get("X-Request-ID"))
			s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func decode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		s.Log.Error("response encoding failed", "request_id", w.Header().Get("X-Request-ID"))
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(data, '\n')); err != nil {
		s.Log.Warn("response delivery failed", "request_id", w.Header().Get("X-Request-ID"))
	}
}

func (s *Server) utterance(w http.ResponseWriter, r *http.Request) {
	var request genesys.Request
	if err := decode(w, r, &request); err != nil {
		s.Log.Warn("invalid connector JSON", "request_id", w.Header().Get("X-Request-ID"))
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or oversized JSON request"})
		return
	}
	if err := s.Genesys.Validate(request); err != nil {
		s.Log.Warn("invalid connector request", "reason", err.Error(), "request_id", w.Header().Get("X-Request-ID"))
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.TurnTimeout)
	defer cancel()
	response, err := s.Genesys.Handle(ctx, request)
	if err != nil {
		id := w.Header().Get("X-Request-ID")
		s.Log.Error("connector turn failed", "request_id", id, "error", err.Error())
		response = genesys.Reply(request)
		response.BotState = "Failed"
		response.ErrorInfo = &genesys.ErrorInfo{
			Code: "TURN_FAILED", Message: "Unable to complete this turn. Start a new bot session. Reference: " + id,
		}
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Query string `json:"query"`
	}
	if err := decode(w, r, &request); err != nil || strings.TrimSpace(request.Query) == "" || len(request.Query) > 4000 {
		s.Log.Warn("invalid knowledge query", "request_id", w.Header().Get("X-Request-ID"))
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query must contain 1-4000 bytes"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	results, err := s.Search.Search(ctx, request.Query)
	if err != nil {
		s.Log.Error("knowledge search failed", "request_id", w.Header().Get("X-Request-ID"), "error", err.Error())
		s.writeJSON(w, http.StatusBadGateway, map[string]string{"error": "knowledge service unavailable"})
		return
	}
	s.writeJSON(w, http.StatusOK, results)
}
