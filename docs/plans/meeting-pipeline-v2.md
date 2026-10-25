# Meeting pipeline v2: the live transcript is the final transcript

Status: server implemented (Parts 1–3 server side, Part 5, Part 6 server tests), except the meeting description (2.3) and the speaker review flag (Part 9). Not yet built: gateway `ranges` (Part 4) and the desktop client (1.5, 1.6, 2.1, Part 9). Scope: desktop client (`windows-client`), Go server (`server`), worker gateway (`deployment/spot-worker/gateway`).

## Why

Today every meeting is transcribed twice. The live stream (`/v1/meetings/live`) is a display-only preview that the desktop discards. After upload, the server sends the full `mic` and `system` recordings to a GPU worker, which transcribes them again from scratch (`gateway/meeting.go`). The second pass is the only one saved. This started on Oct 24 in `dd7b350`.

After this change:
- The live transcript, signed by the server and kept by the desktop on disk, becomes the final transcript.
- Only the parts the live stream missed (gaps) are re-transcribed.
- The end-of-meeting pipeline runs entirely in memory on the main server, and writes the finished meeting once.

## Facts this plan relies on (verified in code)

- **Timestamped final words already exist.** The worker's streaming ASR emits final words with start/end times. `services/fleet_stream.go` already uses them for crash replay, then flattens them to text (`TextChan`). Worker crashes mid-stream are already recovered by replaying journaled audio to a new worker on the same timeline (`baseOffset`).
- **Live audio is sample-identical to the recording.** The desktop pushes the same echo-cancelled samples to the live feed and to the recorded files (`audio.rs:373`). It sends fixed 560 ms chunks (8,960 samples), each encoded as a standalone Ogg Opus blob. So chunk N starts at exactly N × 0.56 s on the recording's timeline.
- **The client never reconnects.** One dropped live WebSocket ends live transcription for the rest of the meeting (`meeting_stream.rs`).
- **Vocabulary choices are byte spans on the raw text** (`shared/vocabulary` `Choice.StartByte/EndByte`), so resolved terms map back to word timestamps mechanically.
- **Jev is Typesafe's external API**, with a 400 ms timeout and an RPS/TPS limiter in `vocabularydecision.Client`. The main-server matcher fallback (`vocabulary.Propose`) already exists.
- **The archive needs no change.** The vault archives only the `mixed` file, as maintenance after completion (`meeting_spool.go`).

## Design decisions (agreed)

1. **The desktop is the source of truth.** It keeps the audio files and the signed live transcript on disk until the server reports the meeting completed, and retries on any failure.
2. **The server signs every live word batch (HMAC).** Unsigned or invalid batches count as gaps, so a modified client cannot inject text or fake coverage.
3. **No transcript content is written to the database until the final pass.** The bookkeeping writes stay: the job row status for polling, the quota charge at commit, and the empty meeting row created at commit.
4. **The encrypted upload spool stays, for uploads only.** Resumable 4 MB pieces have to survive between requests. The signed transcript is uploaded as a fourth spooled file, so after a restart the pipeline can re-run from the spool with no client involvement.
5. **Vocabulary → Jev runs at the end, not live.** It gets full context, retries are possible, and gap words use the same path. It runs alongside diarization, so it adds no wait.
6. **Diarization starts first, in parallel.** System-channel LLM enhancement and the summary wait for it; mic-channel enhancement does not.

## Target flow

```
DESKTOP (during the meeting)
  capture mic + system → recorded .ogg files (unchanged)
                       → 560 ms numbered chunks → /v1/meetings/live (protocol 2)
  ← "turn" events (preview, unchanged)
  ← "words" events (signed batches) → appended to <meeting>.live.jsonl
  reconnects automatically; resends from a 2-minute buffer

DESKTOP (after the meeting)
  upload mic, system, mixed, transcript(.live.jsonl) → commit → poll status

SERVER (after commit, all in memory)
 ├─ load + verify signed transcript → coverage → gaps
 │     └─ gaps only: ASR on a worker for those ranges
 │           └─ vocabulary (main server) → Jev    [both channels, word times kept]
 │                 └─ mic turns → chunked LLM enhancement
 └─ diarization on Modal (system audio, starts immediately)
          ↓ both done
   system words → speaker turns → chunked LLM enhancement
          ↓
   merge by time → summary + action items → one final UPDATE → completed
```

