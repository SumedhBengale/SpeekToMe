package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"speektome-server/audiospool"
	"speektome-server/db"
	log "speektome-server/privacylog"
	"speektome-server/vocabularydecision"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"speektome/vocabulary"
)

// A committed meeting is processed entirely in memory, from the files in its
// sealed spool:
//
//	signed live transcript → gaps → ASR for the gaps only → vocabulary → Jev
//	   └ mic turns → LLM enhancement
//	diarization on the system audio, started first, in parallel
//	   └ system turns → LLM enhancement
//	merge → summary and action items → one final write
//
// Nothing is saved until the end. A restart re-runs the whole pipeline from
// the spool, so every step is safe to repeat.

// pipelineWord is a word on the recording timeline. Locked words are resolved
// vocabulary terms that the LLM must return unchanged.
type pipelineWord struct {
	Word       string
	Start, End float64
	Locked     bool
}

type meetingRun struct {
	j                       *MeetingJobs
	key, attempt, clientID  string
	name, description       string
	user, meeting           int64
	manifest                map[string]MeetingFile
	llmSlots                chan struct{}   // LLM calls this meeting may run at once
	speakerTokens           map[string]bool // participant tokens the summary may use
	participants            string          // the summary prompt's participant list
	gpu                     atomic.Bool     // holds a fleet meeting lease for gap ASR
	gapSeconds, asrRequests atomic.Int64
	jevCalls, llmRetries    atomic.Int64
	llmFallbacks            atomic.Int64
}

type meetingResult struct {
	turns   []*pipelineTurn
	audits  []vocabularydecision.Audit
	summary string
	actions string
}

// meetingError carries whether a failure is worth retrying.
type meetingError struct {
	message   string
	permanent bool
}

func (e *meetingError) Error() string { return e.message }

func permanentError(message string) error { return &meetingError{message, true} }

func (j *MeetingJobs) process(parent context.Context, key, attempt string) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	defer func() {
		result, err := j.db.Exec(`UPDATE meeting_jobs SET admitted=0 WHERE job_key=? AND status IN ('failed','cancelled')`, key)
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				j.releaseProcessing(key)
			}
		}
	}()
	run, err := j.loadRun(ctx, key, attempt)
	if err != nil {
		var failure *meetingError
		if errors.As(err, &failure) {
			j.retry(key, attempt, failure.message, failure.permanent)
		}
		return
	}
	defer run.releaseGPU()
	go run.renew(ctx, cancel)
	began := time.Now()
	result, err := run.execute(ctx)
	if err == nil {
		err = run.save(ctx, result)
	}
	if err != nil {
		var failure *meetingError
		permanent := errors.As(err, &failure) && failure.permanent
		message := err.Error()
		if ctx.Err() != nil {
			message = "meeting processing was interrupted"
		}
		j.retry(key, attempt, message, permanent)
		return
	}
	log.Printf("[MEETINGS] meeting %d processed in %.0fs: gap audio %ds over %d ASR requests, %d Jev calls, %d LLM retries, %d LLM fallbacks",
		run.meeting, time.Since(began).Seconds(), run.gapSeconds.Load(), run.asrRequests.Load(), run.jevCalls.Load(), run.llmRetries.Load(), run.llmFallbacks.Load())
}

func (j *MeetingJobs) loadRun(ctx context.Context, key, attempt string) (*meetingRun, error) {
	run := &meetingRun{j: j, key: key, attempt: attempt, llmSlots: make(chan struct{}, enhancePerMeeting)}
	var files string
	var meeting *int64
	err := j.db.QueryRowContext(ctx, `SELECT j.user_id,j.client_id,j.meeting_id,j.files_json,COALESCE(j.description,''),COALESCE(u.name,'') FROM meeting_jobs j JOIN users u ON u.id=j.user_id WHERE j.job_key=? AND j.attempt=? AND j.status='processing'`, key, attempt).Scan(&run.user, &run.clientID, &meeting, &files, &run.description, &run.name)
	if err != nil {
		return nil, err
	}
	if run.description != "" && j.app.DB.Privacy != nil {
		if run.description, err = j.app.DB.Privacy.OpenStored(ctx, run.user, "job-description", key, run.description); err != nil {
			return nil, err
		}
	}
	if meeting == nil || json.Unmarshal([]byte(files), &run.manifest) != nil {
		return nil, permanentError("meeting record is incomplete")
	}
	run.meeting = *meeting
	if run.name == "" {
		run.name = "You"
	}
	return run, nil
}

