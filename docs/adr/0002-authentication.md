# ADR 0002: RFC 9807 OPAQUE or no password login

Status: implemented, interoperability verification pending

Password hashes, password-derived bearer secrets, and homemade PAKEs are rejected.
The authentication boundary accepts only messages produced by an RFC 9807
implementation. The backend uses `github.com/bytemare/opaque` v0.18.0 and the
browser uses `@serenity-kit/opaque` v1.1.0. The backend processes serialized
messages as unpadded base64url and holds login state only in a bounded,
expiring in-memory flow map. It verifies KE3 before creating or refreshing
session authentication state. Unknown account login attempts use a fake record.

The Go package is maintained by an RFC author but not independently audited.
The browser implementation is based on opaque-ke and its maintainers report a
7ASecurity penetration test/white-box review. A Go-only RFC-library round trip
is tested. Cross-library interoperability still needs separate end-to-end
verification; no insecure fallback is enabled. The stable server setup is loaded
from `APP_DATA_DIR/opaque_server_setup`, generated once on a new installation, or
from an explicit `OPAQUE_SERVER_SETUP`. Existing files are validated and never
overwritten. Missing setup with existing accounts requires restoring the original
key. The optional `go run ./cmd/opaque-setup` command remains available for
externally managed deployments.
