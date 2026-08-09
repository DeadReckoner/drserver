package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// healthPingTimeout bounds the readiness probe's database ping. Deliberately
// shorter than any sane external check interval: a probe that hangs is worse
// than one that fails, because the checker can't tell "slow" from "wedged".
const healthPingTimeout = 2 * time.Second

// HealthHandler serves the liveness and readiness probes.
//
// The two are split on purpose, because they answer different questions and
// have different consequences:
//
//   - /health  (liveness)  — is the process running? Never touches the
//     database. A failure here means the binary is wedged and restarting is
//     the right remedy.
//   - /healthz (readiness) — can this instance actually serve requests? Pings
//     the database. A failure means "stop sending me traffic", and restarting
//     would NOT help, because the fault is downstream.
//
// Pointing a restart-triggering check at the readiness probe is the classic
// way to turn a brief database blip into a crash loop, so keep liveness
// shallow.
type HealthHandler struct {
	db *gorm.DB
}

func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Live reports process liveness. Always 200 if the process can serve HTTP.
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "dr-server"})
}

// Ready reports whether this instance can serve real traffic, which in
// practice means: can it reach Postgres. Returns 503 with the reason when it
// can't, so an unhealthy instance is pulled from rotation instead of accepting
// requests it will only fail.
func (h *HealthHandler) Ready(c *gin.Context) {
	if err := h.pingDB(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unavailable",
			"checks": gin.H{"database": "unreachable"},
			"error":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"checks": gin.H{"database": "ok"},
	})
}

// pingDB verifies the connection pool can hand out a live connection. Ping is
// used rather than a SELECT so this stays cheap enough to run on a tight
// interval — it validates the pool without touching application tables.
func (h *HealthHandler) pingDB(ctx context.Context) error {
	// A router built without a database is not ready to serve, and gorm's
	// DB() panics on a nil receiver — so answer this before asking gorm.
	if h.db == nil {
		return errors.New("no database configured")
	}
	sqlDB, err := h.db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, healthPingTimeout)
	defer cancel()
	return sqlDB.PingContext(ctx)
}
