package detection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

func freezeEvidence(key windowKey, w *windowState, r Rule) (*contracts.Alert, []byte, string, error) {
	a := windowAlert(key, w, r)
	if err := a.Validate(); err != nil {
		return nil, nil, "", errors.New("invalid canonical alert")
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, nil, "", errors.New("encode canonical alert")
	}
	// Match immutable storage's fingerprint policy without importing a storage
	// adapter into the pure reducer: workflow status and detected_at are excluded.
	immutable := *a
	immutable.Status = ""
	immutable.DetectedAt = nil
	content, err := json.Marshal(&immutable)
	if err != nil {
		return nil, nil, "", errors.New("encode immutable alert")
	}
	digest := sha256.Sum256(content)
	return a, raw, hex.EncodeToString(digest[:]), nil
}

func windowAlert(key windowKey, w *windowState, r Rule) *contracts.Alert {
	start := time.UnixMilli(key.start).UTC()
	end := contracts.NewTimestamp(time.UnixMilli(w.end))
	entity := []string{key.ip, key.principal}
	summary := "Repeated failed logins against one user"
	switch r.ID {
	case scanRuleID:
		summary = "Distinct destination ports probed"
	case floodRuleID:
		entity = []string{key.ip}
		summary = "Excessive network request volume"
	case denyRuleID:
		entity = []string{key.ip}
		summary = "Burst of denied policy decisions"
	}
	display := strings.Join(entity, "|")
	return &contracts.Alert{
		SchemaVersion: 1, AlertID: contracts.DedupeKey(key.tenant, r.ID, entity, start),
		TenantID: key.tenant, RuleID: r.ID, RuleVersion: r.Version, Severity: r.Severity, Status: "open",
		CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end, EntityKey: display,
		Summary: summary, EventCount: len(w.ids), SampleEventIDs: slices.Clone(w.samples),
		DedupeKey: key.tenant + "|" + r.ID + "|" + display + "|" + start.Format(time.RFC3339Nano),
	}
}
