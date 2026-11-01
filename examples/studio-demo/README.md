# Studio workbench demo

This runnable Go server puts one Svelte workbench over both registered Go actions and an NFlow-declared HTTP route. From the repository root, run:

```sh
go run ./examples/studio-demo
```

Open [http://127.0.0.1:8088/studio/](http://127.0.0.1:8088/studio/). Use the **ALL**, **GO**, and **NFLOW** filters to switch between operation types. The dark/gold workbench loads request fields and schemas from the real action metadata or Analyzer output; it does not create a second NFlow parser.

## NFlow route

`examples/studio-demo/app.nflow` declares `POST /api/demo/nflow-echo`, its request and response schemas, and the `demo_nflow_echo` pipeline. The pipeline calls the same registered Go action shown in the directory as `demo.echo`.

```sh
curl -sS http://127.0.0.1:8088/api/demo/nflow-echo \
  -H 'Content-Type: application/json' \
  -d '{"message":"through NFlow","repeat":2}'
```

The workbench sends NFlow requests to that same-origin HTTP endpoint and shows its actual status, response body, and elapsed time. The Studio identity selector does not override route identity; the request uses the browser's normal session and server authentication.

## Registered Go actions

The Go actions are registered in `main.go` and executed through Studio's action executor. They also expose their explicitly registered HTTP bindings.

| Go action | HTTP endpoint | Example request |
| --- | --- | --- |
| `demo.echo` | `POST /api/demo/echo` | `{"message":"hello","repeat":2}` |
| `demo.calculate` | `POST /api/demo/calculate` | `{"left":7,"right":5,"operator":"+"}` |
| `demo.validate` | `POST /api/demo/validate` | `{"title":"release notes"}`; use `"reject"` for a structured validation error |
| `demo.secure-preview` | `POST /api/demo/secure-preview` | `{"document_id":"doc-42"}`; requires the `operator` role, `demo.write` permission, and `preview-v2` feature |

Go-action runs made inside Studio include decoded input, output or structured errors, duration, trace spans, and replayable history. The Studio also exposes `GET /studio/api/actions`, `POST /studio/api/execute`, `POST /studio/api/ai/scenario`, `GET /studio/api/history`, and `GET /system/security`.

For local testing only, the claims selector can supply an identity override when both flags are enabled:

```sh
APP_ENV=development NEXSS_STUDIO_ALLOW_IDENTITY_OVERRIDE=true go run ./examples/studio-demo
```

The override is ignored unless `APP_ENV` is `dev`, `development`, or `test` **and** `NEXSS_STUDIO_ALLOW_IDENTITY_OVERRIDE=true`. Do not enable it in a production deployment.

## Refreshing the generated backend

`backend_server_gen.go` is checked in so the demo can be started with `go run`. If you edit a Go action binding or the NFlow route, regenerate the analyzed route table:

```sh
go run ./cmd/nexssp backend \
  --scan ./examples/studio-demo \
  --out ./examples/studio-demo/backend_server_gen.go \
  --package main \
  --function RunStudioDemoBackend \
  --force
```
