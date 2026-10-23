# Optional mail features

The existing password accounts, aggregated Inbox, OPAQUE login and browser vault
remain the defaults. New preferences are encrypted in the existing collections;
they require no collection migration. Nothing here schedules sending or provides
a new provider worker while the browser is closed. PHP and EWS are excluded.

## Unified views and personal organization

Mail supports Unread, Sent, Drafts, Starred, All mail and a saved selection of
folders. Non-Inbox folders require the account's existing synchronization opt-in.
Role mappings, including None, apply. All mail prefers the provider's All Mail
role to avoid fetching both it and every Gmail label. Other duplicate folder
copies remain possible. Lists merge loaded windows, with separate cursors per
account/folder, two simultaneous reads, at most 100 sources and the existing
50-row pages. They are not a globally consistent snapshot or a persistent index.
Period and volume settings describe the loaded window, not provider retention.

Boolean search accepts AND/OR/NOT, groups and text/from/to/cc/subject/is fields.
Queries are bounded to 2048 bytes, 64 tokens, 32 terms and eight nesting levels.
Simple search and sender/subject filters highlight escaped list text. Boolean
expressions are evaluated by IMAP; their operators are not highlighted as text.
Saved searches preserve scope. Optional encrypted history retains 20 queries for
30 days and can be cleared. The retention limit is applied on reading/writing;
it is not a background deletion service. Saved-search alerts are opt-in, checked
every two minutes in the visible, unlocked mail view: three searches, ten folders
each, one request at a time. The first successful check establishes a baseline.
Only current result windows are compared; misses, provider failures and scope
limits are possible. Notices remain in the open mail view. No system notification,
closed-browser surveillance or guaranteed delivery is promised.

Tags and snooze use encrypted application metadata, keyed by account, exact folder,
UIDVALIDITY and UID. They do not set IMAP keywords or move messages. Snooze hides a
message until its date, evaluated every 30 seconds while open or at next opening.
Visits are optional, at most 100 over 30 days. The whole reference register is
limited to 1000 entries and 512 KiB. Exact UID requests retrieve old referenced
messages without scanning recent pages. Moving a message or changing UIDVALIDITY
can make a reference stale; remove or reapply it explicitly. Message-ID is not used
as a unique reference. No body is saved with these metadata.

## Composition

Templates retain To/Cc/subject/text/HTML, at most 20 and 256 KiB in total. Insertion
appends the body and fills empty fields; attachments, Bcc and signatures are
excluded. Plain-text signatures with escaped HTML rendering remain supported;
arbitrary rich signature HTML has not been added. Reply-To remains an identity
setting. Auto-Bcc is visible before sending, deduplicated, and excluded from the
public SMTP MIME; private Sent/Drafts can preserve Bcc as before.

Drag-and-drop uses the same attachment limits and encrypted draft persistence as
the file picker. Forward as attachment fetches the authorized original MIME and
attaches those bytes as message/rfc822. It fails visibly if unavailable or too
large. Forwarding is never submitted automatically.

Inline images support PNG/JPEG only, eight images at most, 2 MiB per image,
4096 pixels per side and 16 million pixels. The server validates the actual image
header and declared format. Images share the existing total attachment budget.
Outgoing MIME uses multipart/related and Content-ID, with ordinary files outside
the related group in multipart/mixed. Only IDs belonging to validated images
survive outgoing HTML sanitization; remote/data/SVG image HTML is excluded.
Encrypted local drafts retain IDs with their encrypted attachment descriptors.
IMAP draft editing fetches attachments explicitly and preserves supported IDs.
Visual mode and inert local preview support images; Markdown editing is disabled
on a draft containing inline images to prevent loss. Reading received mail keeps
the existing external-image policy and does not automatically download embedded
image bodies. Unsupported embedded formats should be edited in another client.

Custom headers are restricted to Organization (256 bytes), Importance
(low/normal/high), and X-Priority (1/3/5). All control characters, unknown names,
duplicates with different casing, and identity/MIME/authentication fields are
rejected server-side. Local and IMAP drafts preserve these supported fields.

