# Hosted mode — operator guide

Hosted mode (`CONVERGE_MODE=hosted`) adds per-user accounts, login sessions, and
per-user provider configuration on top of everything `standalone` mode already
does. It is documented in the README's *Modes* section; this file covers the
operational gaps hosted mode deliberately leaves open, and the two things an
operator must get right that the README only summarizes.

## Registration is open

Any client that can reach `POST /api/auth/register` can create an account.
There is no invite flow, no email verification, no approval step, and no
administrator role that can review or block signups. If a hosted instance is
reachable from the internet, put it behind a VPN or reverse-proxy
authentication (mutual TLS, an OAuth proxy, HTTP basic auth at the edge — any
mechanism that gates *reaching* the server at all). Converge's own login
system authenticates users to each other's data; it does not decide who is
allowed to have an account in the first place.

## Two settings that must match the deployment

- **`CONVERGE_SECURE_COOKIES=true`** only when TLS terminates at a reverse
  proxy in front of Converge (Converge itself speaks plain HTTP). If TLS is
  not in the picture at all, leave this `false` — the cookie is still marked
  `Secure` automatically whenever a request actually arrives over TLS
  (`r.TLS != nil`), so this variable exists only for the proxy-termination
  case.
- **`CONVERGE_TRUSTED_PROXY=true`** only when a reverse proxy genuinely sits
  in front of every request. This setting controls whether the per-IP login
  throttle trusts the `X-Forwarded-For` header. With no proxy in front, any
  client can set an arbitrary `X-Forwarded-For` value on a direct request and
  get a fresh throttle key on every attempt, defeating the per-IP lockout
  entirely. Turn this on only alongside a proxy that overwrites (not merely
  appends to) that header for external clients.

## The four gaps this mode leaves open

Hosted mode ships a working account system, not a full user-management
product. Four things are deliberately out of scope for now:

### 1. No password reset

A user who forgets their password loses the account: there is no mailer and
no administrator role able to reset it on their behalf. The only way back in
is to register a new account. A `converge-cli` subcommand that resets a
password hash directly against the database file is a coherent follow-up, but
it needs its own design thinking before it ships — specifically, who is
allowed to run it, and what stops it from being an authentication bypass on a
shared host (anyone with filesystem or shell access to the database would
effectively be able to take over any account).

### 2. No way to remove another user

Self-service account deletion (`DELETE /api/auth/me`) is the only removal
path. There is no administrator role and no API for removing someone else's
account, so an abandoned account persists indefinitely — including its
per-user mirrors and its encrypted provider configuration rows — until that
user deletes it themselves. The same reasoning as password reset applies to
any future admin-removal feature: it needs its own answer to "who may do
this," not a quick addition here.

### 3. Master key rotation

`CONVERGE_SECRET_KEY` encrypts every user's stored provider tokens
(AES-GCM, with the row's `user_id | id` as additional authenticated data).
No rotation command ships in this task. To rotate the key by hand:

1. Stop the Converge process.
2. For each row in `user_providers`, decrypt the stored ciphertext with the
   **old** key (AAD = `user_id | id`), then re-encrypt the resulting plaintext
   with the **new** key using the same AAD.
3. Write the new key back into every re-encrypted row.
4. Set `CONVERGE_SECRET_KEY` to the new key and start the process again.

**The key is not recoverable from the database.** If it is lost, every stored
provider token becomes permanently unreadable, and every user must re-enter
their provider token before they can use Converge again. Back the key up
somewhere durable and separate from the database file.

### 4. Per-user mirrors multiply disk use

Each user's review of a given repository uses its own mirror under
`REPOSITORY_CACHE_ROOT`, namespaced per user. N users reviewing the same
repository means N mirrors on disk, not one shared mirror. The existing
per-mirror prune still runs and reclaims space from mirrors that fall idle,
but no per-user or global quota ships with this task — a quota is a separate
feature with its own UX question (what happens to a review in progress when a
user hits their limit?) that this task does not attempt to answer.

## Unowned review sessions

Review sessions created before hosted mode was enabled — or created by
`converge-cli`, which never has a user identity — have no owner. In hosted
mode, unowned sessions are invisible to every user: no account's `GET
/api/reviews` will list them, and there is no way to claim one through the
API. On startup, the server logs how many unowned sessions exist (in the
`hosted mode ready` line, alongside the user count), so an operator can
find and remove those session directories by hand under `WORKSPACE_ROOT` if
they are no longer wanted.

## `converge-cli` stays standalone-only

`converge-cli` never opens the hosted database and never builds any
authentication machinery of its own; it always constructs its scope as
`identity.Standalone()` and resolves providers through the same static,
server-wide `PROVIDERS__*` registry standalone mode uses.

That holds regardless of the environment it inherits. It shares
`internal/app`'s wiring code with the server binary, but it pins its own mode
before handing that wiring an environment: it appends
`CONVERGE_MODE=standalone` to `os.Environ()`, and `config.Load` builds its
variables by assigning into a map as it walks the slice in order, so the last
occurrence of a key wins. `CONVERGE_MODE=hosted` inherited from the calling
shell — or from the hosted container, if the CLI is exec'd inside it — is
therefore overridden, not honoured. There is nothing an operator needs to
unset. A test asserts no database file is created when the CLI runs with
`CONVERGE_MODE=hosted` in its environment.
