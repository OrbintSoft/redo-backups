# Project rules for Claude (and human contributors)

These rules govern how work is carried out in this repository. They are binding for
AI-assisted changes and recommended for all contributors.

## 1. Project language

The language of the project is **English**: README, documentation, code comments,
identifiers (function/variable/type names), commit messages, and issue/PR text must all
be in English. (Conversation with the maintainer may happen in Italian, but nothing that
lands in the repository should be.)

## 2. Authorized programming languages

A language may only be used in this repository if it appears in the list below. Adding a
new language requires explicit authorization from the maintainer and an update to this
list **before** any code in that language is committed.

Authorized languages:

- **Go** — primary implementation language (the backup tool).
- **Bash** — installation/helper scripts and CI glue.

## 3. One authorized step at a time

- Never make too many changes at once. Work in small, reviewable steps, and only proceed
  with a step after it has been explicitly authorized.
- If a change would touch too many files or lines, split it into multiple steps — even if
  this leaves the tree temporarily incomplete or non-compiling between steps.
- Each step should have a clear, stated scope.

## 4. End-of-session quality gate

After all authorized steps in a session are complete, verify that:

1. the code builds — `go build ./...`;
2. the linter is clean — `go vet ./...` and `golangci-lint run`;
3. the tests pass — `go test ./...`.

Report the actual results. If something fails or was skipped, say so.

## 5. Testable, configurable, parametrizable

Write code that is testable, configurable, and parametrizable wherever reasonable:

- Side-effecting operations (running external commands, touching disks) go behind
  interfaces so logic can be unit-tested without root or real hardware.
- Behavior is driven by configuration/parameters rather than hardcoded values.

## 6. Licence and authorship

This project is licensed under the **EUPL-1.2** (see [LICENSE](LICENSE)). Author:
**Stefano Balzarotti (Orbintsoft)**, with the support of Claude agent AI. Keep copyright
and licence notices intact; new source files should carry an SPDX header
(`// SPDX-License-Identifier: EUPL-1.2`).

## 7. No personal data

Never commit personal data into the repository — in particular drive UUIDs, partition
labels, hostnames, or any content extracted from real backups. Documentation and test
fixtures must use sanitized/synthetic values only.