Recent To/Cc suggestions and collection into local contacts are separate opt-ins.
Only confirmed SMTP success triggers collection; Bcc/auto-Bcc and ambiguous
outcomes are excluded. Suggestions retain at most 100 addresses over 30 days.
Disabling contact collection does not delete already imported contacts.

Responses/reply-all retain a bounded References chain, appending the parent
Message-ID and preserving the root plus newest parents (32 identifiers, 1000
characters each at the API). The reader fetches headers with a 32 KiB budget. Local
encrypted and explicit provider drafts retain this metadata through reopening;
ordinary forwards start a separate conversation.

## OAuth Google and Microsoft

Configure APP_PUBLIC_URL and the provider's client ID/secret in the environment.
Register exactly `/api/v1/oauth/google/callback` or
`/api/v1/oauth/microsoft/callback` below that origin. Provider endpoints are fixed;
tenant discovery or arbitrary OAuth issuer URLs are not accepted. The flow uses
Authorization Code, PKCE S256, random one-use state, a five-minute deadline and
the original authenticated session's lock revision. The public callback does not
depend on a cross-site SameSite=Strict cookie. The browser subsequently consumes
the result through an authenticated POST. Provider HTTP requests share a 16-request broker limit with 20-second deadlines. Tokens never enter redirects or disk
files in the OAuth broker. Configure reverse-proxy logs to omit callback query
strings, because providers return a temporary authorization code there.

Google requests `https://mail.google.com/`. Microsoft uses the common v2 endpoint
for personal/work/school accounts and requests offline_access, delegated
IMAP.AccessAsUser.All and SMTP.Send for Outlook. Administrative policy may disable
IMAP or SMTP AUTH even after consent. Google production verification or Microsoft
administrator consent may be required. Choose Outlook.com or Microsoft 365
explicitly after authorization. The supplied email must pass an IMAP OAuth probe
before saving; an unverified ID-token claim is not used as identity proof.

Access/refresh tokens are encrypted in preferences (`oauth-grants`). Account and
SMTP records share a grant ID so they share refresh rotation. Refresh happens on
demand in an unlocked browser, serialized across local tabs where Web Locks is
available and guarded by collection revisions. Other-device refresh rotation may
require retry/reconnection; there is no distributed refresh coordinator. The Go
session receives only the current access token for a lease bounded by its expiry.
Lock cancels/discards late flows, and stale activations cannot reinstall secrets.
Disconnect Google can request revocation; Microsoft requires removing application
consent in the provider's account/tenant portal as well as forgetting locally.

XOAUTH2 never falls back to treating a token as a password. Existing password
accounts remain supported. OAuth profiles are **web only**: external gateways
continue using their existing encrypted password snapshots without an OAuth
refresh daemon. Explicit SCRAM-SHA-256 IMAP profiles are also web only initially;
LOGIN stays the default. SCRAM has no silent downgrade, verifies the server's
proof and accepts server work factors only from 4096 to 1,000,000 iterations.
SHA-1 and SCRAM-PLUS/channel binding have not been added.

