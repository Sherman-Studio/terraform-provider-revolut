# Terraform Provider for Revolut Merchant API

Manage [Revolut Merchant API](https://developer.revolut.com/) subscription plans
and webhooks as Terraform resources.

> **Not affiliated with Revolut.** This is an independent, community provider
> published by Sherman Studio Ltd. It is not affiliated with, endorsed by, or
> sponsored by Revolut Ltd. "Revolut" is a trademark of its respective owner.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0 (or OpenTofu >= 1.6)
- A Revolut Merchant account and an API secret key

## Using the provider

```hcl
terraform {
  required_providers {
    revolut = {
      source  = "Sherman-Studio/revolut"
      version = "~> 0.1"
    }
  }
}

provider "revolut" {
  # api_secret_key is read from the REVOLUT_API_KEY environment variable.
  # Set sandbox = true to target the Revolut sandbox host.
  sandbox = true
}
```

## Authentication

The Merchant secret key is supplied via (in precedence order):

1. The `api_secret_key` provider attribute (sensitive; never written to state).
2. The `REVOLUT_API_KEY` environment variable (recommended).

```bash
export REVOLUT_API_KEY="sk_..."
```

## Provider configuration

| Attribute        | Env fallback           | Default        | Notes |
|------------------|------------------------|----------------|-------|
| `api_secret_key` | `REVOLUT_API_KEY`      | —              | Sensitive. Required (attr or env). |
| `api_version`    | `REVOLUT_API_VERSION`  | `2024-09-01`   | `Revolut-Api-Version` header. Webhooks need >= 2024-09-01. |
| `sandbox`        | `REVOLUT_SANDBOX`      | `false`        | `true` targets `sandbox-merchant.revolut.com`. |

`REVOLUT_ENDPOINT` overrides the base URL entirely (for tests / private hosts).

## Resources & data sources

| Type | Description |
|------|-------------|
| `revolut_plan` (resource/data) | Subscription plan with inline pricing variations, phases, and subscription items. |
| `revolut_webhook` (resource/data) | A Merchant webhook (url + events), with a sensitive, rotatable signing secret. |

> **Plan variations have no standalone resource/data source.** The Revolut
> Merchant API exposes **no** standalone plan-variation endpoint (every
> `*-variations`/`subscription-plans/{id}/variations` path returns 404, verified
> against the live sandbox). Variations exist only nested inside a plan and are
> created inline with `POST /subscription-plans`. Configure them as the nested
> `variations` attribute on `revolut_plan`; read them back from
> `revolut_plan.variations[*]` or the `revolut_plan` data source.

## Important caveats

### Plans are create-and-replace only, and orphan on destroy

The Revolut Merchant API has **no plan update endpoint and no plan delete
endpoint**. Consequences in this provider:

- **Every** `revolut_plan` attribute (including the nested `variations` tree) is
  `RequiresReplace` — any change recreates the plan.
- `terraform destroy` only **removes the plan from Terraform state**. The plan
  **still exists in your Revolut account** (it is orphaned). The provider emits a
  warning when this happens. Clean up orphaned plans manually if needed.

### Webhook signing secret

`signing_secret` is server-generated, **sensitive**, and never an input. It is
returned only by create, update, single-read, and the rotate call (the list
endpoint omits it). Rotate the secret by changing the `rotate_trigger` keeper;
`expiration_period` (ISO-8601) sets the grace window for the previous secret.

A merchant may have at most **10 webhooks** — the 11th create returns a 422.

## Development

```bash
make build          # compile the provider
make test           # unit tests (no network)
make testacc        # acceptance tests (TF_ACC=1, hits the sandbox API)
make fmt            # gofmt
make lint           # golangci-lint
make docs           # regenerate docs/ via tfplugindocs
make install        # install into the local Terraform plugin mirror
```

## License

MIT — see [LICENSE](./LICENSE).
