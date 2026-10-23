# HTTP API

Contacts and groups are managed in the authenticated `/contacts` web page,
separately from Settings. The address book remains browser-encrypted in the
existing `preferences` vault collection under `address-book`. Contacts can have
optional `group_ids`; the record's optional `groups` array contains group IDs and
names. Older address books without groups remain readable. Removing a group
retains its contacts and removes only the corresponding memberships. Updates use
the existing file revisions and conflict retries. The composer supports selecting
multiple contacts for To, Cc or Bcc, excluding existing addresses and respecting
the shared 100-recipient limit. Optional read-only external connectors return
transient previews; they never replace the encrypted local address book.

The canonical schema is [openapi.yaml](openapi.yaml). Errors use:

```json
{"error":{"code":"VAULT_LOCKED","message":"The vault must be unlocked before accessing mail."}}
```

Authenticated `GET /proxy` advertises separate `imap_hostname` and
`smtp_hostname` fields, falling back to the shared `MAIL_PROXY_HOSTNAME` when the
corresponding `IMAP_PROXY_HOSTNAME` / `SMTP_PROXY_HOSTNAME` is unset. The legacy
`hostname` field remains available. Optional `web_url` contains `APP_PUBLIC_URL`
when configured. These are public connection details; provider credentials remain
absent from the response.

Implemented routes are health/readiness, session inspection/lock/logout,
self-deletion, SMTP submission, IMAP monitoring/folders/message reading, opaque vault metadata/file GET/PUT, and grouped encrypted configuration import. Vault updates require the
quoted revision from GET in `If-Match`; a stale value returns 409. Read
`X-Resource-Revision` in preference to `ETag` (retained for compatibility):
compression proxies can weaken or replace the HTTP ETag. Both headers initially
contain the same stored-file revision. Vault and proxy settings use
`Cache-Control: no-store, no-transform`; the browser also bypasses its cache.

Session views include `idle_timeout_seconds`, `max_accounts` (default 20 per incoming/outgoing group) and `lock_revision`, incremented on explicit lock but not on
upstream credential lease expiry. The browser binds its optional encrypted tab
resume to that value and the current user/CSRF session. A configurable human-idle
deadline (default 15 minutes, 1–60 minutes in Settings) clears resume and provider
credentials; background monitoring never counts as human activity. Reload does
not reset the deadline. Server-side authentication expiry remains an independent
upper bound. Production's Secure session cookie requires HTTPS on mobile/IP
access; the UI does not downgrade to insecure cookies or a bearer-token fallback.

Every `POST /session/credentials` must include the `lock_revision` known by the
unlocked tab. The server compares it atomically when installing the credentials;
an activation pending before a lock, or coming from an older tab, returns
`409 LOCK_REVISION_CONFLICT` and cannot restore secrets. Missing revisions return
`400 LOCK_REVISION_REQUIRED`; older clients must reload the updated application.
`DELETE /session/credentials` returns 204 with `X-Lock-Revision` identifying that
specific lock. Configuration edits may keep their local vault open and adopt
their own reset's next revision; an intervening lock requires unlocking again.
Expired authentication sessions cannot install credentials or be reauthenticated.
The server also sweeps expired sessions and individual provider leases every
second, erasing their managed secret buffers without waiting for another request.

Credential activation accepts `mode:"merge"` to update only the supplied identities.
The browser uses this mode for targeted reads and sending. Concurrent activations
preserve other identities and atomically enforce the resulting group limit.
The default `replace` mode preserves the earlier group replacement contract.
A 200 response includes the session plus optional `account_errors` and
`smtp_errors` maps. Monitoring displays those errors and watches only successful
account IDs using the optional `accounts` array on `/mailboxes/watch`.
An entirely failed group is rejected; it is never reported as a healthy empty view.

Monitoring allows up to `max_accounts` connections per user, plus a separate
interactive reserve of `max(2, floor(max_accounts/5))`. Monitoring is globally
bounded to `max(64, max_accounts)` and interactive operations to 16; additional
tabs share their user's budgets. Reaching a budget returns 429 `WATCH_LIMIT`.

### Encrypted configuration import

