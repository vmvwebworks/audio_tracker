# AGENTS.md

Instructions for AI coding agents working on this repository. Humans: see [CONTRIBUTING.md](CONTRIBUTING.md).

## Project

Windows desktop app in pure Go (no cgo): plays Guitar Pro 7/8 scores with a SoundFont synth, shows the tablature, records an instrument through ASIO, and can play along a backing track synced to the score. Read [docs/ARQUITECTURA.md](docs/ARQUITECTURA.md) before changing `internal/engine` or anything touching time or positions.

## Commands (PowerShell, from the repo root)

| Task | Command |
|---|---|
| First-time setup (SoundFont + modules) | `scripts\setup.ps1` |
| Everything CI runs (gofmt, vet, test, build) | `scripts\check.ps1` |
| Run the app in an isolated sandbox | `scripts\run.ps1 -Sandbox` (prints the PID) |
| Screenshot / click / type in a window | `scripts\ui.ps1 -ProcessId <PID> -Action shot -Out x.png` |
| Hardware smoke test (real ASIO driver) | `go run ./cmd/asiotest <file.gp> <outdir>` |

Always run `scripts\check.ps1` before saying a change is done, and report its result.

## Hard rules

1. **Audio thread** (`Engine.process` and everything it calls): no blocking, locks, I/O, logging or per-block allocation. Change its state only through `e.exec(func(*rt){…})` from the UI goroutine.
2. **Time domains**: score seconds ≠ session seconds when a backing track is loaded. Convert with `a.gpTime` / `a.sessionTime` in the UI. The engine works in session time.
3. **No cgo, Windows only.** System calls go through `golang.org/x/sys/windows`.
4. **Persistent formats** (`config.json`, `tomas.json`, `proyecto.json`, WAV/BWF): add fields, never rename or remove. Old files must keep loading.
5. **Language**: UI strings and docs in Spanish; code, comments and identifiers in English.
6. **Never commit** `assets/*.sf2`, `*.exe`, `*.wav` or personal files (see `.gitignore`).
7. **Keep `.ps1` scripts ASCII.** Windows PowerShell 5.1 misreads UTF-8 without a BOM.
8. **Never push to `main`.** Work on a branch (`feat/…`, `fix/…`, `docs/…`) and open a pull request; it is merged by squash once CI is green. `main` is not technically protected (free private repo), so this rule is on you; CI flags direct pushes.

## Verifying UI changes

- Use `scripts\run.ps1 -Sandbox`: it sets `APPDATA` to a temp folder and opens the test score, so the user's settings and takes are never touched.
- The user may have their own instance open. Only interact with, and only stop, **the PID you started**. Never kill `audio_tracker.exe` by name.
- If `audio_tracker.exe` is locked, the scripts rename it to `.exe.old`; don't delete the user's running binary.
- Don't press Play in a manual check unless the task needs it: it makes sound on the user's speakers. Prefer offline engine tests.

## Tests

- `go test ./...` runs on the fixture `internal/gp/testdata/minimal.gpif` (bars 1 2 3 2 3 4 5 at 100 bpm, drums, a tie, 3/4 bar). Extend it for parser changes.
- Engine tests call `e.process` offline (see `offline()` in `internal/engine/engine_test.go`). Add one for every change in the audio path.
- Optional real files: `GP_SAMPLE=<file.gp>`, `MP3_SAMPLE=<file.mp3>`.

## Documentation (required in the same change)

- User-visible change → `CHANGELOG.md` (*Sin publicar*) + `docs/USO.md`.
- New package, thread, persistent file or design rule → `docs/ARQUITECTURA.md`.
- New command or setup step → `README.md` + this file.
- Roadmap item done or dropped → `docs/ROADMAP.md`.

Docs describe the current state, briefly. Edit sentences that became false instead of appending. The CI fails a PR that changes Go code without touching `CHANGELOG.md`.
