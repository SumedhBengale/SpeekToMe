# Meeting speakers and microphone echo

Meeting transcript, timeline, legend, live turns, speaker count and inspector use
one identity resolver. The microphone is `speaker_local` on channel `mic` and
appears as **Me**. Diarizer IDs, including `spk_0` and larger numbered speakers,
are separate system speakers. Names, colors and grouping use this same identity.

New server segments retain `speaker_id` and `channel` alongside the existing
`speaker`, `start`, `end` and `text` fields. Display names alone are not identifiers.
Existing recordings remain readable: exact local aliases or the account's exact
name identify the microphone; numbered and unknown speakers stay remote. The old
UI's assumption that every name outside Speaker 1–4 meant the user is removed.
No existing meeting transcript or audio is rewritten by this display fix.

For new recordings, the desktop runs the [Sonora 0.2.0 implementation of WebRTC
AEC3](https://docs.rs/sonora/0.2.0/sonora/) on aligned 10 ms, 16 kHz mono frames.
System audio is the playback reference and remains unchanged. The cleaned
microphone feeds the saved microphone file, mixed playback, microphone level
meter, energy metadata, and the legacy streaming sender. It is processed before
ASR, rather than relying on transcription text similarity or muting the microphone
whenever system audio is active. Ordinary dictation capture is unchanged.

The canceller state persists for the recording. Independent capture queues allow
up to 200 ms of scheduling skew before padding a missing/suspended channel;
remaining tails are aligned and processed on stop. Processing failures preserve
microphone audio and disable cancellation for that recording with a single log.
Noise suppression and automatic gain adjustment are not added to the pipeline.

Echo cancellation adapts to the acoustic path. Loudspeaker distortion, clipping,
missing reference audio or a changing device can leave residual echo. Previously
saved microphone leakage is not retroactively removed.

## Validation (2026-10-17)

- Actual 24-turn meeting: one Me identity and five remote identities agree across
  transcript, timeline, inspector and colors. Structured and legacy JSON shapes,
  Speaker 0, Speaker 10, unknown speakers, and nonmatching names were checked.
- Server tests verify identities survive persistence and microphone/system
  speakers remain separate. Full Go tests passed.
- Synthetic 50 ms echo test: 41.1 dB energy reduction after adaptation. A separate
  simultaneous local signal retained 84.7% of its energy. Quiet near-end audio,
  silence, partial final frames, and state continuity across blocks passed.
- Latest 109.9135-second paired local recording processed in 0.773 seconds with
  4,232 KB peak RSS on this desktop. Input files stayed unchanged and sample count
  was preserved. This offline run is not a live speaker/room accuracy benchmark.
- A one-hour replay of the paired recording processed 57,600,000 samples in
  23.028 seconds (22.69 seconds CPU) at 4,048 KB peak RSS. This checks sustained
  resource use and state handling, not natural hour-long acoustic accuracy.
- The Windows target was cross-checked and the Linux client was built.

For a bounded-memory offline reproduction, convert both channels to PCM16 mono
16 kHz, then run:

```
cargo run --manifest-path windows-client/Cargo.toml --release \
  --example meeting_echo_check -- mic.wav system.wav clean.wav
```

The example reads fixed-size frames; it does not load the meeting into memory.
No inference-worker or regional launch artifact changes are needed for this
client-side processing and additive server metadata.

## Saved speaker names

Open a completed meeting and expand **Rename speakers** below the date/duration.
Enter names and select **Save names**. Clearing a field restores its original
label. All speakers, including the microphone speaker, can be named. Names are
per meeting and stored on the main server, so they survive app/server restarts
and appear on other signed-in devices.

`PUT /meetings/{id}/speakers` accepts `speaker_names` and `previous_names` maps,
keyed by channel-qualified identities such as `system:spk_0` and
`mic:speaker_local`. Authentication and meeting ownership are required. Names
are trimmed, limited to 80 Unicode characters, and reject control characters.
A stale previous map returns 409 rather than overwriting another device's save.

The additive `meetings.speaker_names` column preserves canonical transcripts,
segments and generated notes. Detail/list responses include a `speakers` catalog
and `speaker_key` / `speaker_display_name` on each segment. Summary, action-item
and polished-transcript speaker references are projected from the original
labels in one pass. Renaming again, name swaps, and Speaker 1 versus Speaker 10
therefore do not cascade. Ambiguous legacy labels shared by two identities are
left alone in prose rather than attributing them to the wrong person. New
analysis prompts explicitly preserve input speaker labels for attribution.
No new ASR, LLM, or worker launch change is needed for renaming.

Validation: full Go suite, authenticated save/read after database reopen,
ownership checks, stale writes, input limits, old/new segment formats, repeated
renames, action-item attribution, Unicode word boundaries, and ambiguous legacy
labels. The DOM smoke check also covers consistent transcript/timeline/inspector
names and colors, saved editor values, failed saves retaining edits, playback
position, and HTML-looking names rendered as text:

```
npm install --prefix /tmp/speektome-ui-tests jsdom
NODE_PATH=/tmp/speektome-ui-tests/node_modules node windows-client/tests/meeting-speaker-names.cjs
```
