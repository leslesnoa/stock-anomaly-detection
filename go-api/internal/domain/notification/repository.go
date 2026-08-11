package notification

import "context"

type Repository interface {
	Save(ctx context.Context, n Notification) error
}
