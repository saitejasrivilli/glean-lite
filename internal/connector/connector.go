package connector

import "context"

// Document is a unit of indexable content from any source.
type Document struct {
	ID       string            // Unique ID (e.g. "github:owner/repo:path/to/file.go")
	Title    string            // Human-readable title
	Body     string            // Full text content
	URL      string            // Link to the source
	Metadata map[string]string // e.g. {"repo": "glean-lite", "language": "go"}
}

// Connector is the interface every data source must implement.
// Adding a new source = implement this interface, register in engine.
type Connector interface {
	// Name returns a short identifier for logging/config (e.g. "github", "confluence")
	Name() string

	// Fetch retrieves all documents from this source.
	// Implementations should respect ctx cancellation.
	Fetch(ctx context.Context) ([]Document, error)
}