// renew keeps the job lease, and the fleet lease while gap ASR holds one. Losing
// either cancels the attempt; the next attempt starts over from the spool.
func (r *meetingRun) renew(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if r.gpu.Load() {
			renewCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			err := r.j.fleetCall(renewCtx, "/fleet/meetings/acquire", map[string]string{"id": r.key + "_" + r.attempt})
			stop()
			if err != nil {
				cancel()
				return
			}
		}
		res, err := r.j.db.ExecContext(ctx, `UPDATE meeting_jobs SET lease_until=? WHERE job_key=? AND attempt=? AND status IN ('processing','analyzing')`, time.Now().Add(90*time.Second).Unix(), r.key, r.attempt)
		if err != nil {
			cancel()
			return
		}
		if n, _ := res.RowsAffected(); n != 1 {
			cancel()
			return
		}
	}
}

func (r *meetingRun) execute(ctx context.Context) (meetingResult, error) {
	durations := map[string]float64{}
	for _, channel := range []string{"mic", "system"} {
		seconds, err := r.j.channelDuration(ctx, r.key, channel)
		if err != nil {
			if errors.Is(err, audiospool.ErrCorrupt) || errors.Is(err, audiospool.ErrInvalidAudio) {
				return meetingResult{}, permanentError("the uploaded recording is unreadable")
			}
			return meetingResult{}, err
		}
		durations[channel] = seconds
	}
	live := map[string]*liveChannel{"mic": {}, "system": {}}
	if _, ok := r.manifest["transcript"]; ok {
		// An unreadable transcript only costs GPU time: every part becomes a gap.
		if data, err := r.j.readChannel(ctx, r.key, "transcript", meetingTranscriptLimit); err == nil {
			live = parseLiveTranscript(data, r.user, r.clientID)
			clear(data)
		}
	}

	// Diarization is the longest step and needs only the audio, so it starts first.
	type diarization struct {
		speakers []jobSpeaker
		err      error
	}
	diarized := make(chan diarization, 1)
	go func() {
		speakers, err := r.j.diarization.run(ctx, r.key)
		diarized <- diarization{speakers, err}
	}()

	words, err := r.channelWords(ctx, live, durations)
	if err != nil {
		return meetingResult{}, err
	}
	terms, err := r.j.app.DB.GetActiveVocabularyTerms(r.user)
	if err != nil {
		return meetingResult{}, err
	}
	vocab := newMeetingVocabulary(terms)
	var resolved [2][]pipelineWord
	var audits [2][]vocabularydecision.Audit
	var wg sync.WaitGroup
	for i, channel := range []string{"mic", "system"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved[i], audits[i] = vocab.resolve(ctx, r, words[channel])
		}()
	}
	wg.Wait()

	// The microphone is always the user, so it needs no diarization.
	micTurns := buildTurns("mic", resolved[0], nil, r.name)
	micDone := make(chan struct{})
	go func() {
		defer close(micDone)
		r.enhance(ctx, micTurns)
	}()
	diarizedResult := <-diarized
	if diarizedResult.err != nil {
		<-micDone
		return meetingResult{}, diarizedResult.err
	}
	_, _ = r.j.db.ExecContext(ctx, `UPDATE meeting_jobs SET status='analyzing',error='' WHERE job_key=? AND attempt=? AND status='processing'`, r.key, r.attempt)
	systemTurns := buildTurns("system", resolved[1], diarizedResult.speakers, r.name)
	r.enhance(ctx, systemTurns)
	<-micDone
	if ctx.Err() != nil {
		return meetingResult{}, ctx.Err()
	}
	turns := append(micTurns, systemTurns...)
	sort.SliceStable(turns, func(a, b int) bool { return turns[a].Start < turns[b].Start })
	for i, turn := range turns {
		turn.ID = fmt.Sprintf("turn-%d", i)
	}
	summary, actions := r.summarize(ctx, turns)
	return meetingResult{turns: turns, audits: append(audits[0], audits[1]...), summary: summary, actions: actions}, nil
}

// Gaps over this share of a channel are transcribed as the whole channel.
const wholeChannelShare = 0.6

// channelWords returns each channel's words: the verified live transcript with
// ASR output spliced into whatever it did not cover.
func (r *meetingRun) channelWords(ctx context.Context, live map[string]*liveChannel, durations map[string]float64) (map[string][]pipelineWord, error) {
	gaps := map[string][]timeSpan{}
	for _, channel := range []string{"mic", "system"} {
		g := transcriptGaps(live[channel].Covered, durations[channel])
		if spanSeconds(g) > wholeChannelShare*durations[channel] || len(g) > maxGapRanges {
			g = []timeSpan{{0, durations[channel]}}
			if durations[channel] <= 0 {
				g = nil
			}
		}
		gaps[channel] = g
		r.gapSeconds.Add(int64(spanSeconds(g)))
	}
	out := map[string][]pipelineWord{}
	for channel, l := range live {
		for _, w := range l.Words {
			out[channel] = append(out[channel], pipelineWord{Word: w.Word, Start: w.Start, End: w.End})
		}
	}
	if len(gaps["mic"]) == 0 && len(gaps["system"]) == 0 {
		return out, nil
	}
	if err := r.acquireGPU(ctx); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var first error
	var wg sync.WaitGroup
	for channel, g := range gaps {
		if len(g) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			words, err := r.transcribeGaps(ctx, channel, g)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			merged := append(out[channel], words...)
			sort.SliceStable(merged, func(a, b int) bool { return merged[a].Start < merged[b].Start })
			out[channel] = merged
		}()
	}
	wg.Wait()
	r.releaseGPU()
	return out, first
}

