// Package repository reads Qt's online repositories: directory listings decide
// what exists, and the metadata files inside them describe how to install it.
//
// Everything here is offline. A caller hands in bytes and a URL, which is what
// makes the rules below testable against fixtures instead of against a live
// mirror.
package repository
