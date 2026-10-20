# Repository cleanup, 20 October 2026

Performed in the primary checkout on `master`. Commit `e5ada30` checkpoints the
remaining generated desktop-schema changes before cleanup.

## Source cleanup

- Removed the unreferenced standalone `server1_gateway/` and `worker_gateway/`
  modules. Current gateway/controller sources remain in `deployment/spot-worker/`
  and `server/orchestrator/`; deployment and CI references were checked.
- Removed the duplicate root `assets/` tree and `website/assets/`, `website/logo.svg`
  and `website/favicon.svg`. Desktop assets remain in its frontend; public website
  assets remain in `website/public/`, and imported assets in `website/src/assets/`.
- Removed generated Android/iOS desktop-icon output. All Windows/macOS/Linux
  bundle icons, including Windows Store icon sizes, remain.
- Removed generated Tauri schemas from Git and ignored `src-tauri/gen/`. A build
  with that directory removed successfully regenerated the required schemas.
- Removed tracked runtime audio from the current Git tree. The standalone provider
  benchmark now requires `SPEEKTOME_BENCHMARK_AUDIO` rather than implicitly selecting
  a user's meeting. Deliberate synthetic test fixtures remain tracked.
- Made the icon helper use repository-relative paths and active asset destinations;
  it no longer writes to a developer-specific Windows path or recreates deleted
  duplicate asset trees.
- Moved the old cleanup note and obsolete inference specification into this
  historical operations directory. Added a root README with the maintained layout.

## Local artifacts and preservation

Removed Rust caches/cross-build outputs, obsolete server binaries, local language
caches and temporary website dependencies/build output. The running desktop
executable remains at its original path, with its checksum unchanged. No desktop
or server service was restarted for this cleanup.

Recordings, debug captures and selected previous binaries are preserved outside
Git at `~/.local/share/SpeekToMe/repository-cleanup-2026-10-20/`. Recording copies
were checksum-verified before removal from the source tree. Credentials and local
user databases were preserved. Existing Git history was not rewritten.

Measured checkout size (including `.git`): **33,877,785,600 bytes before** and
**210,874,368 bytes after** local artifact cleanup, approximately **31.4 GiB**
reclaimed from this checkout. Small documentation edits after measurement are not
included in those byte counts. Other worktrees and global dependency caches were
not cleaned.

## Verification

- `go test ./...` passed in `server/`, including compilation of the standalone
  benchmark with its explicit-input change.
- `cargo check --locked` passed. A second check after deleting the generated schema
  directory confirmed regeneration without tracked schema inputs.
- Clean `npm ci --ignore-scripts` and production Vite build passed in `website/`;
  retained public assets were copied into the build successfully.
- The portable icon-generation helper parses successfully; existing images were
  not regenerated or visually changed.
- Generated schemas, runtime uploads and build outputs are ignored by Git.
- The existing desktop process remained running from the unchanged executable.

Future builds recreate their caches and dependencies. Application source, current
CI/deployment modules, shared vocabulary code, useful benchmark tools/reports and
historical research documentation remain available.
