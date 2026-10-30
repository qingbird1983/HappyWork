package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExportResult describes the evidence copy written by Export. The manifest is
// not signed; it records the chain head and verification outcome so the JSONL
// can be re-checked independently.
type ExportResult struct {
	JSONLPath    string       `json:"jsonlPath"`
	ManifestPath string       `json:"manifestPath"`
	Rows         int          `json:"rows"`
	Verify       VerifyResult `json:"verify"`
}

type exportManifest struct {
	Version    int          `json:"version"`
	ExportedAt string       `json:"exportedAt"`
	Rows       int          `json:"rows"`
	Verify     VerifyResult `json:"verify"`
}

// Export writes all records as JSONL plus a manifest under dir. It always
// writes the current (possibly broken) evidence; callers must surface
// result.Verify.OK instead of assuming success.
func Export(dir string, records []Record, result VerifyResult) (ExportResult, error) {
	if dir == "" {
		return ExportResult{}, fmt.Errorf("audit: export directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ExportResult{}, fmt.Errorf("audit: create export directory: %w", err)
	}
	now := time.Now().UTC()
	base := "audit-" + now.Format("20060102T150405.000000000Z")
	jsonlPath := filepath.Join(dir, base+".jsonl")
	manifestPath := filepath.Join(dir, base+".manifest.json")

	if err := writeJSONL(jsonlPath, records); err != nil {
		return ExportResult{}, err
	}
	manifest := exportManifest{
		Version:    1,
		ExportedAt: now.Format(time.RFC3339Nano),
		Rows:       len(records),
		Verify:     result,
	}
	if err := writeJSON(manifestPath, manifest); err != nil {
		_ = os.Remove(jsonlPath)
		return ExportResult{}, err
	}
	return ExportResult{
		JSONLPath:    jsonlPath,
		ManifestPath: manifestPath,
		Rows:         len(records),
		Verify:       result,
	}, nil
}

func writeJSONL(path string, records []Record) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".audit-*.tmp")
	if err != nil {
		return fmt.Errorf("audit: create jsonl: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: chmod jsonl: %w", err)
	}
	writer := bufio.NewWriter(tmp)
	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			_ = tmp.Close()
			cleanup()
			return fmt.Errorf("audit: encode jsonl: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: flush jsonl: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: sync jsonl: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("audit: close jsonl: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("audit: rename jsonl: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("audit: marshal manifest: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("audit: create manifest: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: chmod manifest: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: write manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("audit: sync manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("audit: close manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("audit: rename manifest: %w", err)
	}
	return nil
}
