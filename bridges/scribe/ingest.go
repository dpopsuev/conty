package scribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dpopsuev/battery/translate"
	"github.com/dpopsuev/conty/internal/domain"
)

// IngestBuilds translates CI runs and POSTs to the Scribe ingest URL.
func IngestBuilds(ctx context.Context, runs []domain.CIRun, backend, ingestURL string) error {
	result := TranslateBuilds(runs, backend)
	return postNDJSON(ctx, result, ingestURL, "conty")
}

func postNDJSON(ctx context.Context, result translate.Result, ingestURL, source string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	for _, r := range result.Records {
		_ = enc.Encode(map[string]any{
			"type": "node", "id": r.ID, "kind": r.Kind,
			"title": r.Title, "labels": r.Labels,
			"extra": r.Extra, "sections": r.Sections,
		})
	}
	for _, e := range result.Edges {
		_ = enc.Encode(map[string]any{
			"type": "edge", "from": e.From, "to": e.To, "relation": e.Relation,
		})
	}
	_ = enc.Encode(map[string]any{
		"type": "meta", "source": source,
		"scanned_at":  time.Now().UTC().Format(time.RFC3339),
		"total_nodes": len(result.Records), "total_edges": len(result.Edges),
	})

	url := fmt.Sprintf("%s?source=%s", ingestURL, source)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST ingest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	slog.InfoContext(ctx, "scribe ingest: complete",
		"status", resp.StatusCode,
		"nodes", len(result.Records),
		"edges", len(result.Edges),
	)
	return nil
}
