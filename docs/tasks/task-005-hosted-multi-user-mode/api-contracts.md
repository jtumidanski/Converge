# Hosted Multi-User Mode — API Contracts

Companion to `prd.md` §5. Every document below is JSON:API and is produced or consumed by
the existing `internal/jsonapi` helpers:

- Requests are decoded with `jsonapi.Decode[T](r, "<type>")`, which requires
  `data.type`, requires `data.attributes`, and **rejects unknown attribute fields**. Every
  request body therefore has the shape `{"data":{"type":"…","attributes":{…}}}`.
- Single resources are written with `jsonapi.Write`, collections with `jsonapi.WriteList`,
  errors with `jsonapi.WriteError(w, status, code, title, detail)`.
- Requests to `/api/` must send an acceptable `Accept` header (`*/*`,
  `application/json`, or `application/vnd.api+json`), as today.

`Content-Type` on responses is `application/vnd.api+json` throughout.

---

## Resource types

| Type | Used by |
| --- | --- |
| `modes` | `GET /api/auth/mode` |
| `credentials` | login and register requests |
| `users` | current-user responses |
| `passwords` | password change request |
| `accountDeletions` | account deletion request |
| `userProviders` | provider settings CRUD |
| `providers` | existing `GET /api/providers` (unchanged) |

---

## `GET /api/auth/mode`

Unauthenticated. Available in both modes. This is the SPA's first call; it decides whether
to render a login screen at all.

**200**

```json
{
  "data": {
    "type": "modes",
    "id": "current",
    "attributes": {
      "mode": "hosted",
      "registrationOpen": true
    }
  }
}
```

`mode` is `"standalone"` or `"hosted"`. `registrationOpen` is `false` in standalone mode
and `true` in hosted mode (it exists so a future invite-code or closed-registration
feature does not need a new endpoint).

---

## `POST /api/auth/register`

Unauthenticated. Hosted only. Creates the account and logs the caller in.

**Request**

```json
{
  "data": {
    "type": "credentials",
    "attributes": {
      "username": "alice",
      "password": "correct horse battery staple"
    }
  }
}
```

**201** — sets `Set-Cookie: converge_session=…; HttpOnly; Path=/; SameSite=Lax` (plus
`Secure` per FR-3.2) and returns the new user:

```json
{
  "data": {
    "type": "users",
    "id": "01J8Z9Q2K7T3V5",
    "attributes": {
      "username": "alice",
      "createdAt": "2026-09-10T14:02:11Z"
    }
  }
}
```

**Errors** — `INVALID_USERNAME` (422), `WEAK_PASSWORD` (422), `USERNAME_TAKEN` (409),
`ACCOUNT_LOCKED` (429, per-IP registration throttle), `NOT_FOUND` (404, standalone mode).

---

## `POST /api/auth/login`

Unauthenticated. Hosted only. Same request body as register (`type: "credentials"`).

**200** — sets the session cookie; body is the `users` resource above.

**Errors**

- `INVALID_CREDENTIALS` (401) — returned identically for an unknown username and a wrong
  password, with the same detail string: `"The username or password is incorrect."`
- `ACCOUNT_LOCKED` (429) — includes a `Retry-After` header in seconds. The detail does not
  say whether the lock is on the username or the IP.
- `NOT_FOUND` (404) — standalone mode.

---

## `POST /api/auth/logout`

Requires a session. Hosted only. No request body.

**204** — deletes the login session row and sends
`Set-Cookie: converge_session=; Max-Age=0; …`.

Calling logout with a missing or already-invalid cookie also returns **204**; logout is
idempotent and never reveals session validity.

---

## `GET /api/auth/me`

Requires a session. Hosted only.

**200** — the `users` resource shown under register, with one added attribute:

```json
{
  "data": {
    "type": "users",
    "id": "01J8Z9Q2K7T3V5",
    "attributes": {
      "username": "alice",
      "createdAt": "2026-09-10T14:02:11Z",
      "providerCount": 2
    }
  }
}
```

`providerCount` lets the UI decide whether to route a new user straight to provider
settings (FR-5.9) without a second request.

**Errors** — `UNAUTHENTICATED` (401), `NOT_FOUND` (404, standalone mode).

---

## `POST /api/auth/password`

Requires a session. Hosted only.

**Request**

```json
{
  "data": {
    "type": "passwords",
    "attributes": {
      "currentPassword": "correct horse battery staple",
      "newPassword": "a much longer replacement phrase"
    }
  }
}
```

**204** — password replaced. Every other login session for this user is revoked; the
calling session's cookie stays valid.

**Errors** — `INVALID_CREDENTIALS` (401, `currentPassword` wrong), `WEAK_PASSWORD` (422),
`UNAUTHENTICATED` (401), `FORBIDDEN` (403, Origin check).

---

## `DELETE /api/auth/me`

Requires a session. Hosted only. Takes a body, because deletion is irreversible and must
be password-confirmed.

**Request**

```json
{
  "data": {
    "type": "accountDeletions",
    "attributes": {
      "password": "correct horse battery staple"
    }
  }
}
```

**204** — the user row and, by cascade, their login sessions, provider configurations, and
lockout records are removed; their review sessions and workspaces are deleted; their
mirror namespace is removed; the cookie is cleared.

**Errors** — `INVALID_CREDENTIALS` (401), `UNAUTHENTICATED` (401), `FORBIDDEN` (403).

A deletion that fails partway through filesystem cleanup still removes the database rows —
the account must not remain usable — and logs the orphaned paths at `ERROR` for operator
cleanup.

---

## `GET /api/settings/providers`

Requires a session. Hosted only. Returns the calling user's provider configurations,
sorted by slug.

**200**

