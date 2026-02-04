package database

import (
	"context"

	"dr/server/types"
)

func CreateFeedback(ctx context.Context, feedback *types.Feedback) error {
	return db.Create(feedback).Error
}
