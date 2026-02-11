package context

import (
	"context"
	"dr/server/log"
)

func InitContext(ctx context.Context, requestID string) {

	log.Info(ctx, "Context: ", requestID)
	// context, _ := context.WithValue(ctx, "requestID", requestID)

}
