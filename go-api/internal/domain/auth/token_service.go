package auth

import "errors"

var ErrInvalidToken = errors.New("invalid token")

type TokenService interface {
	IssueToken(userID string) (string, error)
	VerifyToken(token string) (userID string, err error)
}
