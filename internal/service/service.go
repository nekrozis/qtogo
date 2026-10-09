// Package service is the layer between the command front end and the repository
// layers.
//
// A command asks a service for a result; the service knows which of repository,
// discovery and transport to use and in what order, so a command reads as what it
// shows rather than as a walk over the network. The front end never reaches past
// this seam.
package service

import (
	"context"
	"path"
	"slices"

	"github.com/nekrozis/qtogo/internal/discovery"
	"github.com/nekrozis/qtogo/internal/model"
)

// Service drives the repository layers for the commands.
type Service struct {
	discover *discovery.Discover
}

// New returns a Service that reads a repository through fetch.
func New(fetch discovery.Fetcher) *Service {
	return &Service{discover: discovery.New(fetch)}
}

// ListQtVersions returns the Qt versions a host and target offer, oldest first and
// without duplicates.
func (s *Service) ListQtVersions(ctx context.Context, host model.Host, kind model.Kind) ([]model.Version, error) {
	segment, err := discovery.Segment(host, kind)
	if err != nil {
		return nil, err
	}
	found, err := s.discover.Versions(ctx, path.Join(discovery.Root, segment, string(kind)))
	if err != nil {
		return nil, err
	}
	versions := make([]model.Version, 0, len(found))
	for _, v := range found {
		versions = append(versions, v.Directory.Version)
	}
	return order(versions), nil
}

// order sorts versions oldest first and drops one that two directories spell the
// same way, so a target that offers a version under two names lists it once.
func order(versions []model.Version) []model.Version {
	slices.SortFunc(versions, model.Version.Compare)
	return slices.CompactFunc(versions, func(a, b model.Version) bool { return a.Compare(b) == 0 })
}
