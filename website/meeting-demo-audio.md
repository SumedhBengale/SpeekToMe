# Meeting demo audio

Fictional, synthetic conversation created for the homepage Meetings preview.
No customer recording or microphone access is used.

Voices: Maya = en-US-AriaNeural, Alex = en-US-GuyNeural,
Sam = en-GB-RyanNeural, synthesized with edge-tts at +5% rate.
Four turns were decoded to 24 kHz mono PCM, concatenated, and encoded to
64 kbps MP3 with FFmpeg. Duration: 20.712 seconds; size: 166,317 bytes.

The exact script and turn boundaries are in
`website/src/components/meetingDemoData.json`. Playback uses these prepared
boundaries to demonstrate the speaker timeline; it does not run transcription
or speaker identification in the browser.
