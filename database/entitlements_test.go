package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldResetAIPeriod(t *testing.T) {
	tests := []struct {
		name        string
		periodStart time.Time
		now         time.Time
		want        bool
	}{
		{
			name:        "same calendar month",
			periodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
			want:        false,
		},
		{
			name:        "same day same month",
			periodStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 25, 23, 59, 59, 0, time.UTC),
			want:        false,
		},
		{
			name:        "crossed into next month",
			periodStart: time.Date(2026, 5, 31, 23, 59, 0, 0, time.UTC),
			now:         time.Date(2026, 6, 1, 0, 1, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "crossed into new year",
			periodStart: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "crossed into new year, same month number",
			periodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "now in a different timezone, same UTC month",
			periodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 25, 12, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldResetAIPeriod(tt.periodStart, tt.now))
		})
	}
}
