# Deployment

IMAP live monitoring uses long-running `/api/v1/mailboxes/watch` responses.
Disable proxy buffering for that route and allow streaming responses for at
least five minutes; the app sends a heartbeat every ten seconds. `X-Accel-Buffering:
no` is set for nginx. Other requests retain their normal size/time limits. A
separate CSRF-protected presence request closes abandoned watches after 45 seconds.

## Filesystem and process

Mount a dedicated persistent filesystem at `/app/data`, owned by the container
user. For the included bind mount (`./data:/app/data`), create `data/` and build
with the host owner's IDs, e.g. `mkdir -p data && APP_UID="$(id -u)"
APP_GID="$(id -g)" docker compose up --build`. If `data/` already exists with
different ownership, change its ownership on the host first. These are image
build arguments, so rebuild when they change. Do not relax directory/file modes
to compensate: the store enforces mode 0700/0600. Do not place it on a filesystem
that ignores Unix permissions or rename/fsync semantics. Backups contain
encrypted vaults plus authentication records; protect and expire them. Account
deletion cannot remove old backup snapshots.

For the **published Docker Hub image**, the process uses UID/GID `1000:1000`.
Starting `docker run` as root does not change that container user. Prepare the
mounts from the project directory as root (or use sudo):

```sh
mkdir -p data
chown -R 1000:1000 data
chmod 700 data
```

`chmod /app/data: operation not permitted` commonly indicates incorrect bind-mount
ownership. Stop the container, correct the host ownership, then restart it.
An existing `.env` may be mounted at `/app/.env:ro` for optional configuration;
make that file readable by UID/GID `1000:1000` while retaining private permissions.
`APP_UID`/`APP_GID` are build arguments, not runtime ownership overrides for
a prebuilt image. Running the command from inside `data/` would make
`$PWD/data` select a different nested directory; use the project directory or an
absolute host path.

With no explicit `OPAQUE_SERVER_SETUP`, the first startup generates
`APP_DATA_DIR/opaque_server_setup` for an empty installation. It is created
privately (`0600`), atomically installed without replacing an existing file,
and reused after restarts or container recreation. With the recommended Docker
mount, it is `/app/data/opaque_server_setup` in the container and
`./data/opaque_server_setup` on the host. Include it in protected backups along
with the account records. A data backup now contains both the setup and records;
protect it accordingly. Neither is the user's vault decryption key.

Existing installations can retain the original explicit `OPAQUE_SERVER_SETUP`
from a secret manager, environment or `.env`. This externally managed mode does
not create a duplicate file. If a persisted file and explicit setup are both
present, their validated values must match. An invalid, empty, oversized or
symlinked setup file is never silently regenerated. When account/index data
exists but neither source is available, startup stops with restoration guidance.
Restore the original key; creating another key makes those accounts unusable.

Back up `data/index/index.key` and `data/index/users.json` together with the
user directories. Startup refuses to generate a replacement index key when
indexed accounts, user data or deletion journals remain. A missing `users.json`
with existing user data also stops startup. Restore the matching original files;
creating empty replacements does not recover account access. New installations
create a private index key without overwriting one installed concurrently.

Direct launches (`./server` and `go run ./cmd/server`) load `.env` automatically
from the working directory, with a fallback beside the executable. Environment
variables supplied by the process or Compose override file values. Relative data
paths loaded from `.env` are resolved beside that file. Quoted values, comments,
and optional `export` prefixes are supported; shell expansion is not performed.
Never regenerate the OPAQUE setup to resolve a missing-environment error: restore
the existing setup instead.

The image is non-root, drops capabilities, and uses a read-only root filesystem
under Compose. `/tmp` is an ephemeral bounded tmpfs.

## TLS and proxy

Terminate TLS at a maintained reverse proxy. Set `SESSION_COOKIE_SECURE=true` in
production. Leave `TRUSTED_PROXIES` empty unless the proxy source CIDRs are known;
never trust arbitrary forwarded headers. Restrict direct access to port 8080.

If desktop login works on localhost but mobile login fails at an HTTP LAN/IP
address, check HTTPS first. Compose deliberately forces Secure cookies; browsers
will not send them over ordinary HTTP to a remote host, even with a correct
password. The localhost development exception does not apply to a phone accessing
the host's LAN IP. Use the HTTPS hostname on both devices rather than weakening
the production cookie policy.

## Optional client-facing IMAPS and SMTPS

The base Compose deployment exposes only the web app. To enable the optional
gateways, set `MAIL_PROXY_HOSTNAME` in the existing `.env` to the DNS name or server
IP used by your clients, then run Docker Compose directly:

```sh
docker compose -f compose.yaml -f compose.proxy.yaml up -d --build
```

If `MAIL_PROXY_HOSTNAME` is empty or unspecified, the initializer skips certificate
generation successfully and the application logs **"Mail proxies disabled:
MAIL_PROXY_HOSTNAME is not specified."**. Both gateways are unavailable and
webmail continues to start normally. A malformed address or invalid existing
certificate still produces a configuration error. DNS names must resolve to this
server; when enabled, the selected address must be reachable on TCP 993/465.
Configure external DNS, router forwarding and firewall rules separately.

By default, a one-shot **proxy-certificates** service generates a local self-signed
ECDSA certificate, including a DNS or IP subject alternative name. It is kept in
the project-scoped persistent Docker volume `mail-proxy-certs`, with the private
key mode `0600`. The application mounts this volume read-only and starts only
after initialization succeeds. No host OpenSSL installation or root runtime is
needed. Existing valid key pairs are reused, so client trust survives restarts.
The initializer never replaces a key, silently changes its hostname, or rotates
an expired certificate.

**A generated local certificate must be explicitly trusted in each mail client.**
It is not issued by a public certificate authority. Inspect its SHA-256 fingerprint
with `docker compose -f compose.yaml -f compose.proxy.yaml logs proxy-certificates`.
To export its public certificate for client import:

```sh
docker compose -f compose.yaml -f compose.proxy.yaml cp \
  mail-aggregator:/app/mail-proxy-certs/fullchain.pem ./mail-proxy-public.pem
