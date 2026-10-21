# Changelog

All notable changes to this project are documented in this file. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-21

### Added

- Initial public release.
- `attribution`: links AI coding sessions to pull requests, with evidence, confidence, explicit abstentions, memory and SQLite stores, export adapters and AI-tool signature scanning.
- `prcomplexity` and `diffsize`: measure pull-request complexity and effective lines of code by language.
- `prworktype`: classifies pull-request type and domain through a companion Python model service.
- `defectlineage`: traces bug fixes to likely introducing changes, with evidence and optional PostgreSQL persistence.
- `aidlc`: scores AI development lifecycle maturity across nine stages and five autonomy levels.
- `airoi`: estimates recovered engineering time and ROI from comparable pull-request cohorts, withholding unsupported claims.
- `branchlineage`: records the work carried through branch promotions.
- `issuelinkage` and `issueattribution`: link pull requests to issues and measure the AI-assisted share of completed issues.
- Runnable examples, package documentation and local validation scripts.

[Unreleased]: https://github.com/tetriz-ai/tetriz-engineering-intelligence/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tetriz-ai/tetriz-engineering-intelligence/releases/tag/v0.1.0
