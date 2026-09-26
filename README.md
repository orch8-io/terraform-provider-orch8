# Terraform Provider for Orch8

Manage [Orch8](https://orch8.io) — the self-hosted durable workflow engine — as code:
sequences, triggers, cron schedules, API keys, queue routing/dispatch, rollback
policies and credentials, through the engine's REST API (`/api/v1`).

```hcl
terraform {
  required_providers {
    orch8 = { source = "orch8-io/orch8" }
  }
}

provider "orch8" {} # ORCH8_URL, ORCH8_API_KEY, ORCH8_TENANT_ID

resource "orch8_sequence" "welcome" {
  name       = "welcome-email"
  version    = 1
  definition = file("${path.module}/sequence.json") # the same file `orch8 dev` runs
}

resource "orch8_trigger" "signup" {
  slug          = "user-signup"
  sequence_name = orch8_sequence.welcome.name
  version       = orch8_sequence.welcome.version
}
```

Full reference: [`docs/`](docs/) (Terraform Registry layout); examples: [`examples/`](examples/).

## Resources and data sources

| Terraform type | Engine API | Update model |
|---|---|---|
| `orch8_sequence` | `POST /sequences`, `GET/DELETE /sequences/{id}` | immutable versions → replace |
| `orch8_trigger` | `POST /triggers`, `GET/DELETE /triggers/{slug}`, `PATCH /triggers/{slug}/target` | retarget in place when `version` is pinned, else replace |
| `orch8_cron_schedule` | `POST /cron`, `GET/PUT/DELETE /cron/{id}` | in place (expr, timezone, enabled, metadata, overlap policy) |
| `orch8_api_key` | `POST/GET /api-keys`, `DELETE /api-keys/{id}` | immutable → replace; root key required |
| `orch8_queue_route` | `POST /routing-rules`, `GET/DELETE /routing-rules/{id}` | no update API → replace |
| `orch8_queue_dispatch` | `POST/GET /queues/dispatch`, `DELETE /queues/dispatch/{tenant}/{queue}` | upsert in place |
| `orch8_rollback_policy` | `POST /rollback-policies` (upsert), `GET/DELETE /rollback-policies/{name}` | upsert in place |
| `orch8_credential` | `POST /credentials`, `GET/PATCH/DELETE /credentials/{id}` | PATCH in place |
| `data.orch8_sequence` | `GET /sequences/{id}`, `GET /sequences/by-name` | — |
| `data.orch8_instance` | `GET /instances/{id}` | — |

Every resource supports `terraform import` (see `examples/resources/*/import.sh`).

Behaviour worth knowing:

* **Write-only secrets.** `orch8_credential.value`/`refresh_token`, `orch8_trigger.secret`
  and `orch8_queue_dispatch.secret` are never returned by the engine, so drift in them
  is not detected. `orch8_api_key.secret` is only available on the create that minted it.
* **Sequence definitions are not refreshed from the server.** The engine re-serializes
  blocks with defaults, which would cause spurious replacements; identity fields and
  deletion are still detected. After `terraform import`, the imported `definition`
  is the engine's serialization and will usually differ from your file, planning a
  replacement — use `lifecycle { ignore_changes = [definition] }` for the first apply
  or bump `version`.
* **Deleting a sequence** fails (409) while non-terminal instances reference it.
* 429/503 responses (e.g. SQLite `database is locked` under parallel applies) are
  retried with exponential backoff.

Not managed (yet): plugins (`/plugins`), pools, releases/canaries (use `orch8 release`/`orch8 deploy`),
outbound webhook URLs (static engine config: `ORCH8_WEBHOOK_URLS`).

## Development

```sh
go build ./...
go vet ./...
go test ./...                     # unit tests; CLI-driven ones skip without a terraform binary
TF_ACC_TERRAFORM_PATH=$(which terraform) go test ./...   # + lifecycle tests against an in-memory fake engine
go generate ./...                 # regenerate docs/ (tfplugindocs)
terraform fmt -check -recursive examples
```

### Acceptance tests (real engine, `TF_ACC=1`)

Start an engine — either the engine repo's `docker-compose.yml`
(`ORCH8_API_KEY=... ORCH8_ENCRYPTION_KEY=$(openssl rand -hex 32) docker compose up -d`)
or a single SQLite container:

```sh
docker run -d --name orch8-acc -p 8080:8080 \
  -e ORCH8_STORAGE_BACKEND=sqlite -e 'ORCH8_DATABASE_URL=sqlite:///tmp/orch8.db?mode=rwc' \
  -e ORCH8_HTTP_ADDR=0.0.0.0:8080 -e ORCH8_API_KEY=acc-root-key \
  -e ORCH8_ENCRYPTION_KEY=$(openssl rand -hex 32) ghcr.io/orch8-io/engine:latest

export ORCH8_URL=http://127.0.0.1:8080 ORCH8_API_KEY=acc-root-key ORCH8_TENANT_ID=tf-acc
TF_ACC=1 ORCH8_ACC_API_KEYS=1 go test ./internal/provider -run TestAcc -v
```

(`orch8 dev` also serves the API on :8080, but runs without auth; `orch8_api_key`
needs a root key, so prefer the container above for the full suite.)

Use a local build from Terraform with a dev override in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides { "orch8-io/orch8" = "/path/to/go/bin" }
  direct {}
}
```

## Publishing to the Terraform Registry (manual — not done yet)

1. Create a public GitHub repo **`orch8-io/terraform-provider-orch8`** (the name must be
   `terraform-provider-<name>`) and push this repository.
2. Generate a GPG signing key (RSA or DSA; the Registry does not accept ECC):
   `gpg --full-generate-key`, then `gpg --armor --export-secret-keys <FPR>` into the repo
   secret `GPG_PRIVATE_KEY` (+ `PASSPHRASE`).
3. Add a release workflow using `crazy-max/ghaction-import-gpg` and
   `goreleaser/goreleaser-action` with `args: release --clean` and
   `GPG_FINGERPRINT` in env (this repo's `.goreleaser.yml` already produces the
   zip archives, `SHA256SUMS`, its `.sig`, and `_manifest.json` the Registry needs).
4. Tag and push: `git tag v0.1.0 && git push origin v0.1.0`.
5. Sign in to <https://registry.terraform.io> with GitHub, **Publish → Provider**, choose the
   `orch8-io` org and repo, and add the ASCII-armored **public** key under
   *User Settings → Signing Keys* (`gpg --armor --export <FPR>`).
6. Subsequent tags are picked up automatically by the Registry webhook.
