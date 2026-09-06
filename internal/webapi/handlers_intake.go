package webapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/intake"
)

type startSessionRequest struct {
	Message string `json:"message" validate:"required"`
}

type messageRequest struct {
	Message string `json:"message" validate:"required"`
}

type answerRequest struct {
	Answers map[string]intake.Answer `json:"answers" validate:"min=1"`
}

// handleStartSession は新しい対話を始める（FR-CHT-1）。
// セッション ID はサーバー側で採番し、以降のやり取りはそれを使う。
func (s *Server) handleStartSession(c *echo.Context) error {
	var req startSessionRequest
	if err := bindAndValidate(c, &req); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	turn, err := s.Agent.Start(c.Request().Context(), s.newID(), req.Message)
	if err != nil {
		return writeError(c, http.StatusInternalServerError, err)
	}
	return writeJSON(c, http.StatusOK, turn)
}

// handleSay は利用者の自由入力を対話に足す。
func (s *Server) handleSay(c *echo.Context) error {
	id := c.Param("id")
	var req messageRequest
	if err := bindAndValidate(c, &req); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	turn, err := s.Agent.Say(c.Request().Context(), id, req.Message)
	if err != nil {
		return writeSessionError(c, err)
	}
	return writeJSON(c, http.StatusOK, turn)
}

// handleAnswer は選択 UI で得た回答を返して対話を再開する（FR-CHT-1）。
func (s *Server) handleAnswer(c *echo.Context) error {
	id := c.Param("id")
	var req answerRequest
	if err := bindAndValidate(c, &req); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	turn, err := s.Agent.Answer(c.Request().Context(), id, req.Answers)
	if err != nil {
		return writeSessionError(c, err)
	}
	return writeJSON(c, http.StatusOK, turn)
}

func writeSessionError(c *echo.Context, err error) error {
	if errors.Is(err, intake.ErrSessionNotFound) {
		return writeError(c, http.StatusNotFound, err)
	}
	return writeError(c, http.StatusInternalServerError, err)
}
