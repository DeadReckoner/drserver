package database

import "time"

// Now is the clock used by database operations whose behavior depends on the
// current time (streak boundaries, overdue checks, expiry windows). Tests
// override this to freeze time.
var Now = time.Now
