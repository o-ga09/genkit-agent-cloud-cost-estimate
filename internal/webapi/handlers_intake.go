package webapi

import (
	"errors"
	"net/http"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

type startSessionRequest struct {
	Message string `json:"message"`
}

type messageRequest struct {
	Message string `json:"message"`
}

type answerRequest struct {
	Answers map[string]intake.Answer `json:"answers"`
}

// handleStartSession は新しい対話を始める（FR-CHT-1）。
// セッション ID はサーバー側で採番し、以降のやり取りはそれを使う。
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req startSessionRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, errors.New("message は必須です"))
		return
	}
	turn, err := s.Agent.Start(r.Context(), s.newID(), req.Message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, turn)
}

// handleSay は利用者の自由入力を対話に足す。
func (s *Server) handleSay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req messageRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, errors.New("message は必須です"))
		return
	}
	turn, err := s.Agent.Say(r.Context(), id, req.Message)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, turn)
}

// handleAnswer は選択 UI で得た回答を返して対話を再開する（FR-CHT-1）。
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req answerRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Answers) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("answers は必須です"))
		return
	}
	turn, err := s.Agent.Answer(r.Context(), id, req.Answers)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, turn)
}

func writeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, intake.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
