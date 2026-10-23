# Speaking language

The account's `languages` list is the set of languages the user speaks. The
Windows and Linux clients keep one device-local `active_language` in config.json.
It is either a code from that list or `auto`. Settings and the tray's language
submenu change the same persisted choice immediately.

A new dictation or meeting captures that choice when it starts. Changing the
tray selection during a recording applies to the next recording. Concrete
choices constrain ASR; Automatic enables the model's existing language detection
and retains its limitations with accents and mixed-language speech.

Client requests send one `language` string:

- `/v1/stream` and `/v1/meetings/live`: initial `start` JSON. Both authenticate with an `Authorization: Bearer` header; `/v1/stream` still accepts `?token=` from older desktop builds.
- `/generate` and legacy meeting uploads: multipart field.
- `/v1/meetings/jobs`: upload manifest; the local recording manifest and server
  job retain the value across upload retries and processing restarts.

The main server validates the code, snapshots it on a copy of the request's user
profile, and maps it to a Nemotron locale such as `en-US` or `hi-IN`. Explicit
`auto` reaches Nemotron unchanged. Whisper-compatible fallback APIs omit their
language parameter for automatic detection. Multiple saved account languages do
not create a multilingual decoder prompt.

Old clients and configs default to the first saved spoken language, or English
when the list is absent. Removing the active language selects the first remaining
language. Automatic remains selected until changed explicitly. The dedicated
language command owns this state, so saving another settings form cannot undo a
more recent tray choice. Language lists remain synced with the account.

The worker gateway already forwards this single language field; no worker boot
artifact changes are required for this feature in any fleet region.

## Validation on 2026-10-17

- Server and worker-gateway Go test suites passed.
- Linux client built; Windows source and tests cross-checked for the GNU target.
- Native Linux tray tests selected Hindi, Automatic, and English with one checked
  choice, verified disk persistence, and restored Hindi after restarting the app.
  Concurrent reads during the final atomic settings writes remained valid JSON.
- Settings/onboarding DOM checks covered list changes, Automatic, and selection.
- Four authenticated, paced production ASR sessions kept account languages
  `["en", "hi"]` unchanged. Explicit English transcribed the Indian-English
  fixture in Latin script; explicit Hindi transcribed the Hindi fixture in
  Devanagari. Automatic reproduced the existing wrong-script result on the same
  English fixture; omitting the field correctly used the first saved language.
- The language column migration was verified in the active database. Durable
  meeting tests cover saved-language retention after account preferences change.

These short synthetic fixtures verify routing and the observed script issue;
they do not establish an accuracy rate across speakers or languages. Full live
meeting processing was not exercised in this validation.
