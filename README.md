# Nexss OPS

**Nexss OPS** is a Go toolkit for inspecting Go application actions and NFlow programs, then generating backend routing and TypeScript integration pieces. It builds on [Nexss Flow](https://github.com/nexssp/flow), Kernel, and Transport.

## Requirements

- Go 1.27.3 or later
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

# Generate a Flow/Kernel HTTP backend from explicitly routed actions
nexssp backend --scan . --out app/backend_server_gen.go --package app

# Generate TypeScript and Zod SDK files (default output: ui/src/lib)
nexssp sdk --scan .

# Scaffold the SvelteKit UI or the Studio route
nexssp ui:init
nexssp ui:studio

# Open the standalone local Studio for a Flow program
nexssp studio ./app.nflow
```

`nexssp backend` builds its route table from action contracts: it mounts only Go actions and NFlow pipelines that explicitly declare HTTP routes. Use `--exclude-action <name>` or `--exclude-pipeline <name>` to leave a route out. OPS does not invent routes or action implementations; application code supplies the registered actions and their Flow bundles to the generated server.

`nexssp studio [file.nflow]` is a separate local Svelte editor for a Flow entrypoint and its local `@include` modules. Studio uses Flow's compiler source index for the file tree, delegates Validate and Run to `nexssp flow`, and uses Flow's native explain plan output. The server binds only to loopback; running a flow executes it on the local machine with the current user's permissions. `nexssp ui:studio` remains the command that scaffolds the existing app-embedded Studio route.

For a hands-on example, see the [incident-triage Studio walkthrough](examples/studio-incident-triage/README.md), which uses an entrypoint, two included `.nflow` modules, and a sample JSON payload.

The SDK generator inspects the selected source tree and preserves user-edited extension files by default. Use `nexssp sdk --force` only when you intend to overwrite those generated outputs. The UI scaffold derives its display/package name from the current project directory and does not require a named system's API contract. Generated transport clients do not assume authentication or call token-refresh endpoints; add auth only in the application, with caller-supplied headers or a custom fetcher.

When an NFlow HTTP pipeline binds `:request-schema` or `:response-schema`, the public SDK types and endpoint signatures come from those NFlow `@schema` declarations rather than conflicting Go-AST DTOs. Bodyless routes stay argument-free, and unsupported untyped fields such as `any` are reported instead of widened into public contracts. Routes without explicit NFlow boundary schemas continue to use their analyzed action contracts.

**Existing UI scaffolds:** `nexssp ui:init` now emits a generic SvelteKit shell and no longer generates the legacy login/auth/client/router templates. Existing files are preserved unless `--force` is used; migrate or remove obsolete files yourself, and back up local edits before forcing replacements.

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

To rebuild the standalone Flow Studio frontend before compiling the Go CLI:

```sh
cd flowstudio/web
npm ci
npm run build
```

Common checks:

```sh
task build
task test
task pre-push
task ci
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contributor workflow and [SECURITY.md](SECURITY.md) for vulnerability reporting.
