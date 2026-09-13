# AGENTS.md

## Project Overview

AutoGet Organizer is the post-processing core service in an automated media pipeline. It takes downloaded files (movies, TV series, books, music, photobooks, JAV) and intelligently renames/archives them according to conventions (e.g. Jellyfin library structure, actress archive directories).

## Architecture: 4-Stage Pipeline

1. **Stage 1 Classifier** (`internal/pipeline/stage1_classifier/`) — determines the media `Category`. This is the ONLY stage allowed to do lightweight pattern matching (pure ebook/audio extensions, standard bango IDs like `^[A-Z]{3,5}-\d{3,4}$`, valid `dmm_id`). Dirty/ambiguous/mixed inputs must fall through to the LLM classifier.
2. **Stage 2 Metadata Enricher** (`internal/pipeline/stage2_enricher/`) — fills in authoritative metadata via the local metadata clients (`internal/metadata/`: TMDB, Metatube) and the local `actor.json` actress alias store (guarded by a Go file lock `flock`).
3. **Stage 3 LLM Domain Planners** (`internal/pipeline/stage3_planner/`) — per-category planners (TV/Movie/Bango LLM, Simple local planner for book/music/photobook).
4. **Stage 4 Post-Process** (`internal/pipeline/stage4_postprocess/`) — LLM semantic subtitle-to-video pairing and pure-Go physical path safety validation.

## Critical Design Rules

- **NO over-design.** Choose the simplest implementation that fully meets the current requirements. Avoid speculative abstractions, configuration, and indirection. Grow the system in layers: start from the smallest version that works end to end, then add each new capability on top of a working product. Never trade a working product for unfinished complexity.
- **NO regex/pattern matching after Stage 1.** All semantic work — episode number extraction (CJK-style markers, `1x05`, `OVA1`, `[01-02]` collections), multi-part detection (`cd1`/`partA`/volume markers), fansub noise stripping (`[HDSky]`, `1080p.WEB-DL`), subtitle language detection — is delegated to LLM reasoning. Do not "fix" edge cases with hardcoded regex; real-world filenames are too messy for pattern matching to ever be complete.
- **Code is only responsible for**: flow orchestration, external metadata tool calls, and physical I/O safety validation.
- **All LLM calls use strict Structured Outputs (JSON Schema)** via the unified `ai.Provider` interface (`GenerateStructured`), with `temperature: 0.1`. Providers: Gemini and Grok. Never parse free-form LLM text.
- **Physical safety is non-negotiable**: every output `target` must pass `filepath.Clean` and stay inside `TARGET_DIR` (block `../` traversal). Mark `.nfo`, `.url`, torrent files as `skip`.
- **API compatibility is 100% mandatory** — upstream AutoGet must not change: `POST /v1/plan`, `POST /v1/execute`, `POST /v1/replan-with-hint` (see `internal/model/api.go` and `internal/handler/`).

## Naming Conventions (emit in planner targets)

- TV: `tv_series/{Lang}/{Title (Year)}/Season {XX}/{Title (Year)} S{XX}E{YY}.{ext}` where `{Title (Year)}` is the official Simplified Chinese title, e.g. `黑客帝国 (1999)`
- Movie: `movie/{Lang}/{Title (Year)}/{Title (Year)}.{ext}` (same `{Title (Year)}` rule as TV)
- JAV: `jav/{actress}/{BANGO}.{ext}` (multi-part: `BANGO.part.1.ext`; Chinese subs: `BANGO-C.ext`)
- Subtitles: `<VideoBaseName>.<Language>.<ISO639-2>.<ext>` (e.g. `黑客帝国 (1999).简体中文.chi.srt`)

## Tooling

- Automation tasks live in `justfile` (fmt, lint, test, build). Run these before finishing work.
- **E2E tests cost real money** (`tests/e2e/` hits live LLM providers). NEVER run the full e2e suite unless the user explicitly asks for it. Always filter to the exact test case under work, e.g.:
  `E2E_TEST=1 go test -count=1 ./tests/e2e/ -run 'TestE2E_Stage1CheckersOnly/gemini/audiobook'`
  Note: without `-count=1`, `go test` may replay cached results instead of actually executing.

## Go Test

- go test should use stretchr/testify lib. Use require for setup and prerequisites; use assert for result verification.
- Any test helper functions must call t.Helper() at the beginning.
- Prefer t.Cleanup() over defer for resource teardown to ensure clean execution order.
- Be specific about errors. Use assert.ErrorIs or assert.ErrorContains where appropriate.
- Prefer manually written stubs or fakes over reflection-based mocking frameworks, eg. testify/mock.
- For infrastructure (DB, FileSystem, Cache), prefer real implementations with isolated environments over mocks whenever possible.
- When need to use temp dir, you should use t.TempDir().
- Call t.Parallel() at the start of the test and inside t.Run for independent cases.
  - easy to trigger race condition
  - fast
- Prefer table tests

```go
func TestExample_TableDriven(t *testing.T) {
    t.Parallel()

    tempDir := t.TempDir()

    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid input", "data", false},
        {"empty input", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            res, err := YourFunc(tt.input, tempDir)

            if tt.wantErr {
                require.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, "expected", res)
        })
    }
}
```