```json
{
  "data": [
    {
      "type": "userProviders",
      "id": "01J8ZA4M1P8Q2R",
      "attributes": {
        "slug": "github",
        "displayName": "GitHub",
        "kind": "github",
        "baseUrl": "https://api.github.com",
        "tokenLast4": "9f2c",
        "tokenSetAt": "2026-09-10T14:05:40Z",
        "createdAt": "2026-09-10T14:05:40Z",
        "updatedAt": "2026-09-10T14:05:40Z"
      }
    }
  ]
}
```

There is no attribute anywhere in this API that carries a token value. `tokenLast4` is the
last four characters of the plaintext, captured at write time and stored alongside the
ciphertext so rendering a mask never requires a decrypt.

---

## `POST /api/settings/providers`

Requires a session. Hosted only.

**Request**

```json
{
  "data": {
    "type": "userProviders",
    "attributes": {
      "slug": "gitlab-internal",
      "displayName": "Internal GitLab",
      "kind": "gitlab",
      "baseUrl": "https://gitlab.example.com",
      "token": "glpat-xxxxxxxxxxxxxxxxxxxx",
      "validate": true
    }
  }
}
```

- `slug` matches `^[a-z0-9][a-z0-9-]{0,31}$` and is unique per user.
- `kind` is `github` or `gitlab`.
- `baseUrl` is an absolute `http`/`https` URL; a trailing slash is stripped. Optional when
  `kind` is `github`, defaulting to `https://api.github.com`.
- `token` is required and non-empty.
- `validate` defaults to `true`. When true, the server calls the provider API with the
  token before writing anything.

**201** — the `userProviders` resource, token masked as above.

**Errors** — `PROVIDER_SLUG_TAKEN` (409), `PROVIDER_UNAUTHORIZED` (422, credential
rejected and nothing written), `VALIDATION_ERROR` (422, malformed slug/kind/baseUrl or
missing token — the existing code for request-shape problems), `UNAUTHENTICATED` (401),
`FORBIDDEN` (403).

---

## `PATCH /api/settings/providers/{id}`

Requires a session. Hosted only. Partial update; `slug` is immutable.

**Request** — every attribute is optional:

```json
{
  "data": {
    "type": "userProviders",
    "id": "01J8ZA4M1P8Q2R",
    "attributes": {
      "displayName": "Internal GitLab (EU)",
      "token": "glpat-yyyyyyyyyyyyyyyyyyyy"
    }
  }
}
```

**Token semantics (FR-5.5):** an omitted `token`, or `"token": ""`, leaves the stored
token unchanged. Only a non-empty value replaces it. This is what lets the UI render an
edit form without ever holding the secret.

**200** — the updated resource.

**Errors** — `NOT_FOUND` (404, unknown ID **or** an ID belonging to another user — the two
are indistinguishable by design), `PROVIDER_UNAUTHORIZED` (422),
`VALIDATION_ERROR` (422, including an attempt to change `slug`), `UNAUTHENTICATED` (401),
`FORBIDDEN` (403).

---

## `DELETE /api/settings/providers/{id}`

Requires a session. Hosted only.

**204** — configuration removed. The user's cached provider registry is invalidated.

**Errors** — `NOT_FOUND` (404, unknown or foreign ID), `PROVIDER_IN_USE` (409, a review
session of this user in a non-terminal state references this provider),
`UNAUTHENTICATED` (401), `FORBIDDEN` (403).

Deleting a provider does not delete the user's existing review sessions; completed reviews
that referenced it remain readable.

---

## Existing endpoints under hosted mode

Shapes are unchanged. The differences are behavioural:

| Endpoint | Hosted-mode behaviour |
| --- | --- |
| `GET /api/providers` | Returns the calling user's configured providers, mapped into the existing `providers` resource. Empty array when none are configured. |
| `GET /api/providers/{provider}/repositories` and subpaths | `{provider}` resolves against the caller's own configurations; a slug belonging to another user is `NOT_FOUND` (404). |
| `POST /api/reviews` | The created session records `owner = <caller>` and builds in the caller's mirror namespace. |
| `GET /api/reviews` | Returns only the caller's sessions. Unowned sessions are never returned. |
| `GET|DELETE /api/reviews/{id}`, `/files`, `/files/{path…}`, `/diff` | A session owned by another user, or unowned, returns `NOT_FOUND` (404). |
| `GET /healthz` | Unauthenticated; additionally reports database reachability. |

---

## Error document shape

Unchanged from today — produced by `jsonapi.WriteError`:

```json
{
  "errors": [
    {
      "status": "401",
      "code": "INVALID_CREDENTIALS",
      "title": "Unauthorized",
      "detail": "The username or password is incorrect."
    }
  ]
}
```

New codes and their HTTP statuses are listed in `prd.md` §5.3. `title` continues to come
from `jsonapi.StatusTitle(status)`.

---

## Cross-cutting request rules (hosted mode)

1. **Authentication.** Every `/api/` route except `GET /api/auth/mode`,
   `POST /api/auth/register`, and `POST /api/auth/login` requires a valid
   `converge_session` cookie. Missing or invalid ⇒ `401` / `UNAUTHENTICATED`.
2. **Origin check.** Every request whose method is not `GET` or `HEAD` must present an
   `Origin` matching the request host, or `Sec-Fetch-Site: same-origin`. Failing both ⇒
   `403` / `FORBIDDEN`. This runs *before* authentication so a cross-site request never
   reaches a handler.
3. **No resource disclosure.** Any authenticated request for a resource owned by another
   user returns `404`, never `403`.
4. **Cookies from the SPA.** The frontend `fetch` wrapper sends `credentials: "include"`.
   No token is read from or written to `localStorage` or `sessionStorage`.
