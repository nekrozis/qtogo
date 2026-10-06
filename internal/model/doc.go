// Package model holds qtogo's domain types: what a Qt release is, and what the
// repository says about it.
//
// Nothing here imports the CLI, HTTP or the filesystem. Qt's repository layout is
// an external protocol that changes; the point of this package is that those
// changes stop at the boundary instead of reaching into the domain.
package model
