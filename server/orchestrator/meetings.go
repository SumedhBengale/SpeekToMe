package orchestrator

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"time"
)

type meetingLease struct {
	Worker  string    `json:"worker"`
	Expires time.Time `json:"expires"`
}

var meetingIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func (o *Controller) acquireMeeting(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request) != nil || !meetingIDPattern.MatchString(request.ID) {
		http.Error(w, "invalid job id", 400)
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.meetingLeases == nil {
		o.meetingLeases = map[string]meetingLease{}
	}
	if lease, ok := o.meetingLeases[request.ID]; ok && lease.Expires.After(o.now()) {
		worker := o.state.Workers[lease.Worker]
		if worker.State == StateActive && worker.Metadata.Ready && !worker.Metadata.Draining {
			lease.Expires = o.now().Add(90 * time.Second)
			o.meetingLeases[request.ID] = lease
			json.NewEncoder(w).Encode(lease)
			return
		}
		http.Error(w, "assigned worker unavailable", 503)
		return
	}
	occupied := map[string]bool{}
	for id, lease := range o.meetingLeases {
		if lease.Expires.Before(o.now()) {
			delete(o.meetingLeases, id)
		} else {
			occupied[lease.Worker] = true
		}
	}
	var chosen SpotWorker
	for _, worker := range o.state.Workers {
		if (!o.cfg.MeetingRequireIdle || max(worker.Metadata.Active, o.reservations[worker.InstanceID]) == 0) && !occupied[worker.InstanceID] && worker.Metadata.Batch == 0 && worker.State == StateActive && worker.Metadata.Ready && !worker.Metadata.Draining && o.now().Sub(worker.LastHealthy) < 30*time.Second {
			if chosen.InstanceID == "" || worker.Metadata.Active < chosen.Metadata.Active {
				chosen = worker
			}
		}
	}
	if chosen.InstanceID == "" {
		http.Error(w, "no meeting capacity", 503)
		return
	}
	lease := meetingLease{chosen.InstanceID, o.now().Add(90 * time.Second)}
	o.meetingLeases[request.ID] = lease
	json.NewEncoder(w).Encode(lease)
}
func (o *Controller) releaseMeeting(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request) != nil {
		http.Error(w, "invalid job", 400)
		return
	}
	o.mu.Lock()
	delete(o.meetingLeases, request.ID)
	o.mu.Unlock()
	w.WriteHeader(204)
}
func (o *Controller) proxyMeeting(p *HTTPProbe, w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-SpeekToMe-Meeting-ID")
	o.mu.RLock()
	lease, ok := o.meetingLeases[id]
	worker := o.state.Workers[lease.Worker]
	o.mu.RUnlock()
	if !ok || lease.Expires.Before(o.now()) || worker.State != StateActive || !worker.Metadata.Ready || worker.Metadata.Draining {
		http.Error(w, "meeting lease unavailable", 503)
		return
	}
	client, e := p.Client(r.Context(), worker)
	if e != nil {
		http.Error(w, "worker unavailable", 503)
		return
	}
	request, e := http.NewRequestWithContext(r.Context(), r.Method, worker.EndpointURL+r.URL.RequestURI(), http.MaxBytesReader(w, r.Body, 512<<20))
	if e != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	request.Header.Set("X-SpeekToMe-Meeting-ID", id)
	longClient := *client
	longClient.Timeout = 0
	transport := client.Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 0
	defer transport.CloseIdleConnections()
	longClient.Transport = transport
	response, e := longClient.Do(request)
	if e != nil {
		http.Error(w, "worker request interrupted", 502)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	io.Copy(w, response.Body)
}
