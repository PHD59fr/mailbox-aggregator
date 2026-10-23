# Security design and limitations

## Encryption and keys

`vault.json` contains Argon2id parameters, salt, nonce, and an AES-GCM-wrapped
VMK. Each `*.enc` file contains only a versioned AES-GCM envelope. The password,
KEK, VMK, and plaintext configuration are never intended for backend persistence.
The backend's login-index HMAC key and OPAQUE server setup are authentication
secrets; neither can decrypt vault files. The server setup is required for auth
and must remain stable. It is not the vault key.

New installations without an externally supplied setup generate
`APP_DATA_DIR/opaque_server_setup` once. Its file mode is `0600`; atomic creation
does not overwrite an existing key, and startup validates and reuses the file.
The server never logs its value. Backups of this directory contain both the
OPAQUE server key material and authentication records, so protect and retain
them together. Externally managed `OPAQUE_SERVER_SETUP` remains supported and is
not copied into the data directory. Conflicting sources are rejected. A lost
key with existing account data, corrupt/empty files and symlinks stop startup;
the application never rotates the authentication identity to repair them.

The Go backend can see IMAP/SMTP credentials after an unlocked browser explicitly
activates them for a session. A compromised Go process, privileged host operator,
compromised browser, malicious extension, or malicious JavaScript served by the
host can steal live secrets. This system is not absolute zero knowledge.

The `@noble/hashes` Argon2id implementation is pinned, but its use in this
application has not received an independent security audit. Browser OPAQUE uses
`@serenity-kit/opaque`, based on opaque-ke; its maintainers report a penetration
test and white-box review by 7ASecurity through OTF. The Go implementation
(`github.com/bytemare/opaque`) is maintained by an RFC 9807 author, but explicitly
states that it has not been independently audited. This repository tests a Go
client/server protocol round trip; cross-library browser/Go interoperability has
not yet been independently verified. There is no insecure fallback.

## Optional mail-client credentials

IMAPS/SMTPS gateways are disabled unless the operator explicitly enables them
with a valid TLS certificate. They use a dedicated per-user proxy password and
do not expose the main OPAQUE password as a legacy authentication fallback.
The authenticated Settings API receives the proxy password and selected provider
credentials over HTTPS, then stores only a salted Argon2id/AES-256-GCM-encrypted
copy in `proxy.json`. The password, vault VMK and plaintext provider configuration
are not persisted. Ciphertext authentication binds the copy to its user UUID and
the source vault-file revisions; moving a record to another user does not grant
access. A strong generated password is recommended because backups of this
opt-in copy permit offline guessing independently of the main vault password.

Proxy authentication decrypts this limited copy in server memory without an
open browser. Web lock/logout intentionally leave independent client access
enabled. Disabling or rotating that access closes existing sessions, account
deletion cancels them, and source credential changes invalidate the affected
copy. Neither the OPAQUE setup nor the login-index key can decrypt this record.
TLS verification and outbound destination restrictions also apply to each
provider connection. The SMTP gateway chooses only among this user's enabled
sender copies and validates both the envelope and author headers; it does not
accept another user's sender address or unauthenticated relay.

## Refresh and loss

Cookies can preserve authentication across refresh. By default, encrypted tab
resume also preserves the unlocked vault in the same tab: sessionStorage holds
an AES-GCM-encrypted VMK and IndexedDB holds its separate non-extractable wrapping
key. The password and plaintext VMK are not stored. Resume checks the authenticated
user, CSRF binding, server lock revision and configurable human inactivity deadline.
Manual locking and logout clear resume material; disabling resume keeps the VMK
only in page memory. A browser may restore sessionStorage when restoring tabs, so
closing a window is not a reliable substitute for locking.

This is a trusted-browser convenience, not hardware-backed protection. Malicious
same-origin JavaScript, a compromised browser/profile or an extension may use the
stored wrapping key. Non-extractability does not protect against a complete profile
compromise. If encrypted browser storage is unavailable, resume fails closed and
the vault needs unlocking after reload. Losing the password means losing vault
access unless a separately designed client-side recovery scheme is added later.

## Filesystem

Data directories are `0700`, files are `0600`, user IDs are canonical UUIDs,
filenames are allowlisted, and symlinks are rejected at access. Writes fsync the
temporary file, rename on the same filesystem, and fsync the directory. This
assumes the host filesystem honors those operations. Ordinary multi-file transactions are not implied. Configuration import has a
specific encrypted recovery journal; it completes before collections become
readable and never stores the export password, VMK or plaintext settings.

Deletion removes application-managed local files and journals. It does not erase
provider messages, delivered mail, backups, filesystem snapshots, SSD remnants,
or host copies. Unlinking is not secure physical erasure.

## Network and sessions

Production cookies are Secure, HttpOnly, and SameSite=Strict. State changes need
a session-bound CSRF value. Sessions have 30-minute idle and 12-hour absolute
defaults and vanish on restart. Outbound hosts are resolved and every answer is
checked; the connection is made to a validated literal address while TLS verifies
the original hostname. Private networks require an administrator allowlist.