// maxGapRanges bounds one gap request; more gaps than this go as one range.
const maxGapRanges = 512

// acquireGPU waits for a worker that can take this meeting's gap ASR.
func (r *meetingRun) acquireGPU(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Minute)
	for {
		leaseCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		err := r.j.fleetCall(leaseCtx, "/fleet/meetings/acquire", map[string]string{"id": r.key + "_" + r.attempt})
		stop()
		if err == nil {
			r.gpu.Store(true)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("waiting for meeting capacity")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

func (r *meetingRun) releaseGPU() {
	if !r.gpu.Swap(false) {
		return
	}
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = r.j.fleetCall(ctx, "/fleet/meetings/release", map[string]string{"id": r.key + "_" + r.attempt})
}

// transcribeGaps sends the whole channel with the gap ranges. The worker
// transcribes only those ranges (older workers transcribe everything), and only
// words whose midpoint falls inside a gap are kept.
func (r *meetingRun) transcribeGaps(ctx context.Context, channel string, gaps []timeSpan) ([]pipelineWord, error) {
	audio, size, err := r.j.openChannel(ctx, r.key, channel)
	if err != nil {
		return nil, permanentError("the uploaded recording is unreadable")
	}
	defer audio.Close()
	ranges := make([]string, len(gaps))
	for i, g := range gaps {
		ranges[i] = strconv.FormatFloat(g.Start, 'f', 2, 64) + "-" + strconv.FormatFloat(g.End, 'f', 2, 64)
	}
	query := "language=" + url.QueryEscape(r.j.language(r.key)) + "&ranges=" + url.QueryEscape(strings.Join(ranges, ","))
	request, err := http.NewRequestWithContext(ctx, "POST", r.j.fleet+"/v1/meeting/"+channel+"?"+query, audio)
	if err != nil {
		return nil, err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", "audio/ogg")
	request.Header.Set("Authorization", "Bearer "+r.j.token)
	request.Header.Set("X-SpeekToMe-Meeting-ID", r.key+"_"+r.attempt)
	r.asrRequests.Add(1)
	response, err := r.j.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		failure := fmt.Errorf("%s transcription returned HTTP %d", channel, response.StatusCode)
		switch response.StatusCode {
		case 400, 401, 403, 413, 422:
			return nil, permanentError(failure.Error())
		}
		return nil, failure
	}
	var transcript jobTranscript
	if json.Unmarshal(data, &transcript) != nil {
		return nil, errors.New("invalid worker output")
	}
	var words []pipelineWord
	for _, w := range transcript.Words {
		if insideSpans(gaps, (w.Start+w.End)/2) && strings.TrimSpace(w.Word) != "" {
			words = append(words, pipelineWord{Word: w.Word, Start: w.Start, End: w.End})
		}
	}
	return words, nil
}

// meetingVocabulary resolves the user's terms on the main server, in sections,
// with each candidate decided by Jev. Word timestamps survive: a replaced span
// takes the first replaced word's start and the last one's end.
type meetingVocabulary struct {
	compiled *vocabulary.UserVocabulary
	sha      string
}

const (
	vocabularyPause        = 1.5
	vocabularySectionWords = 250
	// Meetings use part of Jev's shared concurrency, leaving room for dictation.
	vocabularyParallel = 4
)

func newMeetingVocabulary(terms []db.VocabularyTerm) meetingVocabulary {
	var phrases []string
	for _, t := range terms {
		phrases = append(phrases, t.Canonical)
	}
	if len(phrases) == 0 {
		return meetingVocabulary{}
	}
	compiled, err := vocabulary.Compile(phrases)
	if err != nil {
		return meetingVocabulary{}
	}
	return meetingVocabulary{compiled, vocabulary.TermsHash(phrases)}
}

// Jev answers these immediately with a fallback when it is busy; for a meeting
// it is worth waiting and asking again.
var retryableJev = map[string]bool{"concurrency_limit": true, "rate_limit_cooldown": true, "local_rate_limit": true, "timeout": true, "network_error": true}

func (v meetingVocabulary) resolve(ctx context.Context, r *meetingRun, words []pipelineWord) ([]pipelineWord, []vocabularydecision.Audit) {
	if v.compiled == nil || len(words) == 0 {
		return words, nil
	}
	sections := splitSections(words)
	resolved := make([][]pipelineWord, len(sections))
	audits := make([]*vocabularydecision.Audit, len(sections))
	slots := make(chan struct{}, vocabularyParallel)
	var wg sync.WaitGroup
	for i, section := range sections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			resolved[i], audits[i] = v.resolveSection(ctx, r, section)
		}()
	}
	wg.Wait()
	var out []pipelineWord
	var list []vocabularydecision.Audit
	for i := range sections {
		out = append(out, resolved[i]...)
		if audits[i] != nil {
			list = append(list, *audits[i])
		}
	}
	return out, list
}