Provider references: [Google XOAUTH2](https://developers.google.com/workspace/gmail/imap/xoauth2-protocol),
[Microsoft IMAP/SMTP OAuth](https://learn.microsoft.com/en-us/exchange/client-developer/legacy-protocols/how-to-authenticate-an-imap-pop-smtp-application-by-using-oauth).
Local fake-provider tests do not qualify real Google or Microsoft accounts.

## TOTP and recovery codes

Optional TOTP adds a second factor after successful OPAQUE. The intermediate login
has no ordinary session cookie/CSRF and cannot access the vault or activate mail
credentials. It expires after two minutes, five attempts per flow and ten attempts
per minute per account. TOTP uses six digits, SHA-1, 30-second steps and a one-step
clock window, with a consumed counter preventing reuse. Keep the host clock synced.
Enrollment requires explicit OPAQUE reauthentication and a confirming TOTP code.
The manual secret and otpauth URI are shown during setup; a QR dependency is not
required. A user can enroll before any mail credentials have been activated.

The verifier is a new optional `mfa.json` per user, encrypted with AES-GCM under a
domain-separated key derived from the original canonical OPAQUE server setup.
It contains the TOTP secret/counter and hashes of ten random recovery codes.
It is not a VMK or provider-secret decryption key. Recovery codes are shown once,
consumed atomically, and replaced on regeneration. Disable/regenerate requires
fresh explicit OPAQUE reauthentication plus TOTP or an unused recovery code.
Configuration changes keep the current session but revoke other web sessions.
Independent IMAPS/SMTPS passwords remain valid; they are not protected by TOTP.

Back up `mfa.json` with the account directory **and the unchanged OPAQUE setup**.
The offline diagnostic validates its authenticated envelope. Password/VMK changes
and user configuration import do not disable MFA; account deletion removes it.
Configuration exports do not export this server authentication verifier. Restoring
an old server backup can restore previously consumed counters/codes; reconcile
MFA after such a restore. Recovery codes replace only the second factor, never
the master password or vault key. Losing all factors is not recoverable through
the web UI. Host administrators already control the authentication storage; this
online factor does not add an offline encryption factor to the vault.
See [RFC 6238](https://www.rfc-editor.org/rfc/rfc6238).

The server emits WARN event `mfa_failed_attempts` on the fifth MFA verification
401 response from an IP in a 60-second window, once per window. This is an
operator log alert, not an email notification. It records client IP, request ID,
threshold and window only; no account, challenge, code or request body. At most
4096 windows are held in RAM and expired windows are purged on the next failure.
Log retention and any external notification rules belong to deployment operations.
Existing authentication rate limits remain in place.

## ManageSieve

Enable SIEVE_ENABLED and explicitly allow the configured port (usually 4190).
The encrypted `sieve-settings` record contains connection settings, draft rules,
sender lists, the Screen folder choice and the last five saved script backups.
Credentials are sent transiently only for explicit unlocked-session requests.
TLS/STARTTLS is verified and required. STARTTLS replaces all pre-TLS capabilities.
PLAIN SASL runs only inside that verified TLS channel. OAuth SASL for ManageSieve
is not implemented; use a provider-supported dedicated password if available.

The client supports listing, reading, CHECKSCRIPT, PUTSCRIPT, activation,
deactivation and deletion. Limits: 64 scripts, names 128 bytes, script 64 KiB,
response stream 256 KiB, 20-second deadline, existing connection budgets and lock
cancellation. Unknown/custom scripts stay editable as text; generating graphical
rules always starts a new draft. ALL/ANY, rule order and stop are explicit.
fileinto/imap4flags depend on advertised extensions. Discard/redirect and active
script replacement are visible decisions. Inactive deletion or activation requires
the loaded content hash and expected active name; overwriting saves an encrypted
backup first. Recheck detects prior remote changes but cannot provide an atomic
CAS, which ManageSieve does not define. Avoid concurrent editors. Lost mutation
responses require inspecting remote state, never automatic retry.

The header simulator covers the graphical editor's From/To/Subject comparisons
only. CHECKSCRIPT is syntax validation, not delivery simulation. Folder existence,
quotas and provider-specific interpretation still require provider tests.
Like means keep; Screen means fileinto the chosen folder. Lists can be undone
locally, then reviewed rules must be regenerated/saved/activated to change live
delivery. Screen folder creation is an explicit existing IMAP create action;
collisions and quota failures are not overwritten or renamed automatically.
See [RFC 5804](https://www.rfc-editor.org/rfc/rfc5804).

## External contacts

CARDDAV_ENABLED, LDAP_ENABLED and GOOGLE_CONTACTS_ENABLED default to false; disabled
routes reject requests server-side. Connector options are exposed to the UI.
Required ports must be explicitly added to ALLOWED_MAIL_PORTS (HTTPS 443, LDAPS
636, required LDAP STARTTLS 389); no private destination exception is inferred.
Use precise ALLOWED_MAIL_CIDRS exceptions where needed. Existing mail defaults do
not change. Remote results are previews, separated from the local encrypted book
until the user imports. Local imported contacts remain after disconnecting.

CardDAV uses the exact HTTPS address-book collection URL and a read-only REPORT
with Depth:1, names/email data and ETags. No discovery, redirect credential
forwarding, remote writes, automatic deletion or bidirectional synchronization.
Response/XML depth and card size are bounded (1 MiB total, 24 levels, 64 KiB/card,
50 cards). Over-limit books need a narrower collection or an explicit export.
ETags are returned as metadata, not used to claim conflict-free two-way sync.
[RFC 6352](https://www.rfc-editor.org/rfc/rfc6352).

LDAP supports verified implicit TLS or mandatory STARTTLS, authenticated bind,
configured base DN, fixed cn/mail attributes, escaped search text, no referral
following, explicit 50-entry pages with bounded opaque paging cookies, a 100-address UI preview limit and 20-second deadline.
Results are limited, not an exhaustive directory sync. BER packet and stream sizes
are capped at 1 MiB. There is no LDAP account login integration.

Google requires People API enabled in the registered application and the separate
explicit contacts.readonly consent option when connecting. Each request lists one
page of 50 people from a fixed Google endpoint; the next page is another explicit
click. Only names/emails are imported. The recipient picker also offers an explicit
Google page lookup and selection without importing contacts locally. Results are
held only in component memory, deduplicated, subject to the shared recipient limit
and cleared on page/account changes, unmount or lock. Local contacts remain
independent. Opt-in recent-recipient collection after confirmed sending keeps its
separate existing behavior. Revoking consent or missing scopes surfaces
an error and does not affect local contacts or password mail accounts.
[People API reference](https://developers.google.com/people/api/rest/v1/people.connections/list).

## Operational choices and remaining qualification

CSV account import previews at most 50 rows/256 KiB, rejects unknown columns and
partial SMTP groups, preserves existing defaults/records, skips duplicate receiving
connections and validates links/limits. Browser encryption precedes the existing
three-collection import transaction. Conflicts cannot partially import rows.
Success revokes web sessions and invalidates gateway snapshots, exactly as existing
configuration import; the confirmation explains this. Plaintext CSV is never sent
or persisted by the server. The original user-selected file remains on their device.

MTA-STS/TLS-RPT are responsibilities of the domain's sending/receiving providers.
This application submits SMTP over verified TLS and does not deliver directly to
recipient MXes. No DNS policy diagnostic is added: it would not prove end-to-end
delivery protection. See [RFC 8461](https://www.rfc-editor.org/rfc/rfc8461).

BEC hints identify a different Reply-To domain, a different email inside the From
display name, or an internationalized domain. They do not trust arbitrary
Authentication-Results headers, authenticate senders, score domain similarity or
guarantee fraud detection. Image permission is not sender verification.

CAPTCHA is deferred: no provider/operational need has been selected, and existing
rate limits remain. No third-party script or CSP exception is introduced for it.
English and French catalogs are available; English is the default. Language and direction are edited only in Settings and saved
in the encrypted preferences collection (`interface-language`). The login and
registration screens contain no preference controls. Only
locale/direction are also remembered in localStorage for the login screen; no mail
or identity values are stored there. Automatic follows the chosen language (both
provided languages use LTR); LTR/RTL can be forced in Settings. UI spacing follows
direction; email text, addresses and provider commands retain their own content.
Unknown provider diagnostics remain verbatim. `npm run test:i18n` checks catalog
parity, interpolation parameters and static UI extraction; adding a new language
requires its own catalog and linguistic/accessibility qualification.

Real provider qualification, two-device refresh behavior and broader assistive
technology/mobile checks remain explicit tasks in `todo.md`. The implementation
is not a substitute for an independent authentication/security review.

Display/keyboard, recent-message history, combined-folder selection and search
history preferences are grouped in Settings. Mail applies the saved display
preferences when it opens, without mounting their forms. Advanced queries and
saved-search actions are available through the mail filters; alert checks keep
running while those controls are closed, subject to the existing session limits.
