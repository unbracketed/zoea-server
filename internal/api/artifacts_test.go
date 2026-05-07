package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArtifactFixture(t *testing.T, workingDir, runID string) {
	t.Helper()
	artifactsDir := filepath.Join(workingDir, ".zoea", "output", runID, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, "report.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	nestedDir := filepath.Join(artifactsDir, "sub")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "data.json"), []byte(`{"k":1}`), 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}
	resultsPath := filepath.Join(workingDir, ".zoea", "output", runID, "results.jsonl")
	resultLine := map[string]any{
		"id":        "abc",
		"tool_name": "demo",
		"status":    "success",
		"summary":   "ok",
		"artifacts": []map[string]any{
			{
				"name":          "report.md",
				"relative_path": runID + "/artifacts/report.md",
				"media_type":    "text/markdown",
				"bytes":         7,
			},
			{
				"name":          "sub/data.json",
				"relative_path": runID + "/artifacts/sub/data.json",
				"media_type":    "application/json",
				"bytes":         7,
			},
		},
	}
	enc, err := json.Marshal(resultLine)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := os.WriteFile(resultsPath, append(enc, '\n'), 0o644); err != nil {
		t.Fatalf("write results.jsonl: %v", err)
	}
}

func createSessionWithWorkingDir(t *testing.T, h *Handler, workingDir string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	body := `{"user_id":"alice","working_dir":"` + workingDir + `"}`
	req := adminCtx(httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.SessionID
}

func TestArtifactBytesEndpoint_Markdown(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	writeArtifactFixture(t, workingDir, "run-1")
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/artifacts/run-1/report.md", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "# hello" {
		t.Fatalf("body: %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown" {
		t.Fatalf("content-type: %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "7" {
		t.Fatalf("content-length: %q", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Fatalf("content-disposition: %q", cd)
	}
}

func TestArtifactBytesEndpoint_NestedPath(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	writeArtifactFixture(t, workingDir, "run-1")
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/artifacts/run-1/sub/data.json", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type: %q", got)
	}
}

func TestArtifactBytesEndpoint_RejectsTraversal(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	writeArtifactFixture(t, workingDir, "run-1")
	sid := createSessionWithWorkingDir(t, h, workingDir)

	cases := []string{
		"/v1/sessions/" + sid + "/artifacts/run-1/..%2Fresults.jsonl",
		"/v1/sessions/" + sid + "/artifacts/..%2Frun-1/report.md",
	}
	for _, path := range cases {
		rec := httptest.NewRecorder()
		req := adminCtx(httptest.NewRequest(http.MethodGet, path, nil))
		h.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("path %s: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestArtifactBytesEndpoint_FallsBackToOctetStream(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	runID := "run-2"
	artifactsDir := filepath.Join(workingDir, ".zoea", "output", runID, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, "blob.bin"), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// no results.jsonl → media_type lookup misses
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/artifacts/"+runID+"/blob.bin", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content-type: %q", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("disposition: %q", cd)
	}
}

func TestArtifactRunSummaryEndpoint(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	writeArtifactFixture(t, workingDir, "run-1")
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/artifacts/run-1", nil))
	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RunID   string            `json:"run_id"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RunID != "run-1" {
		t.Fatalf("run_id: %q", resp.RunID)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results count: %d", len(resp.Results))
	}
}

func TestArtifactEndpoint_NonexistentRun(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid+"/artifacts/missing-run/x.txt", nil))
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestArtifactEndpoint_RejectsNonGet(t *testing.T) {
	h, _, _ := newTestHandler(t)
	workingDir := t.TempDir()
	writeArtifactFixture(t, workingDir, "run-1")
	sid := createSessionWithWorkingDir(t, h, workingDir)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodPost, "/v1/sessions/"+sid+"/artifacts/run-1/report.md", nil))
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestResolveOutputDir_HonorsAbsoluteEnv(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("ZOEA_OUTPUT_DIR", abs)
	defer os.Unsetenv("ZOEA_OUTPUT_DIR")
	got := resolveOutputDir("/some/working/dir")
	if got != filepath.Clean(abs) {
		t.Fatalf("got: %q", got)
	}
}

func TestResolveOutputDir_RelativeEnvJoinsWorkingDir(t *testing.T) {
	t.Setenv("ZOEA_OUTPUT_DIR", "out")
	defer os.Unsetenv("ZOEA_OUTPUT_DIR")
	got := resolveOutputDir("/work")
	if got != filepath.Clean("/work/out") {
		t.Fatalf("got: %q", got)
	}
}

// Suppress unused-import warning when the package only uses context for
// matching the test scaffolding helper signatures.
var _ = context.Background
