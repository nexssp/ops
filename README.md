# Nexss OPS

**Nexss OPS** is a Go toolkit for inspecting Go application code and generating the backend and TypeScript integration pieces used by Nexss applications. It builds on [Nexss Flow](https://github.com/nexssp/flow), Kernel, and Transport.

## Requirements

- Go 1.26.9 or later
- [Task](https://taskfile.dev/) for the repository workflows

## Install the command-line tools

```sh
go install github.com/nexssp/ops/cmd/nexssp@latest
go install github.com/nexssp/ops/cmd/nexss@latest
```

Ensure `$(go env GOBIN)` (or `$(go env GOPATH)/bin` when `GOBIN` is unset) is on your `PATH`.

- `nexssp` provides OPS analysis, backend and SDK generation, and UI scaffolding.
- `nexss` provides the Nexss Flow command-line runtime.

To build from a checkout instead:

```sh
go build ./...
go run ./cmd/nexssp help
```

## `nexssp` commands

Run `nexssp help` for the full syntax and options. The main commands are:

```sh
# Inspect Go sources and report actions, routes, structs, and diagnostics
nexssp analyze --scan ./path/to/go/module
nexssp actions --scan ./path/to/go/module

# Generate a Flow/Kernel HTTP backend from the current project
nexssp backend --scan . --out app/backend_server_gen.go --package app

# Generate TypeScript and Zod SDK files (default output: ui/src/lib)
nexssp sdk --scan .

# Scaffold the SvelteKit UI or the Studio route
nexssp ui:init
nexssp ui:studio
```

The SDK generator preserves user-edited extension files by default. Use `nexssp sdk --force` only when you intend to overwrite those generated outputs. The UI scaffold currently uses the built-in `jumalu` service defaults.

## Run a Flow program

`nexss` and `nexssp flow` both run `.nflow` programs through Flow's native bundles:

```sh
nexss run path/to/program.nflow
# or
nexssp flow path/to/program.nflow
```

## Development

Install the repository tools and inspect available tasks:

```sh
go install github.com/go-task/task/v3/cmd/task@latest
task setup
task
```

Common checks:

```sh
task build
task test
task pre-push
task ci
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contributor workflow and [SECURITY.md](SECURITY.md) for vulnerability reporting.
