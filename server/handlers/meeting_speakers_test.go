package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"speektome-server/db"
)

func TestMeetingSpeakerNamesPersistAndFollowAttribution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meetings.db")
	database, err := db.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	owner, err := database.GetOrCreateUser("owner", "owner@example.invalid", "Meeting Owner")
	if err != nil {
		t.Fatal(err)
	}
	other, err := database.GetOrCreateUser("other", "other@example.invalid", "Other")
	if err != nil {
		t.Fatal(err)
	}
	for token, user := range map[string]int64{"owner-token": owner, "other-token": other} {
		if err = database.CreateSession(token, user, 0); err != nil {
			t.Fatal(err)
		}
	}
	id, err := database.CreateMeeting(owner, "Rename test", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	// Includes old records without channel/ID metadata and a new structured turn.
	segments := `[{"speaker":"Speaker 1","start":0,"end":1,"text":"one"},{"speaker":"Speaker 10","start":1,"end":2,"text":"ten"},{"speaker":"Meeting Owner","start":2,"end":3,"text":"mine"},{"speaker":"Speaker 0","channel":"system","speaker_id":"spk_0","start":3,"end":4,"text":"zero"}]`
	actions := "- Owner: Speaker 1\n- Owner: Speaker 10\n- Owner: Meeting Owner\n- Owner: Speaker 0\n- Not Speaker 100 or Speaker 1st"
	if err = database.UpdateMeetingResult(id, "[Speaker 1]: hello", "Speaker 10 will follow up.", actions, segments); err != nil {
		t.Fatal(err)
	}
	app := &App{DB: database}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /meetings/{id}/speakers", app.HandleSaveMeetingSpeakers)
	mux.HandleFunc("GET /meetings/{id}", app.HandleGetMeetingDetail)
	mux.HandleFunc("GET /meetings", app.HandleGetMeetings)
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	endpoint := fmt.Sprintf("/meetings/%d/speakers", id)
	body := `{"speaker_names":{"system:spk_1":"Alice","system:spk_10":"Béla","mic:speaker_local":"Sam","system:spk_0":"Chen"},"previous_names":{}}`
	for token, code := range map[string]int{"": 401, "other-token": 403} {
		if got := request("PUT", endpoint, token, body); got.Code != code {
			t.Fatalf("auth %s: %d %s", token, got.Code, got.Body.String())
		}
	}
	for _, invalid := range []string{
		`{"speaker_names":{"system:spk_99":"Intruder"},"previous_names":{}}`,
		`{"speaker_names":{"system:spk_1":"Line\nbreak"},"previous_names":{}}`,
		`{"speaker_names":{"system:spk_1":"` + strings.Repeat("a", 81) + `"},"previous_names":{}}`,
		`{"speaker_names":{},"previous_names":{}} {}`,
	} {
		if got := request("PUT", endpoint, "owner-token", invalid); got.Code != 400 {
			t.Fatalf("invalid input: %d %s", got.Code, got.Body.String())
		}
	}
	got := request("PUT", endpoint, "owner-token", body)
	if got.Code != 200 {
		t.Fatalf("save: %d %s", got.Code, got.Body.String())
	}
	var saved meetingSpeakerView
	if err = json.Unmarshal(got.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ActionItems != "- Owner: Alice\n- Owner: Béla\n- Owner: Sam\n- Owner: Chen\n- Not Speaker 100 or Speaker 1st" {
		t.Fatal(saved.ActionItems)
	}
	if len(saved.Speakers) != 4 || saved.Speakers[2].ID != "mic:speaker_local" || !saved.Speakers[2].IsMe || saved.Speakers[3].IsMe {
		t.Fatalf("identities: %+v", saved.Speakers)
	}
	var named []namedMeetingSegment
	json.Unmarshal([]byte(saved.SegmentsJSON), &named)
	if named[0].Speaker != "Speaker 1" || named[0].SpeakerKey != "system:spk_1" || named[0].DisplayName != "Alice" {
		t.Fatalf("canonical identity lost: %+v", named[0])
	}
	if saved.Summary != "Béla will follow up." || saved.Transcript != "[Alice]: hello" {
		t.Fatal(saved)
	}
	// Stale clients cannot silently overwrite a newer save.
	if got = request("PUT", endpoint, "owner-token", body); got.Code != 409 {
		t.Fatalf("stale update: %d", got.Code)
	}
	// Close and reopen the database, just like a server restart.
	database.Close()
	database, err = db.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	app.DB = database
	got = request("GET", fmt.Sprintf("/meetings/%d", id), "owner-token", "")
	if got.Code != 200 {
		t.Fatal(got.Code)
	}
	var reloaded meetingSpeakerView
	json.Unmarshal(got.Body.Bytes(), &reloaded)
	if reloaded.ActionItems != saved.ActionItems || reloaded.SpeakerNames["system:spk_1"] != "Alice" {
		t.Fatal("names lost on restart")
	}
	// Swap names and reset the local alias: changes derive from original labels.
	update, _ := json.Marshal(map[string]any{"speaker_names": map[string]string{"system:spk_1": "Béla", "system:spk_10": "Alice"}, "previous_names": saved.SpeakerNames})
	got = request("PUT", endpoint, "owner-token", string(update))
	if got.Code != 200 {
		t.Fatalf("rename again: %d %s", got.Code, got.Body.String())
	}
	json.Unmarshal(got.Body.Bytes(), &reloaded)
	if !strings.HasPrefix(reloaded.ActionItems, "- Owner: Béla\n- Owner: Alice\n- Owner: Meeting Owner\n- Owner: Speaker 0") {
		t.Fatal(reloaded.ActionItems)
	}
	canonical, err := database.GetMeeting(id)
	if err != nil || canonical.ActionItems != actions || canonical.SegmentsJSON != segments {
		t.Fatal("canonical output was overwritten", err)
	}
	got = request("GET", "/meetings", "owner-token", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"speaker_display_name":"Béla"`) && !strings.Contains(got.Body.String(), `\"speaker_display_name\":\"Béla\"`) {
		t.Fatalf("list: %d %s", got.Code, got.Body.String())
	}
	// Compare-and-set also guards a race after the handler has read the record.
	ok, err := database.SaveMeetingSpeakerNames(id, owner, `{}`, `{}`)
	if err != nil || ok {
		t.Fatal("concurrent write was accepted", err)
	}
}

func TestMeetingSpeakerReplacementDoesNotCascadeOrMatchSubstrings(t *testing.T) {
	names := map[string]string{"Speaker 1": "Speaker 10", "Speaker 10": "Speaker 1", "Ann": "Zoë"}
	got := replaceMeetingSpeakerLabels("Speaker 1 / Speaker 10 / Speaker 100 / Speaker 1st / Ann / Anna / Ann's / अAnn", names)
	want := "Speaker 10 / Speaker 1 / Speaker 100 / Speaker 1st / Zoë / Anna / Zoë's / अAnn"
	if got != want {
		t.Fatalf("%q != %q", got, want)
	}
	m := db.Meeting{SegmentsJSON: `[{"speaker":"Me","channel":"mic"},{"speaker":"Me","channel":"system","speaker_id":"spk_0"}]`, ActionItems: "Me must respond", SpeakerNames: map[string]string{"mic:speaker_local": "Alice"}}
	view := prepareMeetingSpeakerView(m, "")
	if view.ActionItems != m.ActionItems || view.Speakers[0].Name != "Alice" || view.Speakers[1].Name != "Me (System)" {
		t.Fatal("ambiguous label was attributed", view)
	}
}

func TestSkippingSpeakerNamesMarksTheMeetingReviewed(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "reviewed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	owner, err := database.GetOrCreateUser("owner", "owner@example.invalid", "Meeting Owner")
	if err != nil {
		t.Fatal(err)
	}
	database.CreateSession("owner-token", owner, 0)
	id, err := database.CreateMeeting(owner, "Review test", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	segments := `[{"speaker":"Speaker 2","channel":"system","speaker_id":"spk_2","start":0,"end":1,"text":"hi"}]`
	if err = database.UpdateMeetingResult(id, "Speaker 2: hi", "", "", segments); err != nil {
		t.Fatal(err)
	}
	if meeting, _ := database.GetMeeting(id); meeting.SpeakersReviewed {
		t.Fatal("a new meeting starts unreviewed")
	}
	app := &App{DB: database}
	r := httptest.NewRequest("PUT", fmt.Sprintf("/meetings/%d/speakers", id), strings.NewReader(`{"speaker_names":{},"previous_names":{},"reviewed":true}`))
	r.SetPathValue("id", fmt.Sprint(id))
	r.Header.Set("Authorization", "Bearer owner-token")
	w := httptest.NewRecorder()
	app.HandleSaveMeetingSpeakers(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"speakers_reviewed":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	meeting, err := database.GetMeeting(id)
	if err != nil || !meeting.SpeakersReviewed || len(meeting.SpeakerNames) != 0 {
		t.Fatal("skipping must mark the review without naming anyone", meeting, err)
	}
}