---

## Part 1: live protocol v2 (desktop ↔ main server)

### 1.1 Start message

```json
{"type":"start","protocol":2,"job_id":"<client meeting uuid>","title":"…","language":"en",
 "sample_rate":16000,"channels":1,
 "resume":{"mic":{"seq":0,"covered_to":0},"system":{"seq":0,"covered_to":0}}}
```

- `job_id` is the meeting record's UUID. The client creates it before the stream starts; today it's created afterwards (`main.rs:1223`, moves up).
- `resume` is all zeros on the first connect. On a reconnect it is:
  - `seq`: the first chunk the client will send;
  - `covered_to`: the last coverage end the client has stored for that channel.
- If `protocol` is absent, the server falls back to v1, today's preview-only relay, so old clients keep working.

### 1.2 Audio frames

Binary frame: `[channel u8][seq u32 big-endian][ogg opus bytes]`.

- **Numbering:** `seq` counts per channel from 0.
- **Timing:** chunk `seq` starts at `seq × 0.56 s`. The final chunk may be shorter.
- **Validation:** the server rejects frames whose seq goes backwards within one connection.

### 1.3 Server → client events

Unchanged: `session_ready`, `turn`, `live_complete`, `error`.

New: `words`.

```json
{"type":"words","batch":{
   "v":1,"kid":"k1","job_id":"…","channel":"mic","session":"<16-byte hex>","index":7,
   "from":123.20,"to":131.04,
   "words":[{"w":"hello","s":123.44,"e":123.71}, …]},
 "sig":"<base64 HMAC-SHA256>"}
```

- **Coverage range:** `from`/`to` is the range of the recording timeline this batch confirms. `from` is the previous checkpoint of this upstream session, or the session's start offset for its first batch. `to` is the new confirmed checkpoint.
- **Silence:** silence-only final events also produce a batch, with an empty `words` list. Without them, silences would look like gaps.
- **Numbering:** `session` is random per upstream ASR session, and `index` increases within a session.
- **Signature:** `HMAC-SHA256(key[kid], "speektome-live-v1\n" + user_id + "\n" + canonical_json(batch))`. `user_id` is bound in, but not sent, so a transcript can't be replayed across accounts.
- **Keys:** `SPEEKTOME_LIVE_TRANSCRIPT_KEY` (32 bytes, base64) with id `k1`. An optional `SPEEKTOME_LIVE_TRANSCRIPT_KEY_PREVIOUS` is accepted for verification during rotation. The new key goes in `server/.env`, gets a placeholder in `server/.env.example`, and is set in the main server's `.env`.

### 1.4 Server changes (`server/services/fleet_stream.go`, `server/handlers/meetings_stream.go`)

**Stream client:**
- Add `WordsChan chan WordBatch` to `UpstreamStreamingSession` and publish there wherever `confirmed` advances: the `fresh` words, plus silence advances.
- Add the session's start offset, so timestamps land on the recording timeline: `baseOffset` starts at `resume.seq × 0.56`.
- `TextChan` is unchanged and still drives the preview turns.

**Live relay:**
- **v2 start:** validate `job_id` with `clientMeetingID`. Drop words whose midpoint is before `resume.covered_to`; these are the overlap the client resent for ASR context.
- **Signing:** sign each batch and send it as a `words` event.
- **Reconnects:** a new session for the same `job_id` cancels the stale one in this process and waits for it to release its account lease, then takes the lease itself. (Keying the lease by `job_id` instead would let the stale session's deferred release delete the new session's lease.) The `meeting_live` rate limit stays at 10 connections a minute; the desktop's backoff (1, 2, 4, 8, then 15 s) stays under it.
- **End of meeting:** on `end_of_meeting`, commit both upstream sessions, wait up to 15 s for their last final words, send those batches, then `live_complete`.
- **Fallback provider:** if the live provider isn't the fleet (no timestamps), send no `words` events. The whole meeting then becomes gaps, which is today's cost and still correct.

