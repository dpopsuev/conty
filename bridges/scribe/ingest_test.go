package scribe_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bridge "github.com/dpopsuev/conty/bridges/scribe"
	"github.com/dpopsuev/conty/testdata"
)

func TestIngestBuilds_PostsNDJSON(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s; want POST", r.Method)
		}
		if !strings.Contains(r.URL.String(), "source=conty") {
			t.Errorf("URL = %s; want source=conty param", r.URL.String())
		}
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()

	err := bridge.IngestBuilds(context.Background(), testdata.SampleBuilds(), testBackend, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "deploy-staging") {
		t.Error("body missing deploy-staging")
	}
	if !strings.Contains(body, `"type":"node"`) {
		t.Error("body missing node records")
	}
	if !strings.Contains(body, `"type":"meta"`) {
		t.Error("body missing meta record")
	}
}

func TestIngestBuilds_IncludesUpstreamEdges(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()

	err := bridge.IngestBuilds(context.Background(), testdata.SampleBuilds(), testBackend, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "produced_by") {
		t.Error("body missing produced_by edge")
	}
}
