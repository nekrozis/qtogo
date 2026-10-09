package cli

import (
	"context"

	"github.com/nekrozis/qtogo/internal/model"
)

// Services is what the commands need from the layers beneath the front end: a
// command asks a service for a result and renders it. The front end never reaches
// past this seam, and the seam is what lets a test drive a command without a
// process or a network.
type Services interface {
	// ListQtVersions returns the Qt versions a host and target offer.
	ListQtVersions(ctx context.Context, host model.Host, kind model.Kind) ([]model.Version, error)
}
