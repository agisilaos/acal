# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

- Added JSON golden contract tests for key agent-facing commands (`setup`, `today`, `week --summary`, `month`, `quick-add --dry-run`).
- Added CI workflow to run release-check on pull requests and pushes to `main`.
- Updated release automation to create missing GitHub repositories as private by default.
- Documentation updates for implemented commands and release process.

## [v0.1.0] - 2026-02-16

- Initial public CLI baseline with setup, view (`today|week|month`), events CRUD, query, and quick-add.