### 1.5 Desktop changes (`meeting_stream.rs`, `audio.rs`, `main.rs`)

- **Job ID:** create the meeting record UUID before starting the stream and pass it to `start_meeting_streaming_session`.
- **Chunk numbering:** `LiveFeed` tags each chunk with a per-channel `seq`.
- **Buffer:** keep a per-channel ring of the last 2 minutes of chunks, about 215 chunks and ~360 KB per channel at 24 kbps.
- **Reconnects:**
  - On socket error or close (not `live_complete`), reconnect with backoff of 1, 2, 4, 8, then 15 s repeating, for as long as the meeting runs.
  - Resume from `floor(covered_to / 0.56) − 8` per channel. The 8 chunks are 4.48 s of overlap for ASR context, matching the server's replay overlap.
  - If that chunk has already left the ring, resume from the oldest buffered chunk; the hole becomes a gap.
  - The preview shows "Reconnecting…" instead of the current permanent error.
- **Saving batches:** append every `words` event **byte for byte as received** (the signature covers the batch's exact bytes, so never re-serialize it) to `<meeting>.live.jsonl`, one JSON line per event. Flush after each line and `fsync` at most every 5 s. After a crash, a torn last line is ignored on read.
- **Track coverage:** keep each channel's highest `to`, for `resume.covered_to`.
- **Meeting end:**
  - Wait up to 20 s (currently 10 s) for `live_complete`.
  - If the meeting ends while disconnected, make one final 10 s connect attempt to flush. Otherwise stop; the tail is a gap.
- **Cleanup:** delete `.live.jsonl` together with the `.mic.ogg` and `.loopback.ogg` files wherever those are deleted today (`main.rs:1412`, `main.rs:1477`, and the post-completion cleanup).

### 1.6 New-meeting setup page (desktop)

The existing **New meeting** button (`meetings-start-rec-btn`) opens a setup page instead of starting a recording. **Nothing is recorded until the user presses Start meeting.**

- **Name:** required. **Start meeting** stays disabled until the trimmed name is non-empty. Capped at 512 characters, the server's title limit.
- **Description:** optional, capped at 2,000 characters, so the upload manifest stays well under the server's 8 KB limit. It's used as context for the summary (2.3).
- **Microphone meter:** live input level from the meeting microphone.
- **System audio meter:** live level of the system-audio capture.
  - **Test sound button:** plays a short chime (about 1 s) through the default output device using `cpal`, which the desktop already uses for capture, so no new dependency. The system meter should jump.
  - **If it doesn't move:** the page says system audio isn't being captured, with the likely cause. On macOS that's the system-audio capture permission; elsewhere, a different output device.
  - **On speakers:** the mic meter also moves during the test. That's expected, and a one-line note says so.
- **Level preview mode** (`audio.rs`): today the levels are computed only inside the recorder (`meeting_writer`), so they exist only while recording. The preview opens the same mic and system-audio capture as a meeting, but only computes levels. It writes nothing to disk, sends nothing, and holds no recording permit. It reuses the existing `meeting-levels` events and closes when the user leaves the page or presses Start meeting.
- **Start meeting:**
  1. Stop the preview.
  2. Take the server's recording permit.
  3. Create the meeting record, then start recording and the live stream.
  - The title goes to `start_meeting` directly, replacing today's `"Recording..."` placeholder, and stays editable during the meeting. The description is saved in the local meeting record (`LocalMeetingRecord.description`).
  - If the permit or the devices fail (for example, another device is already recording), the user stays on the setup page with the name and description kept, and sees the error.

## Part 2: upload (desktop → main server)

### 2.1 Desktop (`meeting_upload.rs`)

- If `<meeting>.live.jsonl` exists and is non-empty, add `("transcript", path.with_extension("live.jsonl"))` to the files map. Its size and SHA-256 go in the manifest like the audio files.
- Everything else is unchanged: resumable 4 MB `PUT`s, commit, status polling, 404 → re-create.

### 2.2 Server (`meeting_jobs.go`)

- **`create` / `upload`:** accept an optional `transcript` channel, capped at 16 MB (a 4-hour meeting is about 3–5 MB).
- **`commit`:** verify the transcript's size and SHA-256 like the audio, but skip the Ogg duration check for it. Duration still comes from the audio channels only.
- **Quota and meeting row:** the quota charge and empty meeting row at commit are unchanged.

### 2.3 Meeting description (desktop and server)

- **Desktop:** `MeetingUpload` gains an optional `description`, taken from the local meeting record.
- **Server:**
  - **Validation:** in `create`, the description must be valid UTF-8 and at most 2,000 characters.
  - **Storage:** it's stored on the job, then copied to the meeting row at commit as a new `meetings.description` column (additive migration), encrypted per account like the other meeting fields.
  - **Summary context:** the summary prompts (3.8) receive it as a separate, clearly labelled context field: "The organiser described this meeting as: …". It's treated as background only, never as instructions. It isn't sent to the enhancement step, which must not add content.
  - **Display:** the meeting API returns it, so the desktop can show it under the title.

## Part 3: end-of-meeting pipeline (main server, in memory)

A new `meetingPipeline` replaces the stage logic in `process`/`stage`/`finish`. It is a pure function of the spooled files. It runs under the existing job lease and 6-hour bound, and is safe to re-run from scratch.

### 3.1 Load and verify

1. **Durations:** take each channel's duration from the Ogg pages, as commit does.
2. **Verify batches:** parse `transcript` line by line. Skip any line that:
   - doesn't parse;
   - fails the HMAC (under either key);
   - has a `job_id` that doesn't match the job;
   - has an unknown channel.
3. **Order batches:** per channel, sort the valid batches by `from`.
4. **Ownership:** each point on the timeline belongs to the first batch that covers it. A word is kept only if its midpoint lies in a span its own batch owns. This removes duplicates from reconnect overlap.
5. **Coverage:** the coverage of each channel is the union of the batches' `[from, to]` ranges.

### 3.2 Gaps

- **Definition:** gaps are `[0, duration]` minus coverage, per channel.
- **Tidying:** merge gaps less than 1 s apart, and ignore gaps shorter than 0.3 s.
- **Whole-channel fallback:** if gaps exceed 60% of a channel, transcribe the whole channel. The cost is the same and it's simpler.
- **Logging:** log the gap seconds per channel, with no text.

### 3.3 Gap ASR (only when gaps exist)

- **Request:** acquire a fleet meeting lease, then `POST /v1/meeting/{mic|system}?language=…&ranges=12.40-75.10,…` through the controller. The query, including `ranges`, is already sealed by the transport.
- **Splicing:** keep returned words whose midpoint falls inside a gap, and merge them into the channel's word list in time order.
- **Release:** release the lease as soon as the gap ASR finishes. There's no lease at all for meetings without gaps.

### 3.4 Diarization (starts at t = 0, in parallel)

- **Request:** seal the `system` audio to the Modal key and submit it, then poll, as today. This all happens in memory.
- **Concurrency:** a global in-memory semaphore of 8 jobs, today's `limit`.
- **Removed:**
  - the durable FIFO, its SQL lease, persisting the remote ID, and `diar_result`/`diar_response_key`;
  - the `tick`/`advance` coordinator in `meeting_diarization.go`. The HTTP request code, envelope and result normalization are kept.
- **After a restart:** the job re-runs and submits a new Modal job, under a fresh random job ID. A duplicate costs well under a cent and its result is just ignored. Each finished remote job is deleted after its result is read.
- **Failure:** retry twice with backoff. If it still fails, the attempt fails and the job goes back into the normal job retry.

### 3.5 Vocabulary → Jev (both channels, starts once words are final)

1. **Sections:** split each channel's words at pauses of 1.5 s or more, or at 250 words, never mid-word.
2. **Raw text:** per section, `raw = join(words, " ")`, recording each word's byte range.
3. **Matching:** run `vocabulary.Propose` on the main server, with the user's term snapshot and a 1 s timeout per section. The `MatchFinalVocabulary` worker call is skipped, so no GPU worker is needed.
4. **Decisions:** call `vocabularydecision.Default().Evaluate(ctx, proposal, "main_meeting")`. Sections run in parallel, bounded by the Jev client's own slots and RPS/TPS limiter.
5. **Mapping back:** map each accepted choice's byte span to the words it covers and replace them with one resolved token, taking the first word's start and the last word's end. Unaccepted choices leave the words untouched.
6. **Audits:** keep every section's `Audit`. The final `vocabulary_audit` is the list of section audits.

### 3.6 Turns and speakers

- **Mic:** consecutive words with gaps under 1 s form a turn, labelled with the user's name. This is the current `mergeMeeting` rule.
- **System:** once diarization is done, give each word the speaker it overlaps most, then form turns, also as in the current `mergeMeeting`. Words with no overlapping speaker get "Unknown speaker".

### 3.7 LLM enhancement (chunked, parallel, validated)

- **Chunks:** whole turns, about 150–300 words each. A turn longer than 300 words is split at the nearest pause or sentence end. Each target turn gets an ID.
- **Context:** the two turns before and the two after are sent, marked read-only ("do not edit, do not return").
- **Protection:** resolved vocabulary terms are replaced with placeholders before sending and restored afterwards, using the existing `audit.Protect` pattern applied per chunk.
- **Request:** the existing `pipeline.RunDeepSeekChatCompletion` (the configured LLM provider) with a strict JSON response: `{"turns":[{"id":"t12","text":"…"}]}`.
- **Validation:** a response is accepted only if:
  - it is valid JSON;
  - it returns exactly the target IDs, each once, with non-empty text;
  - every placeholder appears exactly once;
  - every number in the input survives;
  - it keeps at least 70% of the original words, by multiset overlap after lowercasing and stripping punctuation, which catches invented or dropped content.
- **Failure:** retry up to twice. After that, keep the vocabulary-resolved text for those turns. A chunk is never dropped and never blocks the meeting.
- **Concurrency:** 8 chunks per meeting and 24 globally (env-configurable).
- **Timing:** mic chunks start as soon as 3.5 finishes; system chunks start after 3.6.
- **Timestamps:** turn start/end times are untouched. `segments_json` is turn-level.

### 3.8 Merge and summary

1. **Merge:** sort all turns by start time and assign turn IDs, as today.
2. **Summary input:** the flattened transcript, as `Name: text` lines.
   - Up to 12,000 words: one call returns the summary and action items.
   - Over 12,000 words: 30-minute sections are summarized in parallel, then one combining call produces the final summary and action items, with owners.
3. **Output:** the existing `summary` and `action_items` fields and format, plus `RestoreMentions`. Retry twice. If it still fails, the meeting completes without a summary, which is today's behavior when analysis fails.

### 3.9 The one final write

One transaction:
- `UPDATE meetings SET transcript, segments_json, summary, action_items, vocabulary_audit` (encrypted per account via `Privacy.MeetingFields`, as today);
- `UPDATE meeting_jobs SET status='completed', admitted=0`.

The spool is then archived and purged by the existing maintenance, unchanged.

### 3.10 Status the desktop sees

`queued` → `processing` → `analyzing` (enhancement and summary) → `completed` or `failed`. The same values as today, so the client doesn't change.

### 3.11 Removed from the server

- `mic_result`, `system_result`, `diar_result` and `diar_response_key` are no longer written, and `sealStage`/`openStage` go away. The columns stay in the schema, with no destructive migration.
- The "waiting for diarization" requeue path, and the fleet lease renewal loop except during gap ASR.
- The whole-transcript `polished` rewrite in `generateMeetingAIAnalysis`. It is replaced by 3.7, and the summary prompt moves to 3.8.

### 3.12 Restarts, retries and in-flight jobs at deploy

- **Automatic attempts:** a meeting fails after 4 attempts (was 12), since every attempt re-runs the LLM steps. Permanent errors (an unreadable recording, a rejected upload) fail at once. Waiting for a gap-ASR worker happens inside the attempt, for up to 30 minutes.

- **After a restart:** jobs in `processing`/`analyzing` with an expired lease go back to `queued` and re-run from the spool.
- **At deploy:** jobs queued under the old format have no `transcript` file, so they run with whole-channel gaps, which is equivalent to today. Nothing needs migrating.

## Part 4: worker gateway (`deployment/spot-worker/gateway/meeting.go`)

- **Request:** `/v1/meeting/{mic|system}` accepts an optional `ranges` query parameter: up to 512 comma-separated `start-end` pairs in seconds, validated to be sorted, non-overlapping and within the duration.
- **Behavior:** with `ranges`, it still decodes the whole file in memory, but runs the 30-second ASR windows only where they intersect a range padded by 4.48 s of context. Only words whose midpoint is inside a range are returned. Without `ranges`, it behaves as today.
- **Deploy:** rebuild the gateway for both architectures, publish it to S3 with a new manifest, point `fleet.json` at it, and replace the workers. This is the same procedure as the Oct 25 encryption rollout.

## Part 5: compatibility

| Client | Server | Result |
|---|---|---|
| old (v1, no transcript file) | new | whole-channel gaps → full ASR; same cost as today, new pipeline otherwise |
| new | new | live transcript used; gap ASR only for gaps |
| new | old | server ignores `protocol`, sends no `words`; client uploads no transcript file → today's behavior |
| new, live failed to start | new | whole-channel gaps → full ASR |

## Part 6: tests

**Server:**
- **Signatures:** valid, tampered words, tampered coverage, wrong job, wrong user, previous key accepted, unknown key rejected.
- **Coverage and ownership:** reconnect overlap creates no duplicates, silence batches count as coverage, and a torn last line is ignored.
- **Gaps:** an outage in the middle, a missing tail, live never started, tiny-gap tidying, and the 60% whole-channel rule.
- **No-gap meetings:** zero fleet calls and zero leases (asserted with a fake fleet).
- **Gap splicing:** no lost or duplicated words at gap edges.
- **Vocabulary span mapping:** multi-word replacements keep first-start/last-end, and unaccepted choices change nothing.
- **Enhancement validation:** malformed JSON, missing or extra IDs, a missing placeholder, a changed number, low overlap → retry → fallback to the original text.
- **Ordering:** mic enhancement starts before diarization finishes, and system enhancement waits for it (fake clocks and channels).
- **Long summaries:** meetings over 12,000 words use sections plus a combining call.
- **Restart:** a mid-pipeline restart re-runs from the spool, and the result equals a clean run.
- **Live relay:** a v2 reconnect replaces the stale session, and an out-of-order chunk is refused.
- **Description:** stored encrypted, length-limited, and passed to the summary prompts but not to the enhancement step.
- **Speaker review:** `reviewed: true` sets the flag; saving names sets it too; it's returned with the meeting.

**Gateway:**
- The `ranges` parameter transcribes only the requested windows, against the real binary through the real controller (extending `TestRealGatewayEncryptedBatchAndStream`).

**Desktop:**
- **Chunks:** seq numbering, and the ring buffer's resume point calculation, including when the point has fallen out of the ring.
- **Transcript file:** JSONL append, plus the torn-line tolerance on read.
- **Upload and cleanup:** the manifest includes the transcript file when present, and cleanup removes it.
- **Setup page:**
  - Start meeting is disabled until a name is entered.
  - No recording file exists before Start meeting.
  - The level preview closes on Start and when leaving the page.
  - A failed start keeps the name and description.
- **Speaker prompt:**
  - Clip choice prefers the longest non-overlapping turn and cycles to the next.
  - The microphone speaker is never asked about.
  - Skip and Skip all set the review flag, and a reviewed meeting never prompts again.

**End to end, on production after deploy:**
1. A clean 10-minute meeting with no gaps: zero transcription GPU calls.
2. The same meeting with the network cut for 30 s (no gap, thanks to the buffer) and for 3 min (one gap re-transcribed).
3. A meeting recorded with an old client build: full transcription.

## Part 7: rollout

1. **Server:** live v2, `words` events, transcript upload, the new pipeline and the new key. It accepts every client version, so it ships first.
2. **Gateway:** `ranges` support, then a manifest and worker swap. Until this lands, the server sends gap ASR without `ranges` and splices only the gap words: correct, just not cheaper for meetings with gaps.
3. **Desktop:** protocol v2, reconnect, the transcript file and upload, the setup page, the description, and the speaker prompt. Users get the saving as they update. The server's description and speaker-review fields ship with step 1 or before this step; older desktops simply never send or read them.

Each step deploys on its own and can be rolled back on its own (the server rollback copy follows the existing `~/speektome/rollback-*` convention).

## Part 8: observability (no transcript text in any log)

Per meeting:
- coverage and gap seconds per channel, and gap ASR seconds;
- Jev calls, timeouts and fallbacks;
- LLM chunks, retries and fallbacks;
- diarization wait and processing time;
- total time from commit to completion.

## Part 9: naming speakers on first view (desktop, small server addition)

The first time a completed meeting is opened, the desktop asks the user to name the other speakers. It uses the speaker list and segments the meeting already has, and saves through the existing `PUT /meetings/{id}/speakers`. That endpoint already replaces the default labels in the transcript, summary and action items.

**When to ask:**
- The meeting is completed and has at least one diarized speaker (`spk_N`).
- `speakers_reviewed` is false.
- The microphone speaker is never asked about: it's the user.
- Meetings without speakers (diarization unavailable) are never prompted.

**What the prompt shows**, one speaker at a time (one card per speaker; a single remote speaker gets just one card):
- **The clip:** the speaker's longest turn that overlaps neither the microphone nor another speaker, played for 3–8 seconds from its start. If no turn is free of overlap, the longest turn is used.
- **Play another clip:** steps to the speaker's next-longest qualifying turn.
- **The clip's text,** shown next to the play button, since people often recognise a speaker by what was said.
- **Name field.**
- **Skip this speaker:** they keep their default label ("Speaker 2").
- **Skip all:** closes the prompt.

**Audio source:**
- Clips play from the local `mixed` recording, through the meeting view's existing local audio player (`get_local_meeting_audio`). That's the same file the archive keeps.
- On a device without the local recording, the prompt shows the text samples only.

**Ask only once:**
- Naming, skipping or "Skip all" sets `speakers_reviewed`, so no device asks again.
- Speakers stay renamable at any time in the existing speaker editor.

**Server addition:**
- **New column:** `meetings.speakers_reviewed INTEGER DEFAULT 0` (additive), returned with the meeting.
- **New field on the existing endpoint:** `PUT /meetings/{id}/speakers` accepts an optional `"reviewed": true`, and any save through it sets the flag. A skip-only review sends unchanged names with `"reviewed": true`.

## Defaults to confirm

| Setting | Default | Note |
|---|---|---|
| Client resend buffer | 2 min per channel | ~360 KB each |
| Reconnect overlap | 8 chunks (4.48 s) | matches server replay overlap |
| Vocabulary section | pause ≥ 1.5 s or 250 words | |
| LLM chunk | 150–300 words, ±2 turns context | |
| LLM word-overlap floor | 70% | below → retry, then original text |
| LLM concurrency | 8 per meeting, 24 global | bounded by the provider's rate limits |
| Sectioned summary | over 12,000 words, 30-min sections | |
| Diarization failure | job retries, then `failed` | alternative: complete with "Unknown speaker" |
