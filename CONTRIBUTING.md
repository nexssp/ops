# Contributing to Nexss OPS

Nexss OPS is part of the Nexss Ecosystem. Keep changes focused, preserve compatibility where practical, and add or update tests for behavior changes.

## Toolchain

This repository is a single Go module. Use Go 1.27.3 or later and [Task](https://taskfile.dev/) for the documented development workflows.

```sh
go install github.com/go-task/task/v3/cmd/task@latest
task setup
task
```

## Before submitting a change

```sh
task tidy       # keep go.mod and go.sum tidy
task fmt        # apply configured Go formatters
task pre-push   # formatting, lint, and race-enabled tests
task ci         # full local CI gate
```

Run the relevant `nexssp` command against a representative Go project when changing analysis or code generation. Treat generated output carefully: SDK files marked write-once are preserved when users edit them unless `--force` is supplied.

New dependencies should be justified and reviewed for maintenance and licensing. Do not commit credentials, generated build artifacts, or local application data.

## Optional Git hooks

Install [pre-commit](https://pre-commit.com/) and enable the configured commit and push hooks:

```sh
pre-commit install
```

Task and the repository tools must be available on `PATH` for those hooks to run.
