package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"speektome-server/auth"
	"speektome-server/db"
)

type meetingSpeaker struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DefaultName string `json:"default_name"`
	IsMe        bool   `json:"is_me"`
}
type meetingSpeakerView struct {
	db.Meeting
	Speakers []meetingSpeaker `json:"speakers"`
}
type namedMeetingSegment struct {
	MeetingSegment
	SpeakerKey  string `json:"speaker_key"`
	DisplayName string `json:"speaker_display_name"`
}

var numberedSpeakerName = regexp.MustCompile(`(?i)^(?:speaker|spk)[ _-]*([0-9]+)$`)
var numberedSpeakerID = regexp.MustCompile(`(?i)^spk[_-]?([0-9]+)$`)

// Mirrors the client fallback for older meetings that predate channel/ID metadata.
func meetingSpeakerIdentity(s MeetingSegment, owner string) meetingSpeaker {
	raw := strings.TrimSpace(strings.NewReplacer("[", "", "]", "").Replace(s.Speaker))
	clean, channel, source := strings.ToLower(raw), strings.ToLower(s.Channel), strings.TrimSpace(s.SpeakerID)
	number := numberedSpeakerName.FindStringSubmatch(clean)
	if number == nil {
		number = numberedSpeakerID.FindStringSubmatch(source)
	}
	localAlias := clean == "me" || clean == "you" || clean == "local speaker"
	remote := channel == "system" || channel == "loopback"
	local := channel == "mic" || (!remote && number == nil && (source == "speaker_local" || (source == "" && (localAlias || (owner != "" && clean == strings.ToLower(strings.TrimSpace(owner)))))))
	if local {
		return meetingSpeaker{ID: "mic:speaker_local", Name: "Me", DefaultName: "Me", IsMe: true}
	}
	if source == "" {
		if number != nil {
			source = "spk_" + number[1]
		} else {
			source = clean
		}
	}
	if source == "" {
		source = "unknown"
	}
	label := raw
	if label == "" {
		if number != nil {
			label = "Speaker " + number[1]
		} else {
			label = "Unknown speaker"
		}
	}
	if localAlias {
		label += " (System)"
	}
	return meetingSpeaker{ID: "system:" + source, Name: label, DefaultName: label}
}

func prepareMeetingSpeakerView(m db.Meeting, owner string) meetingSpeakerView {
	if m.SpeakerNames == nil {
		m.SpeakerNames = map[string]string{}
	}
	view := meetingSpeakerView{Meeting: m, Speakers: []meetingSpeaker{}}
	var segments []MeetingSegment
	if json.Unmarshal([]byte(m.SegmentsJSON), &segments) != nil {
		return view
	}
	named := make([]namedMeetingSegment, 0, len(segments))
	seen := map[string]bool{}
	// Ambiguous original labels must never cause us to rename the wrong person.
	labelIDs := map[string]string{}
	addLabel := func(label, id string) {
		if label == "" {
			return
		}
		if old, exists := labelIDs[label]; exists && old != id {
			labelIDs[label] = ""
		} else if !exists {
			labelIDs[label] = id
		}
	}
	for _, segment := range segments {
		speaker := meetingSpeakerIdentity(segment, owner)
		if name := m.SpeakerNames[speaker.ID]; name != "" {
			speaker.Name = name
		}
		if !seen[speaker.ID] {
			view.Speakers = append(view.Speakers, speaker)
			seen[speaker.ID] = true
		}
		named = append(named, namedMeetingSegment{segment, speaker.ID, speaker.Name})
		addLabel(strings.TrimSpace(strings.NewReplacer("[", "", "]", "").Replace(segment.Speaker)), speaker.ID)
		addLabel(speaker.DefaultName, speaker.ID)
	}
	replacements := map[string]string{}
	for label, id := range labelIDs {
		if id != "" && m.SpeakerNames[id] != "" {
			replacements[label] = m.SpeakerNames[id]
		}
	}
	// Always derive text from canonical stored output, never from the last renamed output.
	view.ActionItems = replaceMeetingSpeakerLabels(m.ActionItems, replacements)
	view.Summary = replaceMeetingSpeakerLabels(m.Summary, replacements)
	view.Transcript = replaceMeetingSpeakerLabels(m.Transcript, replacements)
	data, _ := json.Marshal(named)
	view.SegmentsJSON = string(data)
	return view
}

// One pass, longest labels first: Speaker 1 cannot corrupt Speaker 10 or cascade
// into another replacement when two names are swapped. Unicode boundaries also
// protect names embedded in other words.
func replaceMeetingSpeakerLabels(text string, names map[string]string) string {
	labels := make([]string, 0, len(names))
	for label := range names {
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return text
	}
	sort.Slice(labels, func(i, j int) bool { return len(labels[i]) > len(labels[j]) })
	for i := range labels {
		labels[i] = regexp.QuoteMeta(labels[i])
	}
	re := regexp.MustCompile(strings.Join(labels, "|"))
	var out strings.Builder
	last := 0
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }
	for _, match := range re.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (start > 0 && isWord(before)) || (end < len(text) && isWord(after)) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteString(names[text[start:end]])
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

func (a *App) HandleSaveMeetingSpeakers(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}
	meeting, err := a.DB.GetMeeting(id)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}
	if meeting.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	var request struct {
		Names    map[string]string `json:"speaker_names"`
		Previous map[string]string `json:"previous_names"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Names == nil || request.Previous == nil {
		http.Error(w, "Invalid speaker names", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	view := prepareMeetingSpeakerView(*meeting, user.Name)
	allowed := map[string]bool{}
	for _, speaker := range view.Speakers {
		allowed[speaker.ID] = true
	}
	for id, name := range request.Names {
		name = strings.TrimSpace(name)
		if !allowed[id] || utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			http.Error(w, "Use a speaker from this meeting and a name of at most 80 characters", http.StatusBadRequest)
			return
		}
		if name == "" {
			delete(request.Names, id)
		} else {
			request.Names[id] = name
		}
	}
	if !reflect.DeepEqual(request.Previous, view.SpeakerNames) {
		http.Error(w, "Speaker names changed elsewhere. Reopen the meeting and try again.", http.StatusConflict)
		return
	}
	names, _ := json.Marshal(request.Names)
	previous, _ := json.Marshal(view.SpeakerNames)
	saved, err := a.DB.SaveMeetingSpeakerNames(id, user.ID, string(names), string(previous))
	if err != nil {
		http.Error(w, "Failed to save speaker names", http.StatusInternalServerError)
		return
	}
	if !saved {
		http.Error(w, "Meeting changed. Reopen it and try again.", http.StatusConflict)
		return
	}
	meeting.SpeakerNames = request.Names
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prepareMeetingSpeakerView(*meeting, user.Name))
}
