package jobs

import "errors"

// errFakeDB is a sentinel used by tests that need to simulate a DB-layer
// failure without depending on any particular driver's error type.
var errFakeDB = errors.New("simulated db error")
