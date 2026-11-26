// Package core is the domain of wt-manager: what a project, a worktree, a
// stack, a pull request and a ticket are, and the rules about them that need
// no outside world (how a config is normalized, where a worktree goes, which
// ticket a branch belongs to, how rows are filtered and ordered).
//
// It imports no other package of this module and never runs a program, reads
// a file or talks to a network; internal/arch tests that. Everything here can
// be tested with plain values.
//
// ports.go lists what the UIs may ask the application to do when the outside
// world is involved.
package core
