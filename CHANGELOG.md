# Changelog

All notable ArtBay Publisher changes are documented here.

## [4.0.4] - 2026-09-21

- Added a Windows installer (`ArtBayPublisher-Setup-4.0.4.exe`, Inno Setup) that creates Start Menu / Desktop shortcuts and registers an uninstaller.
- Added a branded application icon, embedded into the `.exe` via `go-winres`.
- Fixed a Mermaid diagram rendering error in the README architecture diagram.
- Fixed the `go-winres` resource config (missing language-ID level) that broke the release build.

## [4.0.2] - 2026-09-21

- Added `/cancel` to permanently cancel and delete a saved bulk queue.
- Kept the **Cancel** preview action effective after bulk publishing starts.
- Clarified that `/stop` pauses and `/resume` continues the queue.
- Fixed new ZIP uploads being blocked by an already-cancelled publication.

## [4.0.1] - 2026-09-21

- Split skipped-offer counters into category limits, duplicates, and other reasons.
- Persisted full category node IDs in the queue checkpoint.
- Avoided retrying a known-full category after `/resume`.
- Added clear skip-reason summaries to progress messages.

## [4.0.0] - 2026-09-21

- Rebuilt the local Telegram and FunPay connection dashboard.
- Added live Telegram Bot Token and FunPay `golden_key` validation.
- Added secure owner pairing through a one-time Telegram deep link.
- Prevented stored secrets from being rendered back in HTML or user-facing errors.
- Added upload rate limiting, retries, and SHA-256 image reuse.
- Added durable bulk queues with restart recovery, `/stop`, and `/resume`.
- Added large nested ZIP packages, shared `lots.json` / `artbay-batch.json`, and direct JSON imports.
- Added PNG, JPG, WEBP, and GIF support.
- Increased package limits to 200 MB compressed and 600 MB extracted with traversal protection.

## [3.6] - 2026-09-04

- Previous stable continuation build.
