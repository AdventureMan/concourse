package worker

import (
	"context"
	"io"
)

//go:generate counterfeiter . ArtifactDestination

// Destination is the inverse of Source.
//
// This interface allows the receiving end to determine the location of the
// data, e.g. based on a task's input configuration.
//
type ArtifactDestination interface {

	// StreamIn is called with a destination directory and the tar stream to
	// expand into the destination directory.
	//
	StreamIn(context.Context, string, io.Reader) error

	// Zone gives high-level information about
	//
	Zone() string
}