```

Replace `docker compose` with `docker-compose` on legacy installations. Protect
and back up the certificate volume; deleting it loses the existing client trust.
The fingerprint is public; never distribute the `privkey.pem` file.

For an existing CA-issued certificate, configure a directory containing
`fullchain.pem` and `privkey.pem`:

```dotenv
MAIL_PROXY_CERTS_DIR=./certs
```

Run the same Compose command. This directory is bind-mounted. Both files must be readable by
the configured container UID/GID; initialization validates them without overwriting
or changing their permissions. Certificate symlink targets must be inside the
mount. If generating a local certificate in a bind mount instead, the directory
must also be writable by that UID/GID. Keep existing `APP_UID`/`APP_GID` consistent
with the owners of both persistent data and certificates.

Subsequent starts can use the two Compose files directly:

```sh
docker compose -f compose.yaml -f compose.proxy.yaml up -d --build
```

The override publishes **993 → 1993** for IMAPS and **465 → 1465** for SMTPS.
Clients use the standard ports; the process binds unprivileged ports inside the
container, retaining the non-root user and dropped capabilities. The gateways
perform TLS directly, separately from the HTTP reverse proxy. No cleartext IMAP,
SMTP or public port-25 relay is exposed. TLS 1.2 or newer is required, and startup
fails if the certificate/key cannot be loaded, is expired/not yet valid, does not
match the hostname, or an enabled listener cannot bind.

The override enables both protocols by default. Set `IMAP_PROXY_ENABLED=false`
or `SMTP_PROXY_ENABLED=false` in `.env` to disable one independently; its published
port then has no listening service. The base Compose file does not enable either
proxy. Allow inbound TCP 993/465 only where required. These listeners use the TCP
peer address for the failed-authentication cooldown, so a TCP load balancer or NAT
can make several clients share one IP; the HTTP `TRUSTED_PROXIES` setting and
forwarded headers do not affect them.

In **Settings → Mail client access**, each user selects one receiving mailbox
and one or all of their SMTP sending addresses. They generate or enter a separate
proxy password (at least 16 characters) and use their usual application login plus
that password in the external client. The main OPAQUE login password is not a
proxy login. For SMTP, the envelope sender selects the configured upstream, and
From/Sender headers must match it. Addresses from another application account,
unknown addresses, ambiguous duplicate sender mappings, and a null envelope
sender are refused. The gateway is an authenticated submission service.

The proxy credential copy is stored in `users/<uuid>/proxy.json`, encrypted with
Argon2id (64 MiB, three iterations) and AES-256-GCM. It is bound to the user UUID
and the source encrypted-collection revisions. It contains only the selected
provider credentials, not the vault key. It remains usable while the browser is
closed or locked and after process restart. Changing either provider collection
closes existing proxy sessions; the affected protocol must be saved again in
Settings before it can reconnect. Disabling access or changing its proxy password
closes both services' sessions; deleting the user removes its record and closes
its active sessions. Web lock/logout do not revoke this independently enabled
access. Disabling access does not erase copies in older backups.

Failed authentication imposes a 30-second IP cooldown shared by the two gateways.
Successful connections do not consume that cooldown, allowing IMAP and SMTP to
connect together. There are bounded concurrent connections and password-KDF
workers. Connections have the configured absolute session lifetime (12h by
default), then clients reconnect. SMTP limits messages to 20 MiB and 100 recipients;
messages are processed in memory, never queued or automatically retried. The
external client handles Sent-folder copies through IMAP; the gateway does not
also append a second copy. Webmail HTML filtering/privacy settings do not apply
to the external client's reader or outgoing MIME content.

Restart the service after certificate renewal; certificates are loaded at
startup. For native runs or `docker run`, set `IMAP_PROXY_ENABLED=true` and/or
`SMTP_PROXY_ENABLED=true`, plus `MAIL_PROXY_HOSTNAME`. Leaving both TLS paths empty
uses `APP_DATA_DIR/proxy-certs/fullchain.pem` and `privkey.pem`: the application
creates the local key pair at startup if neither file exists, without a helper
container. Persisting `/app/data` therefore also persists this TLS identity. If
using existing files, supply both `MAIL_PROXY_TLS_CERT` and `MAIL_PROXY_TLS_KEY`;
existing pairs are validated and preserved. Partial pairs still fail closed.
The [direct Docker run example](../README.md#3-run-the-published-image-directly)
mounts the existing `.env` read-only and the data directory read-write.
Listen addresses default to `:1993` and `:1465` and can be
overridden using `IMAP_PROXY_LISTEN_ADDR` / `SMTP_PROXY_LISTEN_ADDR`. Publish or
forward 993/465 to them. Relative certificate paths in `.env` resolve beside that
file. An installation with disabled gateways does not need certificates.

## Private mail servers

Private and link-local destinations are denied by default. Prefer narrow entries
in `ALLOWED_MAIL_CIDRS`. `ALLOW_PRIVATE_MAIL_SERVERS=true` permits every RFC1918/
ULA address and materially expands SSRF reach. Link-local, loopback, multicast,
and unspecified addresses remain denied unless the implementation is changed.

## Updates and monitoring

Back up data, build immutable images, run unit/race/frontend/container checks,
then roll the single instance. Preserve `opaque_server_setup` or the original
externally supplied OPAQUE setup. Restart invalidates all sessions and decrypted
credentials. Enabled mail clients can reconnect using their retained encrypted
proxy configuration. Monitor `/healthz`; `/readyz` must not be considered ready until the
OPAQUE provider is configured. Logs intentionally omit headers, cookies, mail
bodies, and credentials.

IMAP and the complete webmail workflow remain incomplete. OPAQUE uses two
maintained implementations; the Go implementation is not independently audited,
and cross-library interoperability has not yet been independently verified.

### Configuration imports and resource budgets

`MAX_ACCOUNTS` (default 20, accepted range 1–100) controls each identity collection
and active provider group; `/session` exposes it to the browser. Monitoring has
its own budget, so it cannot consume the interactive reserve used by reads,
connection tests and Sent copies. Users behind one IP still share HTTP budgets.

Back up the complete data directory, including any `.vault-import.json` pending
journal. Startup rolls a committed encrypted configuration import forward; do
not delete or edit the journal to clear an error. A corrupt journal or unavailable
filesystem must be restored/fixed before access resumes. Import affects only the
owner's encrypted accounts, senders and preferences and revokes their sessions;
it does not replace OPAQUE authentication or the wrapped VMK. After an uncertain
import result, sign in again and inspect configuration before retrying.

For the reproducible application non-persistence check use `go test -race ./internal/audit`. The test starts isolated TLS providers and kills its own test
subprocess; it never targets an existing instance. Host/proxy/no-swap/no-core-dump
validation still belongs to the deployment operator and is not inferred by this
test.

## Backup validation and isolated restoration

1. Stop the instance and snapshot the **entire** data directory. Keep its private
   modes, `opaque_server_setup` (or the matching external secret), `index.key`,
   `users.json`, deletion journals, pending import/credential journals and all user
   directories together. Do not assemble files from different generations.
2. Restore into a separate private directory. Run
   `APP_DATA_DIR=/absolute/path/to/copy ./mailbox-aggregator --check-data`. This
   command inspects data without creating keys, changing permissions or starting
   listeners. Missing/corrupt components and pending transactions fail explicitly.
3. If a committed transaction is pending, start the application **only against an
   isolated writable copy**, with gateways disabled and a loopback HTTP listener.
   Startup finishes the journal; stop that copy and run `--check-data` again.
4. On an isolated restored instance, sign in and unlock using the expected current
   password. Verify the saved configuration decrypts before replacing production.
   The structural diagnostic cannot prove AES-GCM decryption without the user's
   key. Never regenerate OPAQUE/index keys to repair a restore.

`TEST_SERVER_BINARY=/path/to/test-binary node tests/account-lifecycle.mjs` automates
real JS/Go registration, password rewrap, VMK rotation, session revocation, five
collection decryptions, stopped-instance copy, non-mutating diagnosis, negative
checks for missing keys/permissions/setup and login/decryption after restore. It
creates its own temporary data and never uses a deployed instance. CI runs it.

`REGISTRATION_DISABLED=true` closes new account creation in both endpoints and
the UI. Existing users can still sign in. Compose bounds memory to
`APP_MEMORY_LIMIT` (512 MiB default), sets the memory+swap bound to the same value
and disables core dumps. Choose memory based on observed concurrency; the host,
container runtime and hypervisor still require qualification.

[nginx.conf.example](../deploy/nginx.conf.example) supplies streaming proxy
settings without request/response disk spooling or access logs. Include it in an
existing HTTPS server block and validate the actual proxy configuration; this
repository does not change an operator's running reverse proxy.
