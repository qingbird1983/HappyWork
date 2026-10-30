package audit

// Page is the audit panel payload: recent entries plus the current chain
// status. Total is the number of rows for the requested scope.
type Page struct {
	Entries []Record     `json:"entries"`
	Verify  VerifyResult `json:"verify"`
	Total   int          `json:"total"`
}

// VerifyResult is the outcome of one chain verification. LegacyRows counts
// pre-Phase-4 rows without hashes; they are reported, never silently trusted.
type VerifyResult struct {
	OK            bool   `json:"ok"`
	SealedRows    int    `json:"sealedRows"`
	LegacyRows    int    `json:"legacyRows"`
	FirstSealedID int64  `json:"firstSealedId,omitempty"`
	LastID        int64  `json:"lastId"`
	HeadHash      string `json:"headHash"`
	BrokenID      int64  `json:"brokenId,omitempty"`
	BrokenReason  string `json:"brokenReason,omitempty"`
	HeadMatch     bool   `json:"headMatch"`
}

// Verifier consumes records in ascending ID order. It keeps only the chain
// state, so storage can stream rows without loading them all.
type Verifier struct {
	started       bool
	prev          string
	sealed        int
	legacy        int
	firstSealedID int64
	lastID        int64
	lastSealedID  int64
	head          string
	brokenID      int64
	brokenReason  string
}

// Add consumes one record. After the first break the verifier keeps scanning
// only to report the last row ID; the first broken row is retained.
func (v *Verifier) Add(r Record) {
	v.lastID = r.ID
	if v.brokenReason != "" {
		return
	}
	if r.EntryHash == "" {
		if !v.started {
			v.legacy++
			return
		}
		v.breakAt(r, "unsealed row after sealed segment")
		return
	}
	if !v.started {
		v.started = true
		v.firstSealedID = r.ID
		v.prev = ""
	}
	if r.PrevHash != v.prev {
		v.breakAt(r, "prev hash mismatch")
		return
	}
	want, err := Hash(r.PrevHash, r)
	if err != nil {
		v.breakAt(r, "hash error: "+err.Error())
		return
	}
	if want != r.EntryHash {
		v.breakAt(r, "entry hash mismatch")
		return
	}
	v.prev = r.EntryHash
	v.head = r.EntryHash
	v.lastSealedID = r.ID
	v.sealed++
}

func (v *Verifier) breakAt(r Record, reason string) {
	v.brokenID = r.ID
	v.brokenReason = reason
}

// Finish compares the verified tail with the chain head. A head mismatch is a
// break too: it catches deleted tails and rows appended outside the writer.
func (v *Verifier) Finish(chainID int64, chainHash string) VerifyResult {
	result := VerifyResult{
		SealedRows:    v.sealed,
		LegacyRows:    v.legacy,
		FirstSealedID: v.firstSealedID,
		LastID:        v.lastID,
		HeadHash:      v.head,
		BrokenID:      v.brokenID,
		BrokenReason:  v.brokenReason,
	}
	if v.sealed == 0 {
		result.HeadMatch = chainID == 0 && chainHash == ""
	} else {
		result.HeadMatch = v.brokenReason == "" && chainID == v.lastSealedID && chainHash == v.head
	}
	if !result.HeadMatch && result.BrokenReason == "" {
		result.BrokenReason = "chain head mismatch"
		if v.lastID > 0 {
			result.BrokenID = v.lastID
		}
	}
	result.OK = result.HeadMatch && result.BrokenReason == ""
	return result
}

// Verify is a convenience wrapper over Verifier for slices.
func Verify(records []Record, chainID int64, chainHash string) VerifyResult {
	var v Verifier
	for _, r := range records {
		v.Add(r)
	}
	return v.Finish(chainID, chainHash)
}