`POST /vault/import` takes `{files:{accounts:{data,revision},smtp:{data,revision},preferences:{data,revision}}}`.
Each `data` is an encrypted file envelope; each `revision` is the quoted current
`X-Resource-Revision` of that collection. All revisions are checked before any
replacement. Missing revisions return 428; stale revisions return 409 without
changing any collection. The request limit is four times `MAX_REQUEST_BYTES`;
each envelope still obeys `MAX_VAULT_FILE_BYTES`.

The durable import journal contains only encrypted envelopes and completes before
any later collection read/write, or during startup. A 204 means all three were
committed. Every session belonging to that user is revoked, active operations are
cancelled and proxy snapshots are invalidated; sign in again afterwards. A lost
response or `IMPORT_UNCONFIRMED` may follow a committed import: reload and inspect
the settings rather than retrying automatically. The API never receives the
export password or decrypted collection contents.

The public authentication endpoints implement registration start/finish and
login start/finish. Registration/login messages use the browser library's
unpadded base64url encoding and the protocol library determines their contents.
Successful completion sets the HttpOnly `mail_session` cookie and returns the
session view including its CSRF token, except for enrolled MFA accounts: those
receive only `{mfa_required:true,flow_id,expires_in:120}` until `/auth/mfa/verify`
succeeds. `reauth/start` and `reauth/finish` run a
fresh OPAQUE exchange against the current account and are protected by session
authentication and CSRF. Folder browsing and provider-side filtered search are
available.

Account and SMTP configuration CRUD runs in the browser against encrypted vault
files (`accounts` and `smtp`). Creating an IMAP account does not require an SMTP
configuration. The browser decrypts the current collection, edits it, and sends
an encrypted envelope with the original resource revision in `If-Match`. Collection reads
accept both the initial empty arrays and maps keyed by record ID. The server
never parses these collections as plaintext account or SMTP records.

Receiving identities live in `accounts`; sending identities, including their
sender name/email and SMTP password, live in `smtp`. Both are independent. An
optional `smtp_config_id` on a receiving identity links it to a sending identity;
the Compose action on that link preselects its sender. Removing the link leaves
both identities intact. SMTP submission works independently of IMAP.