Credential activation is bound to the explicit lock revision known by the
unlocked browser. The revision is checked while installing secrets so pending
requests and stale tabs cannot undo a completed lock. Expired sessions and
individual expired provider leases are swept every second, and their managed
secret buffers are zeroed even if the client never returns. Lease expiry does
not change the explicit lock revision or invalidate an otherwise valid tab resume.

Deploy only behind HTTPS. Configure trusted proxy CIDRs explicitly. Never expose
the service directly with `SESSION_COOKIE_SECURE=false`.

Webmail submission uses the pinned `github.com/emersion/go-smtp` client, with
verified TLS/STARTTLS and PLAIN or LOGIN authentication over TLS. Socket deadlines
are clamped to one absolute operation deadline, including client-internal resets.
A DATA outcome that cannot be confirmed is never automatically retried. Failed
QUIT after accepted DATA remains a successful submission. Sent-copy repair cannot
resubmit SMTP. Real-provider qualification remains separate from local tests.

## Drafts, export and evidence scope

Local draft attachment descriptors are encrypted with their contents. Older
records are upgraded after their owner unlocks; another user's VMK cannot migrate
them. Conditional IndexedDB replacement prevents background migration from
replacing newer work. Browser storage can fail or be cleared; these drafts are
not a substitute for provider-side IMAP Drafts or a cross-device backup.
While a previous submission remains pending, a restored composer is read-only to
prevent edits from being discarded by a late SMTP success. SMTP already submitted
may finish after lock/logout; deletion, import or process termination can still
interrupt operations with an uncertain remote result. Never retry ambiguous sends
automatically.

Configuration exports contain provider secrets encrypted under the chosen export
password. The plaintext bundle is limited to 16 MiB and incoming export JSON to
32 MiB; per-collection server limits still apply on import. Imports replace settings, revoke all of that user's sessions and
invalidate gateway snapshots. The server sees only re-encrypted envelopes. Any
missing/ambiguous import response must be reconciled by signing in again.

`internal/audit` now tests authenticated HTTP, real TLS IMAP/SMTP, Sent, source and
attachment reads, redirection, provider rejection and a killed/restarted process.
It scans explicitly controlled data, temporary and log directories with binary,
long-line and MIME/base64 positive controls; an inspection error fails the test.
This evidence covers the tested application process and supports. It does not
certify a deployed reverse proxy, Docker host, swap, core-dump handler, library
version outside the tested build, or real external provider. Those deployment
checks and independent cryptographic review remain release requirements.

## Credential changes

Password changes require a one-use grant from explicit OPAQUE reauthentication
within five minutes, an authenticated owner session and CSRF protection. Login
alone does not authorize changing credentials. The client rewraps the VMK, or
optionally rotates it and re-encrypts all five configuration collections.
`.credentials-update.json` contains OPAQUE records, wrapped metadata, encrypted
collections and disabled gateway settings only. Recovery rolls the committed
transaction forward before any user-file access. All eight revisions must match
before commit. Corrupt or conflicting journals stop startup/access.

Sessions and pending login exchanges bind to the authentication-file revision.
Old in-flight configuration writes compare that revision again under the file
lock, preventing old-key ciphertext from overwriting a rotated vault. Completion
revokes all owner sessions and activated credentials and disables gateway access.
Each other tab must obtain a new session; its old tab-resume material cannot
authenticate. Local draft migration retains prior keys for seven days inside the
new VMK-encrypted config; this is an explicit migration window, not immediate
cryptographic destruction of all historical keys. Old backups are unaffected.

Argon2id runs in a dedicated browser Worker and the Worker is terminated on local
lock. Tests/non-Worker runtimes use the pinned cooperative async implementation.
The same parameter bounds and normalization apply to both paths. Mobile-device
performance and an independent cryptographic review remain unqualified.

## Optional authentication and connectors

[OPTIONAL_FEATURES.md](OPTIONAL_FEATURES.md) specifies TOTP's online-only threat
model, encrypted `mfa.json`, setup-key backup requirement, intermediate logins,
one-use recovery codes and independent gateway scope. It also specifies session-
and revision-bound OAuth, encrypted refresh tokens and web-only OAuth/SCRAM profiles.
No VMK or master-password decryption capability is introduced server-side.

ManageSieve, CardDAV, LDAP and Google contacts are opt-in and bounded. Browser cancellation stops their requests on lock; late fixed-provider HTTP results are discarded by session/revision checks and remain bounded by their 20-second deadlines. New ports are never implicitly allowed. ManageSieve mutation responses can
be ambiguous and its hash recheck is not an atomic remote CAS. Inline images use
validated PNG/JPEG MIME parts and known Content-IDs; preview remains sandboxed.
BEC hints and image permissions do not establish sender authentication. Full
provider qualification and independent security review remain outstanding.

Repeated MFA failures generate an operator WARN event `mfa_failed_attempts` at
five verification failures per IP/minute, once per window. This reuses the HTTP
log's client IP and request ID; it contains no OTP, recovery code, flow ID or
account identifier and never submits mail. RAM aggregation is bounded to 4096
windows; log retention remains the operator's policy.
