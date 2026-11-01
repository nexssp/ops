# Studio example: incident triage

This runnable multi-file `.nflow` example turns an incident record into a triage card and a short first-response checklist. It demonstrates editing Flow source, validating it, supplying JSON input, and running the flow in the standalone Svelte-based Studio. The rules are illustrative decision support—not a universal severity policy—and should be adapted to your team.

The entrypoint, [`app.nflow`](app.nflow), includes two modules: [`modules/normalize.nflow`](modules/normalize.nflow) normalizes the incident record, and [`modules/triage.nflow`](modules/triage.nflow) classifies impact and prepares response recommendations. Studio builds its file tree from Flow's compiler source index; no separate project manifest is needed.

The example is intentionally local and read-only: it does not page anyone, open a ticket, send a message, or change configuration. It needs no credentials or external services.

## Start Studio

Install the v0.2.0 CLI once (requires Go 1.27.3 or later):

```sh
go install github.com/nexssp/ops/cmd/nexssp@v0.2.0
```

From this example directory, start Studio with the installed CLI:

```sh
nexssp studio
```

If you're running from an OPS source checkout instead, use this command from the same example directory:

```sh
go run ../../cmd/nexssp studio
```

Because the entrypoint is named `app.nflow`, Studio finds it automatically. Open the loopback URL printed in the terminal. No Node.js setup is needed to use the bundled Studio UI.

## Try the workflow

1. In Studio's **Run payload (JSON object)** panel, paste the contents of [`incident.json`](incident.json).
2. Click **Validate** to check the entrypoint and included modules.
3. Click **Run flow** to see the triage card and recommendations.
4. Use the **Entrypoint map** tree to open either included module, edit it, and save it. Validate and run again to see the updated result.
5. Click **Explain** to view Flow's entrypoint map and the native plan output for the entrypoint and each included module.

With the included incident, the flow normalizes the service name to `payments-api`, classifies the report as `SEV-2` (customer-visible impact affecting at least 100 users), recommends an on-call lead and a 15-minute acknowledgement target, and suggests paging the on-call team. Those are recommendations only; the flow sends nothing.

To explore other outcomes, try changing `data_integrity_risk` to `true` or setting `affected_users` to `1200` (both classify as `SEV-1`), or set `customer_visible` to `false` with a small affected-user count to see an internal-only `SEV-4` result.

## Run without Studio

The same entrypoint can run from the command line using the pinned Flow runtime bundled with `nexssp`:

```sh
nexssp flow ./app.nflow "$(cat incident.json)" --json
```

Studio's **Run flow** action uses that same Flow CLI path. **Validate** and **Explain** remain separate actions; Explain uses Flow's native `explain --plan --json` output for each source module.

## Example policy

- `SEV-1`: potential data-integrity risk or at least 1,000 affected users.
- `SEV-2`: customer-visible impact affecting at least 100 users.
- `SEV-3`: other customer-visible impact.
- `SEV-4`: no customer impact reported.

The thresholds and acknowledgement targets are intentionally small, visible examples; replace them with your organization's approved incident policy before relying on them operationally.

> **Local execution:** Studio binds to loopback, but running a `.nflow` executes code on your machine with your user permissions. Open only Flow files you trust. Studio's editor only exposes local `.nflow` files in the selected entrypoint's project directory.
