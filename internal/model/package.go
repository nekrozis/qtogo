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
	Name        string `json:"name"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256,omitempty"`
	Size        int64  `json:"size,omitempty"`
	InstallPath string `json:"installPath,omitempty"`
}
