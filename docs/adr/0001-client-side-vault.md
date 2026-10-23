# ADR 0001: Browser-owned vault encryption

Status: accepted

The server stores independently encrypted JSON envelopes. A random VMK is wrapped
by an Argon2id-derived KEK in the browser; HKDF derives file keys. This prevents a
stolen data directory or server-only persisted material from decrypting account
secrets. It does not protect an unlocked session from compromised served code or
process memory.
