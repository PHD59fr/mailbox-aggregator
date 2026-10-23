# Security policy

Report suspected vulnerabilities privately through the repository's
[security advisory form](https://github.com/PHD59fr/mailbox-aggregator/security/advisories/new).
Do not include live credentials, session cookies, OPAQUE setup material or private
mail in a public issue. Supply an affected revision, synthetic reproduction and
the impact you observed. Use the same private channel to coordinate disclosure.

The current development branch is the supported target for fixes. Historical
versions have no promised backport or response-time commitment. Follow the
changelog and test an isolated restored backup before upgrading. Pin deployed
images to their digest and retain the previous binary/image with its matching
data snapshot for rollback; do not mix credential generations from snapshots.

This project has not completed an independent cryptographic audit or production
qualification. See [the threat model](docs/SECURITY.md),
[deployment and restore runbook](docs/DEPLOYMENT.md), and [roadmap](roadmap.md).
