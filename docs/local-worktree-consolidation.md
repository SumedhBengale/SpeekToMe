# Local worktree consolidation, 2026-10-19

Audited all 13 local worktrees and all four named branches against `master`
at `264fb3e`. Every named branch's committed history was already contained in
master; eight worktrees still had uncommitted material (937 path occurrences,
including repeated files and generated outputs).

## Merged additions

- Website product demos, navigation/responsive fixes, shared desktop-UI build
  integration and their small audio fixture from `9258`.
- Dashboard Command Mode usage and pricing disclosure from `d250`, merged with
  the newer demos rather than reverting them. Its backend is already in master.
- Self-contained language-parity and Inf2 research tools from `2433` and `3f43`.
  These are retained research source, not deployed inference implementations.
- Dated operations/research reports from `b9f6`, `20ad` and `907a`; the latter
  two are placed in `docs/research-archive/2026-10` to avoid presenting old
  deployment configurations as current.

## Deliberately retained in source worktrees

- Older worker gateway/bootstrap/manifests in `20ad`, `2244` and `907a` conflict
  with the newer structured vocabulary/replay/language implementation in master.
  Some also alter whether GPU diarization routes are accepted. They were not
  restored over the current production implementation. In particular, the old
  ASR-only route rejection test cannot be merged without its matching policy
  change; that policy change is not silently enabled by consolidation.
- AWS publishers/provisioners and pinned resource snapshots belong to older
  environments. They remain available for separate operational review rather
  than being adopted as a new production deployment path.
- Model/audio fixtures, generated schemas, raw benchmark logs/results, archives,
  certificates, caches and historical fleet-state snapshots remain in place.
  No worktrees, pending files or stash entries were deleted or reset.
- The original `d250` client/config/server-main differences are superseded by
  master's shared settings implementation, production router and integration
  fixes. Its remaining website additions were merged independently.

## Worktree inventory before consolidation

Counts describe dirty-path occurrences, not distinct missing features. Detached
worktrees are included because they can contain work not reachable from a branch.

| Worktree | Branch / checkpoint | Pending paths | Exact in master | Different | Absent from master |
|---|---|---:|---:|---:|---:|
| primary | master / `264fb3ef` | 0 | 0 | 0 | 0 |
| 16ab | codex/shared-desktop-settings / `264fb3ef` | 0 | 0 | 0 | 0 |
| 20ad | detached / `b5f142d8` | 232 | 9 | 9 | 214 |
| 2244 | detached / `b5f142d8` | 36 | 3 | 8 | 25 |
| 2433 | codex/worker-vocabulary-jev / `50d51ba3` | 357 | 0 | 0 | 357 |
| 3a35 | detached / `a88e0e3d` | 0 | 0 | 0 | 0 |
| 3f43 | detached / `b5f142d8` | 9 | 0 | 0 | 9 |
| 494d | detached / `a88e0e3d` | 0 | 0 | 0 | 0 |
| 907a | detached / `b5f142d8` | 192 | 9 | 9 | 174 |
| 9258 | detached / `a88e0e3d` | 22 | 0 | 10 | 12 |
| b9f6 | detached / `a88e0e3d` | 26 | 0 | 0 | 26 |
| d250 | codex/input-gestures / `50d51ba3` | 63 | 51 | 10 | 2 |
| f04d | detached / `b5f142d8` | 0 | 0 | 0 | 0 |

## Validation and release scope

The combined website production build and lint command passed (lint reports
existing warnings). Imported research Python source was parsed without running
models, AWS launchers or benchmarks. The server and desktop source remain the
previously tested integrated checkpoint; website/research recovery does not
change server request behavior. Website source consolidation does not publish
the website. Server deployment and desktop launch are verified separately.
