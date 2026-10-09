package model

// Package is one installable component as the repository metadata describes it.
//
// Downloadables are the archive names the metadata lists; resolving them into
// URLs is the catalog's job, and the result is an Archive.
type Package struct {
	Name          string       `json:"name"`
	Version       Version      `json:"version"`
	Dependencies  []Dependency `json:"dependencies,omitempty"`
	Downloadables []string     `json:"downloadables,omitempty"`
}

// Dependency is another package this one needs. A zero Version means the
// metadata asked for no particular version.
type Dependency struct {
	Name    string  `json:"name"`
	Version Version `json:"version"`
}

// Archive is one downloadable file and where its contents belong.
type Archive struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// SHA1 is the digest the repository metadata declares, carried so a report can
	// say what the repository claimed. It is not what integrity is checked against:
	// that is the transport's own SHA-256, read from the sidecar beside the archive
	// (ADR-007). The two are deliberately different things.
	SHA1        string `json:"sha1,omitempty"`
	Size        int64  `json:"size,omitempty"`
	InstallPath string `json:"installPath,omitempty"`
}
