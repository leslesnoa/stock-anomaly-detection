package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stretchr/testify/assert"
)

type stubTokenVerifier struct {
	userID string
	err    error
}

func (s stubTokenVerifier) VerifyToken(token string) (string, error) {
	return s.userID, s.err
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := handler.RequireAuth(stubTokenVerifier{}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mw := handler.RequireAuth(stubTokenVerifier{err: assert.AnError}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_Success(t *testing.T) {
	called := false
	next := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}
	mw := handler.RequireAuth(stubTokenVerifier{userID: "user-1"}, next)

	req := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rec := httptest.NewRecorder()
	mw(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}
