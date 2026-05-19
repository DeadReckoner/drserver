package types

import "time"

// Now is the clock used for time-dependent status computation (e.g. overdue
// detection in ComputeAllStatuses). Tests override this to freeze time.
var Now = time.Now
