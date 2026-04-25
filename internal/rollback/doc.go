// Package rollback monitors merged Apply Fix PRs for 7 days and opens an
// automated rollback PR if observed metrics breach the predicted bounds.
package rollback
