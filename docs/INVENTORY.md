# BVX organization inventory and customer attribution

BVX is installed on the organization's backend machines. End customers do not
install BVX and do not receive Brevitas keys. The organization service key is
stored only in the machine's native credential store.

AgentMap inventories organization infrastructure:

- a random, stable BVX device ID;
- a pseudonymous repository ID and its final directory name;
- an environment label;
- BVX version, operating system, and architecture;
- installation registration and last-seen heartbeat.

It never uploads source, prompts, responses, absolute paths, Git remotes,
hostnames, usernames, hardware IDs, IP addresses, provider keys, or the
organization service key.

## Customer attribution

The organization's authenticated application middleware must attach its own
opaque, stable customer identifier to every proxied model request:

```http
X-Brevitas-Customer-ID: cust_7fd12a9e
```

Do not copy an untrusted request header from an end customer. Derive this value
from the application's verified session or access token. BVX accepts ASCII
letters, digits, `_`, `-`, `.`, and `:` with a maximum of 200 bytes.

For example, with the OpenAI Python SDK:

```python
from openai import OpenAI

def ai_client(authenticated_customer_id: str) -> OpenAI:
    return OpenAI(
        base_url="http://127.0.0.1:8080/v1",
        default_headers={
            "X-Brevitas-Customer-ID": authenticated_customer_id,
        },
    )
```

The local proxy removes this internal header before a direct provider call. It
includes the ID in content-free Brevitas usage receipts and in the cache
namespace. When `upstream_auth` is `inject`, it forwards the header to the
Brevitas gateway, which derives the organization from the authenticated key
and authorizes the customer ID within that organization.

BVX never tries to infer a customer from a repository, path, prompt, or source
file.

For existing customers, `bvx onboard` reads a local CSV/JSON/JSONL database
export, detects common stable-ID fields, previews invalid rows and duplicates,
then imports exact IDs in batches of 1,000. It never uploads the export itself
or any unselected columns. Generic or ambiguous IDs require `--id-field`; names
remain local unless explicitly selected with `--name-field`. Fuzzy or semantic
matching is never used for customer identity.

## Backend contract

All calls use JSON and the BVX user agent. Authenticated calls use
`X-Brevitas-Key` with the organization service key.

### Start device authorization

`POST /v1/device-auth/start`

```json
{
  "device": {
    "id": "dev_0123456789abcdef0123456789abcdef",
    "platform": "linux",
    "arch": "amd64"
  },
  "client": {"name": "bvx", "version": "0.1.25"}
}
```

The response remains:

```json
{
  "device_code": "opaque-code",
  "verification_uri_complete": "https://brevitassystems.com/dashboard#bvx=opaque-code",
  "expires_in": 600,
  "interval": 5
}
```

### Poll device authorization

`POST /v1/device-auth/token`

```json
{
  "device_code": "opaque-code",
  "device": {
    "id": "dev_0123456789abcdef0123456789abcdef",
    "platform": "linux",
    "arch": "amd64"
  },
  "client": {"name": "bvx", "version": "0.1.25"}
}
```

Pending authorization returns `202`. Approval returns `200` with
`{"api_key":"bvt_..."}`. BVX stores that key in the native keyring only.

### Register an AgentMap installation

`POST /v1/installations`

```json
{
  "installation_id": "67e55044-10b1-526f-9247-bb680e5fe0c8",
  "device": {
    "id": "dev_0123456789abcdef0123456789abcdef",
    "platform": "linux",
    "arch": "amd64"
  },
  "repository": {
    "id": "repo_0123456789abcdef0123456789abcdef",
    "label": "finance-backend"
  },
  "environment": "production",
  "client": {"name": "bvx", "version": "0.1.25"}
}
```

The server upserts this idempotently inside the organization derived from the
key and returns:

```json
{
  "installation_id": "67e55044-10b1-526f-9247-bb680e5fe0c8",
  "heartbeat_interval_seconds": 900
}
```

During a rolling backend upgrade, BVX falls back to the legacy
`POST /v1/repositories` endpoint when `/v1/installations` returns 404 or 405.

### Installation heartbeat

`POST /v1/installations/{installation_id}/heartbeat`

```json
{
  "device": {
    "id": "dev_0123456789abcdef0123456789abcdef",
    "platform": "linux",
    "arch": "amd64"
  },
  "environment": "production",
  "client": {"name": "bvx", "version": "0.1.25"}
}
```

The server verifies that the installation belongs to the key's organization,
updates `last_seen_at`, and returns:

```json
{"status":"active","heartbeat_interval_seconds":900}
```

Registration and heartbeat are best-effort and never stop local AI traffic.
