// Package testutil provides shared helpers for drserver tests: a fake auth
// middleware, a time-freezing helper, a sqlmock-backed *gorm.DB, fixture
// builders for the roadmap hierarchy, and a router factory that wires it all
// together. Production code must not import this package.
package testutil
