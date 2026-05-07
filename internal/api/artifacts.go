package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/unbracketed/zoea-server/internal/session"
)

// resolveOutputDir mirrors zoea-core's resolution: ZOEA_OUTPUT_DIR overrides
// the default `.zoea/output`. Relative paths resolve from the session's
// working dir. Absolute paths are used as-is. Returns "" if no working dir
// is associated with the session.
func resolveOutputDir(workingDir string) string {
	raw := os.Getenv("ZOEA_OUTPUT_DIR")
	if strings.TrimSpace(raw) == "" {
		raw = ".zoea/output"
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	if workingDir == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(workingDir, raw))
}

// resolveArtifactPath joins runID + name into an absolute path under
// outputDir/<runID>/artifacts/, defending against traversal. The returned
// path is guaranteed to live within the artifacts directory after symlink
// resolution. If validation fails, ok is false.
func resolveArtifactPath(outputDir, runID, name string) (path string, ok bool) {
	if outputDir == "" || runID == "" || name == "" {
		return "", false
	}
	if strings.Contains(runID, "/") || strings.Contains(runID, "\\") || runID == ".." {
		return "", false
	}
	artifactsRoot := filepath.Join(outputDir, runID, "artifacts")
	resolvedRoot, err := filepath.EvalSymlinks(artifactsRoot)
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(artifactsRoot, filepath.FromSlash(name))
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	rootWithSep := resolvedRoot + string(filepath.Separator)
	if resolvedCandidate != resolvedRoot && !strings.HasPrefix(resolvedCandidate, rootWithSep) {
		return "", false
	}
	info, err := os.Stat(resolvedCandidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	return resolvedCandidate, true
}

// lookupArtifactMediaType walks results.jsonl for an entry whose
// `relative_path` matches `<runID>/artifacts/<name>` and returns its
// recorded media_type. Empty string when not found.
func lookupArtifactMediaType(outputDir, runID, name string) string {
	resultsPath := filepath.Join(outputDir, runID, "results.jsonl")
	f, err := os.Open(resultsPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	target := runID + "/artifacts/" + filepath.ToSlash(name)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record struct {
			Artifacts []struct {
				RelativePath string `json:"relative_path"`
				MediaType    string `json:"media_type"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		for _, a := range record.Artifacts {
			if a.RelativePath == target {
				return a.MediaType
			}
		}
	}
	return ""
}

// handleArtifactRequest dispatches GETs under /v1/sessions/{id}/artifacts/...
// `tail` is the path remainder after "artifacts/" — either "" (forbidden,
// directory listing not exposed), "<runID>" (returns the parsed result
// JSON), or "<runID>/<name...>" (streams artifact bytes).
func (h *Handler) handleArtifactRequest(w http.ResponseWriter, r *http.Request, s *session.Session, tail string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if !h.requireScope(w, r, "sessions.read") {
		return
	}

	tail = strings.Trim(tail, "/")
	if tail == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}

	parts := strings.SplitN(tail, "/", 2)
	runID, err := url.PathUnescape(parts[0])
	if err != nil || runID == "" || strings.Contains(runID, "/") || runID == ".." {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}

	outputDir := resolveOutputDir(s.WorkingDir)
	if outputDir == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}

	if len(parts) == 1 {
		h.serveArtifactRunSummary(w, outputDir, runID)
		return
	}

	rawName := parts[1]
	name, err := url.PathUnescape(rawName)
	if err != nil || name == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}

	h.serveArtifactBytes(w, outputDir, runID, name)
}

func (h *Handler) serveArtifactRunSummary(w http.ResponseWriter, outputDir, runID string) {
	resultsPath := filepath.Join(outputDir, runID, "results.jsonl")
	f, err := os.Open(resultsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "results_unreadable"})
		return
	}
	defer f.Close()
	results := make([]json.RawMessage, 0, 4)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		buf := make([]byte, len(line))
		copy(buf, line)
		results = append(results, buf)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":  runID,
		"results": results,
	})
}

func (h *Handler) serveArtifactBytes(w http.ResponseWriter, outputDir, runID, name string) {
	path, ok := resolveArtifactPath(outputDir, runID, name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "artifact_not_found"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "stat_failed"})
		return
	}

	mediaType := lookupArtifactMediaType(outputDir, runID, name)
	disposition := "inline"
	if mediaType == "" {
		mediaType = "application/octet-stream"
		disposition = "attachment"
	}

	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Disposition", disposition+`; filename="`+filepath.Base(name)+`"`)
	_, _ = io.Copy(w, f)
}
