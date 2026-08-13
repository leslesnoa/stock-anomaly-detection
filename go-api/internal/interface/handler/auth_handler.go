package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

type registerUsecase interface {
	Handle(ctx context.Context, email, password string) error
}

type loginUsecase interface {
	Handle(ctx context.Context, email, password string) (string, error)
}

type AuthHandler struct {
	register registerUsecase
	login    loginUsecase
}

func NewAuthHandler(register registerUsecase, login loginUsecase) *AuthHandler {
	return &AuthHandler{register: register, login: login}
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := h.register.Handle(r.Context(), req.Email, req.Password)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusCreated)
	case errors.Is(err, user.ErrEmailAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, usecase.ErrInvalidEmail), errors.Is(err, usecase.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, err := h.login.Handle(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, usecase.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}
