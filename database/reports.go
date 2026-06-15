package database

import (
	"context"
	"time"

	"dr/server/types"
)

// CreateReport persists a moderation report. The row is the durable artifact;
// the support email is sent best-effort by the caller after this succeeds.
func CreateReport(ctx context.Context, report *types.Report) error {
	return db.Create(report).Error
}

// CountReportsByReporterSince counts reports filed by a reporter since a given
// time. Used to rate-limit submissions.
func CountReportsByReporterSince(reporterID string, since time.Time) (int64, error) {
	var count int64
	err := db.Model(&types.Report{}).
		Where("reporter_id = ? AND created_at >= ?", reporterID, since).
		Count(&count).Error
	return count, err
}
