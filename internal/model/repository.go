package model

import "strings"

// RepositoryRef locates one subtree of a repository. The base and the path stay
// apart so that nothing has to guess how they join.
type RepositoryRef struct {
	BaseURL string   `json:"baseUrl"`
	Path    []string `json:"path,omitempty"`
}

// URL renders the reference as an absolute URL.
func (r RepositoryRef) URL() string {
	base := strings.TrimRight(r.BaseURL, "/")
	if len(r.Path) == 0 {
		return base
	}
	return base + "/" + strings.Join(r.Path, "/")
}
