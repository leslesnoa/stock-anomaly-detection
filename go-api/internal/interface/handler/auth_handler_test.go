package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/user"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRegisterUsecase struct {
	err error
}

func (s stubRegisterUsecase) Handle(ctx context.Context, email, password string) error {
	return s.err
}

type stubLoginUsecase struct {
	token string
	err   error
}

func (s stubLoginUsecase) Handle(ctx context.Context, email, password string) (string, error) {
	return s.token, s.err
}

func TestAuthHandler_Register_Success(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: nil}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: user.ErrEmailAlreadyExists}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestAuthHandler_Register_InvalidEmail(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{err: usecase.ErrInvalidEmail}, stubLoginUsecase{})
	body, _ := json.Marshal(map[string]string{"email": "bad", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Register(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthHandler_Login_Success(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{}, stubLoginUsecase{token: "signed-token", err: nil})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "signed-token", resp["token"])
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	h := handler.NewAuthHandler(stubRegisterUsecase{}, stubLoginUsecase{err: usecase.ErrInvalidCredentials})
	body, _ := json.Marshal(map[string]string{"email": "a@example.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
