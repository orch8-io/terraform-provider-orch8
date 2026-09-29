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

## Install

The provider is **not on the Terraform Registry yet**, so `terraform init` cannot download it.
Until it is, install a build from [GitHub Releases](https://github.com/orch8-io/terraform-provider-orch8/releases)
(v0.1.0 binaries are unsigned; verify them against the `SHA256SUMS` file).

### Option A: local plugin directory (keeps `terraform init` and the lock file working)

```sh
VERSION=0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')                       # darwin | linux
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')  # amd64 | arm64
gh release download "v$VERSION" -R orch8-io/terraform-provider-orch8 \
  -p "terraform-provider-orch8_${VERSION}_${OS}_${ARCH}.zip" -p "terraform-provider-orch8_${VERSION}_SHA256SUMS"
shasum -a 256 -c --ignore-missing "terraform-provider-orch8_${VERSION}_SHA256SUMS"
DEST="$HOME/.terraform.d/plugins/registry.terraform.io/orch8-io/orch8/$VERSION/${OS}_${ARCH}"
mkdir -p "$DEST" && unzip -o "terraform-provider-orch8_${VERSION}_${OS}_${ARCH}.zip" -d "$DEST"
```

(Windows: `%APPDATA%\terraform.d\plugins\registry.terraform.io\orch8-io\orch8\0.1.0\windows_amd64\`.)
Terraform picks up this *implied local mirror* automatically; keep `source = "orch8-io/orch8"` and
pin `version = "0.1.0"`, then run `terraform init` as usual.

### Option B: `dev_overrides` (skips `terraform init` for this provider)

Unzip the release binary (or `go install` from a checkout) into a directory and point `~/.terraformrc` at it:

```hcl
provider_installation {
  dev_overrides {
    "orch8-io/orch8" = "/absolute/path/to/dir/containing/terraform-provider-orch8_v0.1.0"
  }
  direct {}
}
```

Terraform prints a warning about the override on every plan; that is expected.

### Once the Registry listing exists

```hcl
terraform {
  required_providers {
    orch8 = {
      source  = "orch8-io/orch8"
      version = "~> 0.1"
    }
  }
}
```

Remove the local plugin directory / `dev_overrides` then, and `terraform init -upgrade`.

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
| `data.orch8_executor_join_token` | none (computed locally) | — |

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

## Hybrid executors

Run Orch8 executors in your own Kubernetes cluster or AWS account while the
control plane stays managed. An executor needs one secret, an **executor join
token** (`o8x1...`, passed as `ORCH8_JOIN_TOKEN`), which carries the control
endpoint, a dedicated API key, tenant, runtime id, placement labels and region.
Executors only dial out; nothing needs to reach them.

* **Orch8 Cloud** issues join tokens; pass them to a module as a sensitive variable.
* **Self-managed keys:** mint the key with `orch8_api_key` and compose the token with
  `data.orch8_executor_join_token`, which validates it with the engine's rules
  (https endpoint, UUID runtime id, label format). The token contains the API key,
  so it lands in state like `orch8_api_key.secret` does: use an encrypted backend.

Two modules deploy `ghcr.io/orch8-io/engine` with `ORCH8_JOIN_TOKEN`; every replica
joins as `<worker_id_prefix>-<hostname>`:

| Module | Deploys | Token storage |
|---|---|---|
| [`examples/modules/hybrid-executor-k8s`](examples/modules/hybrid-executor-k8s) | Namespace (optional), ServiceAccount, Deployment (non-root uid 999, read-only rootfs, `/health/live` + `/health/ready` probes); pods roll when the token changes | Kubernetes Secret via `envFrom` |
| [`examples/modules/hybrid-executor-ecs`](examples/modules/hybrid-executor-ecs) | Fargate service (cluster optional), egress-only security group, log group, execution role scoped to the one secret; redeploys when the token changes | Secrets Manager, injected by ECS |

```hcl
resource "orch8_api_key" "executor" {
  name         = "hybrid-executor-berlin"
  capabilities = ["worker"]
}

data "orch8_executor_join_token" "berlin" {
  endpoint         = "https://control.orch8.example.com"
  api_key          = orch8_api_key.executor.secret
  worker_id_prefix = "berlin-k8s"
  labels           = { site = "berlin", gpu = "a100" } # matched by placement.labels
  region           = "eu-central-1"                    # matched by placement.region
}

module "executor" {
  source     = "github.com/orch8-io/terraform-provider-orch8//examples/modules/hybrid-executor-k8s"
  join_token = data.orch8_executor_join_token.berlin.token
  labels     = { "orch8.io/site" = "berlin" } # Kubernetes labels; placement labels live in the token
  replicas   = 3
}
```

A complete root module is in [`examples/hybrid-executor/`](examples/hybrid-executor/).
Module `labels` tag the Kubernetes/AWS objects; the labels the executor advertises
for placement are the ones inside the join token.

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

## Releases and the Terraform Registry

1. Done: the public repo **`orch8-io/terraform-provider-orch8`** exists (the name must be
   `terraform-provider-<name>`).
2. Generate a GPG signing key (RSA or DSA; the Registry does not accept ECC):
   `gpg --full-generate-key`, then `gpg --armor --export-secret-keys <FPR>` into the repo
   secret `GPG_PRIVATE_KEY` (+ `PASSPHRASE`).
3. `.github/workflows/release.yml` already runs on `v*` tags: with `GPG_PRIVATE_KEY` set it
   imports the key (`crazy-max/ghaction-import-gpg`) and runs `goreleaser release --clean`,
   producing the zips, `SHA256SUMS`, its `.sig` and `_manifest.json` the Registry needs.
   Without the secret it publishes the same files **unsigned** (`--skip=sign`) and labels the
   release as not Registry-ready. v0.1.0 was released unsigned, so tag a new version
   (e.g. `v0.1.1`) after adding the key; the Registry needs a signed release.
4. Sign in to <https://registry.terraform.io> with GitHub, **Publish → Provider**, choose the
   `orch8-io` org and repo, and add the ASCII-armored **public** key under
   *User Settings → Signing Keys* (`gpg --armor --export <FPR>`).
5. Subsequent tags are picked up automatically by the Registry webhook.
