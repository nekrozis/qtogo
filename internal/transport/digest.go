package transport

import (
	"bytes"
	"crypto/md5"    //nolint:gosec // G501: reached only behind the weak-checksum opt-in
	"crypto/sha1"   //nolint:gosec // G505: reached only behind the weak-checksum opt-in
	"crypto/sha256" //nolint:gosec // G501: sha256 is the safe default
	"crypto/subtle"
	"encoding/hex"
	"hash"
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// algorithm is a checksum algorithm. Only sha256 is safe to use; the others are
// reached only when the caller has opted in (ADR-007 decision 4).
type algorithm string

const (
	algSHA256 algorithm = "sha256"
	algSHA1   algorithm = "sha1"
	algMD5    algorithm = "md5"
)

// parseAlgorithm resolves a configured name, defaulting to sha256.
func parseAlgorithm(name string) (algorithm, error) {
	switch algorithm(strings.ToLower(strings.TrimSpace(name))) {
	case "":
		return algSHA256, nil
	case algSHA256, algSHA1, algMD5:
		return algorithm(strings.ToLower(strings.TrimSpace(name))), nil
	default:
		return "", errs.New(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig,
			"unknown checksum algorithm %q; want sha256, sha1 or md5", name)
	}
}

// weak reports whether ADR-007 accepts this algorithm only under an opt-in.
func (a algorithm) weak() bool { return a != algSHA256 }

// hexLen is how many hex characters a digest of this algorithm has.
func (a algorithm) hexLen() int {
	switch a {
	case algSHA1:
		return 40
	case algMD5:
		return 32
	default:
		return 64
	}
}

// new starts a hash of this algorithm.
func (a algorithm) new() hash.Hash {
	switch a {
	case algSHA1:
		return sha1.New() //nolint:gosec // G401: weak only by explicit request
	case algMD5:
		return md5.New() //nolint:gosec // G401: weak only by explicit request
	default:
		return sha256.New()
	}
}

// parseSidecar reads the digest out of a checksum sidecar. Qt writes
// "<hex>  <file name>", but an archive's .sha1 is bare hex, so the first
// whitespace-delimited token is taken; a file name, when present, is ignored
// rather than checked against the archive's own name.
func parseSidecar(a algorithm, body []byte) (string, error) {
	fields := bytes.Fields(body)
	if len(fields) == 0 {
		return "", errs.New(exitcode.Integrity, errs.CodeChecksumMalformed, errs.PhaseVerify,
			"checksum sidecar is empty")
	}
	digest := strings.ToLower(string(fields[0]))
	if len(digest) != a.hexLen() {
		return "", errs.New(exitcode.Integrity, errs.CodeChecksumMalformed, errs.PhaseVerify,
			"checksum %q has %d characters; %s uses %d", digest, len(digest), a, a.hexLen())
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errs.New(exitcode.Integrity, errs.CodeChecksumMalformed, errs.PhaseVerify,
			"checksum %q is not hexadecimal", digest)
	}
	return digest, nil
}

// equalDigest compares two hex digests without leaking where they first differ.
// Unequal lengths compare unequal.
func equalDigest(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
