package audit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func testRecord(id int64) Record {
	return Record{
		ID:        id,
		SessionID: "s1",
		TurnID:    id,
		TraceID:   "trace-1",
		Action:    ActionToolCall,
		Decision:  DecisionAllow,
		Reason:    "allowed by policy",
		Detail:    `{"toolRef":"builtin://read_file","matchKey":"docs"}`,
		CreatedAt: "2026-10-30T00:00:00Z",
	}
}

func seal(t *testing.T, rows []Record) ([]Record, int64, string) {
	t.Helper()
	prev := ""
	for i := range rows {
		rows[i].PrevHash = prev
		h, err := Hash(prev, rows[i])
		if err != nil {
			t.Fatalf("Hash row %d: %v", i, err)
		}
		rows[i].EntryHash = h
		prev = h
	}
	if len(rows) == 0 {
		return rows, 0, ""
	}
	return rows, rows[len(rows)-1].ID, prev
}

func TestHashDeterministicAndDetailObject(t *testing.T) {
	r := testRecord(1)
	h1, err := Hash("", r)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, err := Hash("", r)
	if err != nil || h1 != h2 {
		t.Fatalf("hash not deterministic: %q %q %v", h1, h2, err)
	}
	if len(h1) != 64 {
		t.Fatalf("hash length = %d, want 64", len(h1))
	}
	changed := r
	changed.Reason = "different"
	h3, err := Hash("", changed)
	if err != nil || h3 == h1 {
		t.Fatalf("reason did not change hash: %v", err)
	}
	if _, err := NormalizeDetail(`[1,2]`); err == nil {
		t.Fatal("array detail should be rejected")
	}
	if _, err := NormalizeDetail(`{"a":1,"a":2}`); err == nil {
		t.Fatal("duplicate key detail should be rejected")
	}
	if got, err := NormalizeDetail(""); err != nil || got != "{}" {
		t.Fatalf("empty detail = %q, %v", got, err)
	}
}

func TestVerifyAcceptsChain(t *testing.T) {
	rows, id, head := seal(t, []Record{testRecord(1), testRecord(2), testRecord(3)})
	got := Verify(rows, id, head)
	if !got.OK || !got.HeadMatch || got.SealedRows != 3 || got.LegacyRows != 0 {
		t.Fatalf("verify = %+v", got)
	}
	if got.HeadHash != head || got.LastID != 3 {
		t.Fatalf("head/last = %+v", got)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	rows, id, head := seal(t, []Record{testRecord(1), testRecord(2), testRecord(3)})
	rows[1].Reason = "tampered"
	got := Verify(rows, id, head)
	if got.OK || got.BrokenID != 2 || !strings.Contains(got.BrokenReason, "hash") {
		t.Fatalf("verify = %+v", got)
	}
}

func TestVerifyDetectsDeletion(t *testing.T) {
	rows, id, head := seal(t, []Record{testRecord(1), testRecord(2), testRecord(3)})
	rows = append(rows[:1], rows[2])
	got := Verify(rows, id, head)
	if got.OK || got.BrokenID != 3 || !strings.Contains(got.BrokenReason, "prev") {
		t.Fatalf("verify = %+v", got)
	}
}

func TestVerifyDetectsUnsealedInsert(t *testing.T) {
	rows, id, head := seal(t, []Record{testRecord(1), testRecord(2)})
	inserted := testRecord(3)
	inserted.EntryHash = ""
	inserted.PrevHash = ""
	rows = append(rows, inserted)
	got := Verify(rows, id, head)
	if got.OK || got.BrokenID != 3 || !strings.Contains(got.BrokenReason, "unsealed") {
		t.Fatalf("verify = %+v", got)
	}
}

func TestVerifyLegacyPrefix(t *testing.T) {
	legacy := testRecord(1)
	rows, id, head := seal(t, []Record{testRecord(2), testRecord(3)})
	all := append([]Record{legacy}, rows...)
	got := Verify(all, id, head)
	if !got.OK || got.LegacyRows != 1 || got.SealedRows != 2 || got.FirstSealedID != 2 {
		t.Fatalf("verify = %+v", got)
	}
}

func TestVerifyHeadMismatch(t *testing.T) {
	rows, _, _ := seal(t, []Record{testRecord(1), testRecord(2)})
	got := Verify(rows, 1, rows[0].EntryHash)
	if got.OK || got.HeadMatch || !strings.Contains(got.BrokenReason, "head") {
		t.Fatalf("verify = %+v", got)
	}
}

func TestVerifyLegacyOnly(t *testing.T) {
	got := Verify([]Record{testRecord(1)}, 0, "")
	if !got.OK || got.LegacyRows != 1 || got.SealedRows != 0 {
		t.Fatalf("verify = %+v", got)
	}
}

func TestExportEvidence(t *testing.T) {
	rows, id, head := seal(t, []Record{testRecord(1), testRecord(2)})
	result := Verify(rows, id, head)
	dir := t.TempDir()
	out, err := Export(dir, rows, result)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if out.Rows != 2 || !out.Verify.OK {
		t.Fatalf("export = %+v", out)
	}
	data, err := os.ReadFile(out.JSONLPath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d", len(lines))
	}
	var first Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode jsonl: %v", err)
	}
	if first.EntryHash != rows[0].EntryHash {
		t.Fatalf("hash lost in export: %+v", first)
	}
	var manifest exportManifest
	raw, err := os.ReadFile(out.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Rows != 2 || manifest.Verify.HeadHash != head {
		t.Fatalf("manifest = %+v", manifest)
	}
}