The encrypted account preference `save_sent` defaults to true when a sender is
linked. The composer chooses one linked receiving identity (preferring the
reply's original mailbox, then the default), and lets users skip the copy or
choose another. `save_sent:false` or `folder_mapping.sent:null` disables copying
to that mailbox. Activate that mailbox's credentials before sending, and include
`sent_copy:{account_id,folder,exclude}` in the send/redirect request. `folder` is
the exact manually mapped path or empty for automatic SPECIAL-USE/name detection;
`exclude` contains other manually assigned role paths (at most six). Paths must
be non-INBOX, valid UTF-8, at most 1024 bytes and contain no NUL/CR/LF.

After SMTP acceptance, IMAP selects the existing Sent folder and checks the
generated Message-ID (Resent-Message-ID for redirects) before one APPEND with
Seen and the sending date. It never
creates folders or retries an uncertain APPEND. The private copy includes Bcc;
the SMTP payload never does. Copies stay on the mail provider, not local disk.
A successful response includes `sent_copy.status` (`saved` or `failed`); a failed
copy still returns HTTP 200 with `status:sent` and a separate explanatory message.
The browser also continues SMTP submission if preparing the IMAP credentials
fails and displays a copy warning after delivery. Do not resend to repair a copy.
Providers that save asynchronously can still produce duplicates after the initial
Message-ID check; disable app copying in that mailbox's settings when appropriate.

To send, the unlocked browser activates the selected SMTP credentials via
`POST /session/credentials`, including `sender_name` and `sender_email`, with a
five-minute lifetime. It then calls `POST /messages/send` with `smtp_config_id`,
`to`, optional `cc`/`bcc`, `subject`, plain `text`, optional sanitized `html`,
and optional base64 attachments. From is bound to the
activated identity. SMTP credentials and message bodies are never written to
disk by these endpoints; only the encrypted settings are persisted. SMTP-only
activation replaces the temporary sending credentials while preserving active
receiving credentials. Sending uses password authentication over verified
TLS/STARTTLS; OAuth is not exposed by the composer. Attachments are limited to
20 files and 10 MiB total decoded data; they are processed only in memory.

The composer accepts visual editing or Markdown (headings, lists, links, tables,
quotes and fenced code). Markdown is converted locally to the same HTML and plain
text payloads; the API does not accept Markdown source. The preview is a local
opaque-origin sandbox with inert links and no external content. A fixed public
stylesheet supplies formatting without relaxing the app CSP. Outgoing HTML is
sanitized again and receives passive inline email styling, including table
borders and code blocks, so the formatting travels with the MIME message.
Colors/alignment unsupported by Markdown are simplified only when the Markdown
source is edited; switching views alone keeps the visual document.

Appearance palettes (Slate, Paper, Groove, Cinema, or System) are stored as
`appearance:{id:"appearance",theme:...}` in that user's encrypted `preferences`
collection. The saved choice is loaded after vault unlock on any device and
cleared from memory on lock/logout or account changes. Failed saves restore the
previous palette. Other preferences are preserved using the collection's ETag.
Login starts with the neutral Slate palette. The obsolete device-wide
`mailbox-theme` value is discarded rather than copied into an unknown user's
profile. Themes do not change the content or colors of outgoing email.

To monitor incoming mail, activate receiving credentials with `accounts` and
omit `smtp`; existing sending credentials are preserved. Then POST
`/mailboxes/watch` with `refresh_interval_seconds` (10–300, default 30).
Its `application/x-ndjson` response emits `ready`, `heartbeat` and `status`
events. Status includes `account_id`, `messages`, `unseen`, `uid_next`,
`uid_validity`, `checked_at`, `mode` (`idle` or `polling`) and an optional safe
error. Monitoring prefers IMAP IDLE over verified TLS/STARTTLS and falls back
to periodic STATUS connections. It never fetches bodies or marks messages read.

Send POST `/mailboxes/presence` with the response's `X-Watch-ID` as `watch_id`
every 15 seconds. Presence is session-bound and requires CSRF. Streams close on
lock/logout, credential expiry, disconnect or 45 seconds without presence.
They are limited to 20 upstream connections per user and 64 globally. The
browser reconnects after credential renewal and pauses when hidden.

Temporary credential-activation DNS failures return `503 MAIL_SERVER_UNAVAILABLE`,
not invalid input. Browser monitoring and message-list reads retry network/temporary
server failures after 3, 6, 12, 24, then at most every 30 seconds; retries stop on lock
or unmount and pause while hidden. Rejected IMAP credentials return `422 IMAP_AUTH_FAILED`
and are not automatically retried. SMTP submissions, redirects and connection tests
are never automatically retried.

The encrypted receiving-account preference `sync_folders` (default false) enables
`GET /mailboxes/{account}/folders`. It returns `folders` and `truncated`, with at most
200 folder names, hierarchy delimiters, selectable flags, message/unseen counts and
UIDNEXT/UIDVALIDITY. A folder-specific error does not discard the other folders.
The optional `role` identifies `sent`, `drafts`, `trash`, `archive`, `spam`, `all`
or `outbox`; `special_use` marks authoritative server metadata. Names from common
provider/localized layouts are fallback-only. Standard navigation labels point
to these real folder paths; no empty placeholder folders are invented.
The encrypted per-account `folder_mapping` object optionally assigns exact server
paths to `sent`, `drafts`, `trash`, `archive`, `spam`, `all` and `outbox`.
An omitted/empty role stays automatic; `null` (None in Settings) disables that
standard role. `{}` resets all roles to automatic. A disabled role does not hide
its server folder; the folder remains available under its own name. Setting
Trash to None disables Delete in the browser until a Trash folder is chosen.
Manual assignments override server roles and name guesses, never rename/create
folders and cannot share a destination or target INBOX. Missing saved choices are
retained rather than silently reassigned. In Settings, save a new mailbox (or
connection edits) before loading its existing folders. The picker is on-demand,
works independently of `sync_folders`, and shows selectable non-INBOX folders.
Enable `sync_folders` to display them in mailbox navigation. Sent is also used
for the linked sender's optional sent copies. Draft saving is an explicit provider action; local encrypted autosave remains separate.
Navigation groups nested folders using each server-provided hierarchy delimiter.
Real parent folders can still be opened, while missing or non-selectable parents
act only as expandable groups. Child labels are shortened in the tree; opening
a child always uses its exact full server path.
This uses bounded LIST/STATUS connections, no message bodies or persistent local cache.
Folder names/counts refresh at the configured fallback interval while the inbox keeps
IDLE monitoring. All message endpoints, including documents, source, attachments,
flags and redirects, accept an optional opaque `folder_id` query reference (default INBOX); folder names are posted privately to obtain references.
UIDs and UIDVALIDITY are always scoped to that folder. Switching folders clears the
reader. This does not create, move, rename or delete provider folders.

`POST /mailboxes/{account}/messages/{uid}/trash?uid_validity=...&folder_id=...`
requires CSRF and the exact receiving identity/folder/UIDVALIDITY. It moves only
that message to the provider's existing Trash, never permanently deletes it or
creates a new folder. MOVE is preferred. Without MOVE, COPY must succeed before
STORE Deleted, then targeted UID EXPUNGE; UIDPLUS is mandatory. An unrestricted
EXPUNGE is never issued. Unsupported safe moves or absent Trash return 422;
already-in-Trash returns 409. Ambiguous outcomes return
`502 IMAP_TRASH_UNCONFIRMED`; do not automatically retry. Refresh first.
The optional POST JSON field `trash_folder` selects the manually mapped destination;
it must exist and be selectable on this same receiving identity. Missing manual
destinations return 422 without falling back or deleting the original. With
automatic Trash, the POST JSON array `trash_exclude` excludes folders manually
assigned to other roles (maximum six paths). Each path is at most 1024 UTF-8 bytes,
without CR/LF/NUL, and cannot be INBOX. These hints apply only to this operation,
are not persisted server-side and do not grant access to other identities.

`GET /mailboxes/{account}/messages` returns up to 50 newest INBOX summaries,
`uid_validity`, `total` and `next_before` (zero at the end). To paginate, pass
`before={next_before}&uid_validity={uid_validity}`. The server resolves the UID
anchor and fetches a bounded window; it never searches or fetches the entire
mailbox. A missing anchor or changed UIDVALIDITY returns `409 MAILBOX_CHANGED`.

Optional `search` and `subject` (up to 512 UTF-8 bytes), `seen=read|unread`,
`from` and `to` (up to 254 bytes) combine as an AND filter on the selected folder.
The `to` criterion matches either the To or Cc header. `since` and `until` are
received dates in YYYY-MM-DD format, with both endpoints inclusive; invalid or
reversed date ranges return 400. The browser shows a searching state in its top
loading banner and message heading, and a count of active structured filters.
Search runs on the IMAP provider's message text/headers, not only on loaded summaries. SEARCH results
are bounded to 4,096-message sequence windows; at most 50 matched envelopes are
fetched and bodies are not downloaded by the app. Up to 64 windows per request
and the existing 25-second deadline prevent unbounded work. `422 SEARCH_TOO_BROAD`
asks the user to refine the search if scanning would exceed that bound. The
existing UID/UIDVALIDITY pagination also applies to filtered results. `total`
remains the folder's total, not an estimated total of search matches. Browser
queries are debounced and cancelled when changed; query/filter state is cleared
on lock/logout and is never saved in localStorage. Explicit saved searches and optional history are encrypted in the vault. The browser sends criteria to the private POST search endpoint; legacy GET criteria remain accepted for compatibility.

`GET /mailboxes/{account}/messages/{uid}?uid_validity={uid_validity}` returns
the envelope, text/HTML alternatives and attachment metadata. MIME transfer
encodings and text charsets are decoded. HTML is sanitized without active content;
its external image URLs are inert JSON until rendered with consent. Each text
alternative is capped at 2 MiB decoded. Skipped
oversized alternatives set `truncated: true`. Attachments are not loaded until
`GET /mailboxes/{account}/messages/{uid}/attachments/{part}?uid_validity=...`.
Downloads are attachment-only, application/octet-stream, at most 10 MiB decoded.

All reading endpoints use this session's activated receiving credentials only,
return `Cache-Control: no-store`, and cancel work on lock/logout. Missing or
expired receiving credentials return 423; missing messages return 404; upstream
errors return a generic 502 without leaking provider details. Reading is
read-only and does not change the provider's unread flags by itself.

`GET /mailboxes/{account}/messages/{uid}/document?uid_validity=...` serves the
sanitized HTML/CSS in a sandboxed iframe with an independent CSP. The app's CSP
is not relaxed. External images are replaced with visible `[Blocked image]`
placeholders (including escaped alt text when present) and blocked by frame CSP unless
`external=1` was explicitly requested. Only HTTP(S) images may be enabled; scripts,
forms, frames, CSS URLs, external fonts and external stylesheets stay blocked.
Embedded frames, objects and media have `[Blocked external content]` placeholders;
image consent never enables that active content.
Inline CSS/legacy background-image URLs also have a placeholder and remain blocked.
The direct Trust sender and Trust domain buttons beside From store exact-address
or whole-domain image permissions in encrypted preferences. Domain rules are
displayed as `*@example.com`, match every sender at that exact domain and do not
include subdomains. Each button can revoke its own rule. Settings → Security lists trusted senders and domains
separately, with per-rule removal; removing one sender does not remove other rules.
Broader domain rules still apply unless removed separately. Trust only authorizes
image loading and does not authenticate the sender. New-mail/sent notices float
above the current page without changing its layout and disappear
after six seconds; another notice restarts that timer and lock/logout cancels it.
Encrypted preferences remember exact sender/address-domain rules; they are not
proof that a sender is authenticated.

`GET /mailboxes/{account}/messages/{uid}/source?uid_validity=...` returns the
original RFC 822/MIME bytes, including all headers, as an attachment-only
application/octet-stream response (20 MiB maximum). The source viewer escapes
the text and offers an original .eml download.

POST to the same message's `/seen` or `/unseen` URL with `uid_validity` sets or
clears only the Seen flag. Both require CSRF and verify UIDVALIDITY. The browser
can call `/seen` after opening if the receiving identity's encrypted
`mark_as_read` preference is enabled (default false); manual buttons always offer
the inverse of the current status. Monitoring, rendering and source viewing do
not change flags.

`POST /mailboxes/{account}/messages/{uid}/redirect?uid_validity=...` accepts
`smtp_config_id`, `to`, optional `cc`/`bcc`. It retrieves the original source from
the session's receiving identity, adds Resent-Date/Message-ID/From/To/Cc, strips
Bcc headers, and uses the authorized SMTP identity as the envelope sender.
The original From/body remain preserved. Providers can reject this under their
sender policies. It also accepts the same optional `sent_copy` as normal sending.
Ambiguous submissions are never automatically retried.

Reply/reply-all/forward are prepared as editable drafts and use `/messages/send`;
the optional encrypted IMAP preference `reply_from_recipient` (default false)
selects a configured sender matching the original To, then Cc addresses, ignoring
case. If none matches, it selects the first sender with an address, even if another
sender is linked/default. It never invents a From address or uses From/Reply-To as
the user's sender. Users can change From before sending. Disabled, it retains
linked/default selection; forward/redirect/new messages are unchanged.
Replies include `in_reply_to` (and optionally `references`). Reply-all deduplicates
recipients and excludes the user's sending/receiving addresses. Forwarding fetches
attachments explicitly; partial failures require acknowledgement before sending
without the remaining files. No action sends before the user confirms.

`POST /connections/test` accepts `protocol` (`imap` or `smtp`), `host`, `port`,
`security`, `username`, `password`. Authenticated and CSRF-protected, it verifies
TLS/STARTTLS and provider authentication without submitting mail. Both protocols
share a process-local rate limit of one attempt per source IP per 30 seconds,
including successful/invalid attempts. Extra requests return 429 with Retry-After;
blocked requests do not extend the window. Only configured trusted proxies can
supply the client IP. Errors are generic and credentials are not logged/persisted.

A successful send means SMTP accepted DATA, not proof of delivery to an inbox.
On acceptance, IMAP APPENDs a copy to the mapped Sent folder (see Sent copy
section above). `SMTP_OUTCOME_UNKNOWN` means delivery could not
be confirmed: check the provider before sending again. No send is retried
automatically, including after an error while closing an accepted SMTP session.

Readiness requires the loaded OPAQUE setup. An empty installation automatically
initializes `APP_DATA_DIR/opaque_server_setup` when no explicit
`OPAQUE_SERVER_SETUP` is provided; existing account data with a missing key
requires restoration instead of generation. The
server keeps transient protocol flows in memory for five minutes; a restart
invalidates in-progress registration and login attempts. Unknown logins receive
a fake OPAQUE record to reduce account enumeration.

Cookies are HttpOnly and are never copied to JavaScript. Read the CSRF value from
`GET /api/v1/session` and send it as `X-CSRF-Token` on state changes.

### Password and vault changes

`GET /auth/options` publicly returns `{registration_enabled:boolean}` with no-store.
`REGISTRATION_DISABLED=true` returns 403 `REGISTRATION_DISABLED` on both registration
endpoints, including previously opened forms.

`POST /auth/password/start` accepts `{login,registration_request,rotate_vmk}` after
explicit authenticated `/auth/reauth/start` and `/finish`. The one-use grant and
resulting flow expire after five minutes. The flow snapshots auth, metadata, five
collections and gateway revisions. `POST /auth/password/finish` accepts
`{flow_id,registration_record,vault_metadata,files}`. For rewrap, omit files or send
`{}` and preserve key_version; for rotation, provide config/accounts/identities/
smtp/preferences encrypted under the fresh VMK with incremented key_version.
The request limit is eight times `MAX_REQUEST_BYTES`; individual file limits remain.
All revisions are checked before a durable journal is installed. A successful 204
revokes sessions, pending logins and gateways. A 409 changes nothing. An uncertain
response requires a new sign-in to reconcile which password is active. No password
or plaintext VMK is transmitted.

### Explicit IMAP actions

All routes require the authenticated owner's activated receiving identity and
CSRF, use bounded cancellable connections, and retain UIDVALIDITY checks.

- `POST /mailboxes/:account/messages/:uid/flag?uid_validity=N&folder_id=...` takes
  `{flag:"seen"|"flagged"|"answered",enabled:boolean}`. Unsupported permanent flags
  return 422 `FLAG_UNSUPPORTED`; arbitrary Deleted/keywords are refused.
- `POST /mailboxes/:account/messages/:uid/transfer?...` takes `{destination,move}`
  within that account. COPY is acknowledged before any fallback source deletion;
  moves require MOVE or UIDPLUS. No unrestricted EXPUNGE is used.
- `POST /mailboxes/:account/folders` takes `{action,name,new_name?,confirmation?}`
  for create/rename/delete/subscribe/unsubscribe. INBOX cannot be renamed/deleted.
  Deletion requires the exact name in confirmation and can permanently delete
  provider messages. Folder mappings are not silently changed after rename.

`502 IMAP_ACTION_UNCONFIRMED` means the remote outcome needs inspection; never
retry automatically. Browser batches select at most 50 exact message references,
run sequentially and stop at the first failure, reporting only confirmed results.
Cross-account transfers and arbitrary IMAP keywords are not exposed.

### Optional features and private references

See [OPTIONAL_FEATURES.md](OPTIONAL_FEATURES.md) for limits, storage, provider
configuration and qualification status. Every API response is `no-store`.
All protected writes below require the usual cookie and X-CSRF-Token. Requests
with `lock_revision` must use the revision of the current unlocked browser tab;
the server session's provider-credential state is not proof that a browser VMK
is open. Credentials for optional connectors are posted transiently, never saved
by their handlers. ManageSieve and generic directories share mail connection budgets, deadlines and cancellation. The fixed-provider OAuth/People HTTP broker has its own 16-request bound and 20-second deadlines. Disabled connector routes return 404 CONNECTOR_DISABLED.

| Method and path | Request / result |
| --- | --- |
| POST `/auth/mfa/verify` | Public `{flow_id,code}`; cookie + Session only after success. Five attempts/flow, two-minute expiry. |
| GET `/mfa` | `{available,enabled,recovery_codes_remaining}`. |
| POST `/mfa/enroll` | `{lock_revision}` after explicit one-use OPAQUE reauthentication; `{secret,uri,expires_in:300}`. |
| POST `/mfa/confirm` | `{code}`; confirms session-bound enrollment and returns ten `recovery_codes` once. |
| POST `/mfa/recovery-codes`, DELETE `/mfa` | `{lock_revision,code}` plus fresh explicit reauthentication; regenerate or disable. Other web sessions are revoked. |
| GET `/oauth/options` | Fixed-provider availability and exact callback URLs, no client secrets. |
| POST `/oauth/start` | `{provider,email,lock_revision,contacts?:boolean}`; authorization_url. Contacts consent is Google-only and requires GOOGLE_CONTACTS_ENABLED. |
| GET `/oauth/:provider/callback` | Public state/code callback; redirects to Settings with an opaque result ID, never tokens. |
| POST `/oauth/result` | `{state}`; consumes tokens once for the original session/revision. |
| POST `/oauth/refresh` | `{provider,refresh_token,lock_revision}`; short-lived access token, expiry, optional rotated refresh token and scope. Browser encrypts the result. |
| POST `/oauth/revoke` | Google `{provider,refresh_token,lock_revision}`; 204 on confirmed provider revocation. Microsoft uses local forgetting plus its consent portal. |
| GET `/connectors/options` | `{carddav,ldap,sieve,google_contacts}` administrative booleans. |
| POST `/contacts/external` | `{kind,username,password,lock_revision,...connection}`. CardDAV: exact HTTPS url. LDAP: host/port/security/base_dn/query/optional page_cookie. Result `{contacts,cards,truncated,next_page?}`; card fields href/etag/data. |
| POST `/contacts/google` | `{access_token,page?,lock_revision}`; names/emails and opaque `next_page`. Explicit People API consent required; no server-side refresh. |
| POST `/sieve` | `{host,port,security,username,password,lock_revision,action,name?,script?,expected_hash?,expected_active?}`. Actions list/get/check/put/activate/deactivate/delete. Result scripts/extensions and optional script/hash. Conflicts 409; rejected syntax 422; no automatic mutation retries. |
| POST `/mailboxes/:account/folder-references` | `{name:exact_folder_path}`; session/account-bound opaque references. Use folder_id in URL parameters; never put folder paths in URLs. |
| POST `/mailboxes/:account/messages/search` | Private POST criteria search/seen/from/to/subject/since/until, expression and flagged (string values). Pagination before/uid_validity and opaque folder_id stay in query parameters. Same window contract as GET messages. |
| POST `/mailboxes/:account/messages/references` | `{uids:[1..50],uid_validity,filters:{...}}`, with optional folder_id. Intersects exact validated UIDs with server-side filters, metadata only. |
| GET `/mailboxes/:account/quota` | On-demand QUOTA resources with explicit KiB/items units, or supported:false. |

Credential activation and explicit connection probes accept exactly one of
password (max 4096 bytes) and oauth_token (max 16384 bytes). The optional
sasl_mechanism `SCRAM-SHA-256` applies to password IMAP connections only; omission
retains LOGIN. reply_to remains an optional activated sending identity field.
Secrets are never returned in Session. OAuth/SCRAM accounts are excluded from the
browser gateway configuration; legacy gateway password settings retain their format.

Outgoing messages and remote drafts accept `headers` containing only Organization,
Importance and X-Priority, and optional attachment `content_id` for validated PNG/JPEG
inline images. Normal attachments retain their original format. Message reading
returns content_id in attachment metadata and supported custom headers for drafts.
Configuration CSV import uses the existing encrypted `/vault/import` transaction;
there is no plaintext account CSV endpoint.

Reply threading metadata is exposed as `in_reply_to` and `references` on received
messages. Outgoing messages and explicit IMAP drafts accept the same fields;
References is limited to 32 entries of at most 1000 characters each. The web
composer preserves the conversation root and most recent parents through local
and remote draft reopening. Browser localization changes presentation only:
API error codes and backend response messages keep their existing contracts.
