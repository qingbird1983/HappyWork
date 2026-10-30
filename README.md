# HappyWork

**English** · [简体中文](README.zh-CN.md)

**A local-first, permission-controlled desktop AI agent: least privilege, reversibility first, human approval for anything irreversible.**

Changelog: [CHANGELOG.md](CHANGELOG.md) · License: [LICENSE](LICENSE)

## Highlights

- **Controlled, not omniscient**: permission decisions come from a single tool-mapping table; "can this be undone?" outranks "should this be allowed?" when grading risk.
- **Argument-consistency pipeline**: what the approver sees is what gets executed — canonicalization plus an execution-lock hash blocks hook tampering, path escapes and post-approval drift.
- **Local-first**: transcripts in local SQLite, credentials only in the OS keychain, no cloud dependency, no silent auto-update.
- Docs-first engineering — design docs, ADRs, standards and a phased roadmap are committed before any code (see [`docs/`](docs/README.md)).

## Status

Pre-code: the v3 architecture baseline is on disk (under review); Phase 1 (minimal skeleton) has not started.

- **Roadmap & progress board**: [docs/roadmap/00-roadmap.md](docs/roadmap/00-roadmap.md)

## Documentation

Project documentation is maintained in **Chinese** under [`docs/`](docs/README.md) (this README pair is the bilingual entry point):

| Directory | Contents |
|---|---|
| [`docs/research/`](docs/research/) | Research notes: facts and data behind decisions |
| [`docs/adr/`](docs/adr/) | Decision records — why things are the way they are |
| [`docs/architecture/`](docs/architecture/) | System design and key constraints |
| [`docs/roadmap/`](docs/roadmap/) | Phases, scope, acceptance scenarios; `00-roadmap.md` hosts the board |
| [`docs/standards/`](docs/standards/) | 9 repo standards: how work gets done |
| [`docs/design/`](docs/design/) | Per-phase detailed designs and progress reports |
| [`docs/glossary.md`](docs/glossary.md) | Glossary (for non-technical readers) |

## Pending decisions (matters requiring the project owner)

> Owner decisions are collected in this table (mirrored in both READMEs) and flagged in CHANGELOG entries; once decided, write an ADR and remove the row.

None pending (frontend framework decided: React, see [ADR-0007](docs/adr/ADR-0007-frontend-react.md)); new items will be registered here.

## License

MIT © 2026 The HappyWork Authors (see [LICENSE](LICENSE); attribution change noted in [ADR-0006](docs/adr/ADR-0006-license-mit.md))
