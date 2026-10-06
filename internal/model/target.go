package model

import (
	"fmt"
	"strings"
)

// Host is the platform a package belongs to.
type Host string

const (
	HostWindows Host = "windows"
	HostLinux   Host = "linux"
	HostMac     Host = "mac"
	HostAll     Host = "all_os"
)

// Kind is the platform family a package targets.
type Kind string

const (
	KindDesktop Kind = "desktop"
	KindAndroid Kind = "android"
	KindIOS     Kind = "ios"
	KindWinRT   Kind = "winrt"
	KindWASM    Kind = "wasm"
)

// hosts and kinds are the recognised values, in the order help text lists them.
var (
	hosts = []Host{HostWindows, HostLinux, HostMac, HostAll}
	kinds = []Kind{KindDesktop, KindAndroid, KindIOS, KindWinRT, KindWASM}
)

// Hosts returns the recognised hosts.
func Hosts() []Host { return append([]Host(nil), hosts...) }

// Kinds returns the recognised target kinds.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// ParseHost resolves a host name, ignoring case and surrounding space.
func ParseHost(s string) (Host, error) {
	name := Host(normalise(s))
	for _, h := range hosts {
		if h == name {
			return h, nil
		}
	}
	return "", fmt.Errorf("unknown host %q, want one of %s", s, joinNames(hosts))
}

// ParseKind resolves a target kind, ignoring case and surrounding space.
func ParseKind(s string) (Kind, error) {
	name := Kind(normalise(s))
	for _, k := range kinds {
		if k == name {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown kind %q, want one of %s", s, joinNames(kinds))
}

// Target names one package's coordinates: the host it belongs to, the platform
// family it targets, the version, the architecture and any extension variant.
//
// Arch stays a string: its shape varies too much to decompose without inventing
// structure the repository does not guarantee, and only the layer that knows the
// version can tell which architectures exist.
type Target struct {
	Host      Host    `json:"host"`
	Kind      Kind    `json:"kind"`
	Version   Version `json:"version"`
	Arch      string  `json:"arch"`
	Extension string  `json:"extension,omitempty"`
}

// Validate reports whether the target is complete enough to act on.
func (t Target) Validate() error {
	if _, err := ParseHost(string(t.Host)); err != nil {
		return err
	}
	if _, err := ParseKind(string(t.Kind)); err != nil {
		return err
	}
	if t.Version.IsZero() {
		return fmt.Errorf("target has no version")
	}
	if strings.TrimSpace(t.Arch) == "" {
		return fmt.Errorf("target has no architecture")
	}
	return nil
}

// String renders the target the way the command line addresses it.
func (t Target) String() string {
	parts := []string{string(t.Host), string(t.Kind), t.Version.String(), t.Arch}
	if t.Extension != "" {
		parts = append(parts, t.Extension)
	}
	return strings.Join(parts, " ")
}

func normalise(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// joinNames renders a closed set for an error message.
func joinNames[T ~string](values []T) string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = string(v)
	}
	return strings.Join(names, ", ")
}