// splitSections cuts words at pauses or a word limit, never inside a word.
func splitSections(words []pipelineWord) [][]pipelineWord {
	var sections [][]pipelineWord
	begin := 0
	for i := 1; i <= len(words); i++ {
		if i == len(words) || words[i].Start-words[i-1].End >= vocabularyPause || i-begin >= vocabularySectionWords {
			sections = append(sections, words[begin:i])
			begin = i
		}
	}
	return sections
}

func (v meetingVocabulary) resolveSection(ctx context.Context, r *meetingRun, words []pipelineWord) ([]pipelineWord, *vocabularydecision.Audit) {
	var b strings.Builder
	starts, ends := make([]int, len(words)), make([]int, len(words))
	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}
		starts[i] = b.Len()
		b.WriteString(w.Word)
		ends[i] = b.Len()
	}
	raw := b.String()
	matchCtx, stop := context.WithTimeout(ctx, time.Second)
	proposal := vocabulary.Propose(matchCtx, raw, v.compiled, v.sha)
	stop()
	if len(proposal.Choices) == 0 {
		return words, nil
	}
	var audit vocabularydecision.Audit
	for attempt := 0; ; attempt++ {
		r.jevCalls.Add(1)
		audit = vocabularydecision.Default().Evaluate(ctx, proposal, "main_meeting")
		retry := false
		for _, d := range audit.Decisions {
			retry = retry || retryableJev[d.Fallback]
		}
		if !retry || attempt == 2 || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(500<<attempt) * time.Millisecond):
		}
	}
	out := append([]pipelineWord(nil), words...)
	decisions := append([]vocabularydecision.Decision(nil), audit.Decisions...)
	sort.Slice(decisions, func(a, b int) bool { return decisions[a].StartByte > decisions[b].StartByte })
	for _, d := range decisions {
		first, last := -1, -1
		for i := range words {
			if ends[i] > d.StartByte && starts[i] < d.EndByte {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 || d.StartByte < starts[first] || d.EndByte > ends[last] {
			continue
		}
		value := d.Original
		if d.UseVocabulary {
			value = d.Replacement
		}
		token := pipelineWord{Word: raw[starts[first]:d.StartByte] + value + raw[d.EndByte:ends[last]], Start: words[first].Start, End: words[last].End, Locked: true}
		out = append(append(out[:first:first], token), out[last+1:]...)
	}
	return out, &audit
}

// save writes the finished meeting and completes the job in one transaction.
func (r *meetingRun) save(ctx context.Context, result meetingResult) error {
	segments := []MeetingSegment{}
	var lines []string
	for _, turn := range result.turns {
		if strings.TrimSpace(turn.Text) == "" {
			continue
		}
		segments = append(segments, MeetingSegment{Start: turn.Start, End: turn.End, Text: turn.Text, Speaker: turn.SpeakerName, SpeakerID: turn.SpeakerID, Channel: turn.Channel})
		lines = append(lines, turn.SpeakerName+": "+turn.Text)
	}
	data, _ := json.Marshal(segments)
	audit, _ := json.Marshal(map[string]any{"sections": result.audits})
	fields := map[string]string{"transcript": strings.Join(lines, "\n"), "segments_json": string(data), "summary": result.summary, "action_items": result.actions, "vocabulary_audit": string(audit), "description": r.description}
	if r.j.app.DB.Privacy != nil {
		if err := r.j.app.DB.Privacy.MeetingFields(ctx, r.user, r.meeting, fields, true); err != nil {
			return err
		}
	}
	tx, err := r.j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE meetings SET transcript=?,segments_json=?,summary=?,action_items=?,vocabulary_audit=?,description=? WHERE id=? AND user_id=?`, fields["transcript"], fields["segments_json"], fields["summary"], fields["action_items"], fields["vocabulary_audit"], fields["description"], r.meeting, r.user)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE meeting_jobs SET status='completed',admitted=0,lease_until=0,error='',mic_result='',system_result='',diar_result='' WHERE job_key=? AND attempt=? AND status IN ('processing','analyzing')`, r.key, r.attempt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("job ownership changed before completion")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.j.releaseProcessing(r.key)
	return nil
}

// envLimit reads a positive bound from the environment.
func envLimit(name string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 && n <= 1024 {
		return n
	}
	return fallback
}
