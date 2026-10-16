package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRawTranscriptAndVocabularyAuditPersistTogether(t *testing.T) {
	d, e := NewDatabase(filepath.Join(t.TempDir(), "audit.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	user, e := d.GetOrCreateUser("audit", "audit@invalid.example", "Audit")
	if e != nil {
		t.Fatal(e)
	}
	audit := `{"resolved_transcript":"We use PostgreSQL.","decisions":[{"original":"postgrsq","replacement":"PostgreSQL","source":"jev"}]}`
	id, e := d.LogUsageAndUpdateQuota(user, 3, 0.1, 0.2, 0.3, true, "grammar", "We use postgrsq.", "We use PostgreSQL.", "success", 100, "nemotron", "llm", audit)
	if e != nil {
		t.Fatal(e)
	}
	logs, e := d.GetUserUsageLogs(user)
	if e != nil || len(logs) != 1 {
		t.Fatalf("history: %+v %v", logs, e)
	}
	if logs[0].ID != id || logs[0].InputTranscript != "We use postgrsq." || !json.Valid(logs[0].VocabularyAudit) || string(logs[0].VocabularyAudit) != audit {
		t.Fatalf("raw/audit lost: %+v", logs[0])
	}
	meeting, e := d.CreateMeeting(user, "test", 3, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.UpdateMeetingResult(meeting, "We use PostgreSQL.", "", "", "[]", audit); e != nil {
		t.Fatal(e)
	}
	record, e := d.GetMeeting(meeting)
	if e != nil || string(record.VocabularyAudit) != audit {
		t.Fatalf("meeting audit: %+v %v", record, e)
	}
}
