package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldResetAIPeriod(t *testing.T) {
	// The AI generation cap is per UTC calendar day (drmobile#183): the
	// counter resets whenever `now` is on a different UTC day than the
	// period start.
	tests := []struct {
		name        string
		periodStart time.Time
		now         time.Time
		want        bool
	}{
		{
			name:        "same UTC day",
			periodStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 25, 23, 59, 59, 0, time.UTC),
			want:        false,
		},
		{
			name:        "crossed UTC midnight",
			periodStart: time.Date(2026, 5, 25, 23, 59, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 26, 0, 1, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "same month, later day",
			periodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "crossed into next month",
			periodStart: time.Date(2026, 5, 31, 23, 59, 0, 0, time.UTC),
			now:         time.Date(2026, 6, 1, 0, 1, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "one year later, same year-day",
			periodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			now:         time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC),
			want:        true,
		},
		{
			name:        "now in a different timezone, same UTC day",
			periodStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
			// 2026-05-25 20:00 PST == 2026-05-26 04:00 UTC → different UTC day
			// only if conversion is applied; here 12:00 PST == 20:00 UTC, same day.
			now:  time.Date(2026, 5, 25, 12, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			want: false,
		},
		{
			name:        "different local day but same UTC day",
			periodStart: time.Date(2026, 5, 25, 1, 0, 0, 0, time.UTC),
			// 2026-05-24 18:00 PST == 2026-05-25 02:00 UTC → same UTC day.
			now:  time.Date(2026, 5, 24, 18, 0, 0, 0, time.FixedZone("PST", -8*3600)),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldResetAIPeriod(tt.periodStart, tt.now))
		})
	}
}
