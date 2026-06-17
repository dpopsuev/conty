package scribe

import (
	"context"

	scribeclient "github.com/dpopsuev/scribe/client"
	"github.com/dpopsuev/conty/internal/domain"
)

// IngestBuilds translates CI runs and POSTs to the Scribe ingest URL.
func IngestBuilds(ctx context.Context, runs []domain.CIRun, backend, ingestURL string) error {
	result := TranslateBuilds(runs, backend)
	return scribeclient.Post(ctx, result.Records, result.Edges, "conty", ingestURL)
}
