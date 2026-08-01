package provider

import (
	"context"
	"io"
)

// Status is a snapshot of what the active inference server is doing.
type Status struct {
	Running        bool
	State          string // "running", "starting", "stopping", "stopped", "failed", "unknown" (systemd-backed providers)
	ModelID        string // model ID currently loaded, empty if not running
	ContainerID    string // populated for NIM providers
	ContainerState string // "running", "exited", "not found", etc. (NIM providers)
}

// Provider controls a single inference backend (vLLM or NIM).
type Provider interface {
	// Switch loads the named model (slug) and restarts the backend.
	// It writes the new active symlink / starts the new container before
	// stopping the old one where possible.
	Switch(ctx context.Context, modelSlug string) error

	// Stop tears down the currently running backend.
	Stop(ctx context.Context) error

	// Status returns what is currently active.
	Status(ctx context.Context) (*Status, error)

	// Logs streams inference service logs to w.
	// If follow is true it tails indefinitely; lines controls the initial
	// back-scroll (ignored when follow is true and backend has no history).
	Logs(ctx context.Context, w io.Writer, follow bool, lines int) error
}
