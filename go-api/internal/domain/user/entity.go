package user

import "time"

type User struct {
	ID              string
	Email           string
	PasswordHash    string
	SlackWebhookURL *string
	CreatedAt       time.Time
}
