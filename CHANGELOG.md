# Changelog

## Unreleased — 24 October 2026

- Replace the webmail SMTP transport with the maintained go-smtp client; test
  TLS/STARTTLS, PLAIN/LOGIN, missing TLS, absolute deadlines and uncertain DATA.
- Add current-password OPAQUE reauthentication, transactional password changes
  and optional VMK rotation, owner-session revocation and gateway disabling.
  Migrate browser drafts with encrypted, expiring prior-key history.
- Add read-only `--check-data` and an isolated real-client backup/restore exercise.
- Run Argon2 in a cancellable Worker; split lazy routes/dependencies, enforce a
  500 kB JavaScript chunk budget and compare two production builds in CI.
- Add `REGISTRATION_DISABLED`, configuration API/UI handling and regression tests.
- Add Seen/Flagged/Answered selection batches, same-account COPY/MOVE and explicit
  folder create/rename/delete/subscription actions with partial-result feedback.
- Disable core dumps/container swap in Compose; document memory limits, a proxy
  streaming reference and private vulnerability disclosure.

- Preserve other mail accounts during concurrent targeted credential activation;
  expose per-account failures and the configured identity limit to the browser.
- Separate monitoring and interactive connection budgets, including Sent copies.
- Apply image permission mutations to fresh preferences and retry bounded
  revision conflicts without restoring remotely revoked permissions.
- Encrypt local attachment descriptors, migrate legacy drafts after unlocking,
  and prevent edits/duplicate submission while an earlier send is pending.
- Export complete encrypted configurations, support larger exports and reject
  incomplete reads. Import validated collections as a grouped revision-guarded
  commit with encrypted crash recovery; revoke the owner's sessions afterwards.
- Fold MIME subjects, recipient lists, private Sent headers and generated Resent
  headers without changing their decoded values; reject unbreakable invalid tokens.
- Replace the placeholder non-persistence test with authenticated HTTP and real
  TLS IMAP/SMTP flows, process crash/restart and fail-closed scans of controlled
  supports. Add real Chromium IndexedDB regression checks to CI.

Reload existing browser tabs after updating. The browser uses activation mode
`merge` and the new `/vault/import` endpoint. Existing callers without an
activation mode retain group replacement semantics. Configuration imports sign
out all sessions of their owner. Back up the complete data directory; retain any
pending `.vault-import.json` for automatic recovery.

Independent crypto/security review, production deployment qualification, real
provider matrices, OAuth and IMAP Drafts remain planned work.
