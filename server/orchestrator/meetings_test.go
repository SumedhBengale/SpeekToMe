package orchestrator

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMeetingLeaseDoesNotRequireFreeLiveSlots(t *testing.T) {
	now := time.Now()
	o := &Controller{now: func() time.Time { return now }, state: persistedState{Workers: map[string]SpotWorker{"one": {InstanceID: "one", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 20, Max: 20}}, "two": {InstanceID: "two", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 20, Max: 20}}}}}
	acquire := func(id string) (int, meetingLease) {
		w := httptest.NewRecorder()
		o.acquireMeeting(w, httptest.NewRequest("POST", "/fleet/meetings/acquire", strings.NewReader(`{"id":"`+id+`"}`)))
		var lease meetingLease
		json.Unmarshal(w.Body.Bytes(), &lease)
		return w.Code, lease
	}
	code, first := acquire("first")
	if code != 200 {
		t.Fatal(code)
	}
	code, second := acquire("second")
	if code != 200 || first.Worker == second.Worker {
		t.Fatal(code, first, second)
	}
	if code, _ := acquire("third"); code != 503 {
		t.Fatal(code)
	}
	now = now.Add(100 * time.Second)
	for id, worker := range o.state.Workers {
		worker.LastHealthy = now
		o.state.Workers[id] = worker
	}
	if code, _ := acquire("third"); code != 200 {
		t.Fatal("expired lease not reclaimed", code)
	}
}

func TestIdleMeetingLeaseReservesRoutingBeforeUpload(t *testing.T) {
	now := time.Now()
	o := &Controller{cfg: FleetConfig{MeetingRequireIdle: true}, now: func() time.Time { return now }, reservations: map[string]int{}, state: persistedState{Workers: map[string]SpotWorker{"idle": {InstanceID: "idle", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 0, Max: 20}}, "busy": {InstanceID: "busy", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 1, Max: 20}}}}}
	w := httptest.NewRecorder()
	o.acquireMeeting(w, httptest.NewRequest("POST", "/fleet/meetings/acquire", strings.NewReader(`{"id":"meeting"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var lease meetingLease
	json.Unmarshal(w.Body.Bytes(), &lease)
	if lease.Worker != "idle" || o.reserve("idle") {
		t.Fatal("live work reached reserved idle worker", lease)
	}
	candidates := o.Candidates()
	if len(candidates) != 1 || candidates[0].InstanceID != "busy" {
		t.Fatal(candidates)
	}
	w = httptest.NewRecorder()
	o.acquireMeeting(w, httptest.NewRequest("POST", "/fleet/meetings/acquire", strings.NewReader(`{"id":"next"}`)))
	if w.Code != 503 {
		t.Fatal("meeting reached busy worker", w.Code)
	}
}

func TestLiveRoutingPrefersMeetingFreeWorkerWithoutMovingStreams(t *testing.T) {
	now := time.Now()
	o := &Controller{now: func() time.Time { return now }, reservations: map[string]int{}, meetingLeases: map[string]meetingLease{"meeting": {Worker: "quiet", Expires: now.Add(time.Minute)}}, state: persistedState{Workers: map[string]SpotWorker{"quiet": {InstanceID: "quiet", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 1, Max: 20}}, "busy": {InstanceID: "busy", State: StateActive, LastHealthy: now, Metadata: WorkerMetadata{Ready: true, Active: 10, Max: 20}}}}}
	if list := o.Candidates(); len(list) != 2 || list[0].InstanceID != "busy" {
		t.Fatalf("meeting-free preference: %+v", list)
	}
	// Both hosting meetings: fall back to ordinary proportional live load.
	o.meetingLeases["second"] = meetingLease{Worker: "busy", Expires: now.Add(time.Minute)}
	if list := o.Candidates(); len(list) != 2 || list[0].InstanceID != "quiet" {
		t.Fatalf("both meetings: %+v", list)
	}
	if o.state.Workers["quiet"].Metadata.Active != 1 || o.state.Workers["busy"].Metadata.Active != 10 {
		t.Fatal("preference mutated existing streams")
	}
	// A full meeting-free worker does not prevent use of spare meeting-worker slots.
	worker := o.state.Workers["busy"]
	worker.Metadata.Active = 20
	o.state.Workers["busy"] = worker
	delete(o.meetingLeases, "second")
	if list := o.Candidates(); len(list) != 1 || list[0].InstanceID != "quiet" {
		t.Fatal(list)
	}
}
