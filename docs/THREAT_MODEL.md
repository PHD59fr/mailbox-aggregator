# Threat model (STRIDE)

| Threat | Example | Mitigation | Residual risk |
|---|---|---|---|
| Spoofing | stolen session cookie | high-entropy tokens, hashed lookup, expiry, CSRF, TLS | valid cookie works until expiry/revocation |
| Tampering | changed encrypted config | AES-GCM authentication, AAD, strict envelope versions | malicious server JavaScript can steal keys during use |
| Repudiation | disputed deletion | structured opaque-ID lifecycle events are planned | logs are not a non-repudiation system |
| Information disclosure | copied data directory | password-derived KEK, wrapped VMK, independent encrypted files, HMAC login index | index size/timestamps and UUID count leak metadata |
| Information disclosure | compromised browser profile with tab resume enabled | encrypted VMK, separate non-extractable browser key, inactivity expiry, explicit lock clearing; resume can be disabled | a complete profile compromise or same-origin malicious script may use the browser key; not hardware-backed |
| Denial of service | huge payloads or slow upstream | body/file limits, deadlines, rate limit, bounded design | single instance has finite resources |
| Elevation of privilege | user supplies another UUID/path | UUID comes from session, fixed roots, allowlisted names, symlink checks | privileged host administrator remains trusted |
| SSRF | IMAP host points at metadata service | port policy, validate all DNS answers, pinned IP dial, private CIDR admin policy | approved private ranges intentionally expand reach |

Attackers may steal the data directory, modify ciphertext, use an ordinary
account, or control a configured remote mail server. The design does not defend
against a compromised browser/server process, privileged live-memory access,
malicious extensions, compromised providers, or host-level backup retention.
