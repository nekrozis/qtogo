// Package transport downloads from a Qt online repository over HTTP(S).
//
// It implements ADR-007. Two kinds of fetch share one policy: small objects into
// memory — a directory listing, Updates.xml, a checksum sidecar — and archives
// streamed to disk while a digest is computed and then verified.
//
// The policy, in one paragraph: HTTPS only, with ftp refused and http behind an
// explicit opt-in; an ordered list of roots whose order is the priority, with no
// built-in list and no random choice; the expected digest fetched from the official
// endpoint and never from the mirror that served the bytes; redirects followed up
// to ten hops but never downgrading https to http; and no host allowlist, because
// trust is placed in the content — verified against that digest — rather than in
// the host that served it.
//
// Failures carry ADR-002's exit codes:
//
//	2  a scheme refused before any request: an http or ftp root without the opt-in
//	4  unreachable, timeout, TLS failure, HTTP error status, redirect loop
//	5  a downgrade seen in transit, a missing or mismatched digest, a digest
//	   request redirected to another host, or a weak digest without the opt-in
//
// A Client is safe for concurrent use. It has no worker pool of its own: how many
// downloads run at once is the caller's budget, and each Download is expected to
// be given its own destination directory.
package transport
