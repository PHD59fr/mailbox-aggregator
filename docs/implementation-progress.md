# Travaux complémentaires — 24 octobre 2026

Travaux locaux effectués à la demande du propriétaire. Aucun compte réel ni déploiement existant n’a été modifié. La roadmap complète reste ouverte : les cases non couvertes sont conservées.

| Chantier | Résultat livré | Preuve |
| --- | --- | --- |
| MA-002 — SMTP | Client go-smtp épinglé, TLS/STARTTLS, PLAIN/LOGIN, délai absolu incluant DNS/connexion et commandes, aucun retry ambigu ; acceptation DATA distincte de QUIT et Sent | Tests transport, API, passerelles et scénario de non-persistance |
| MA-004 — Mot de passe / VMK | Réauthentification OPAQUE explicite à usage unique ; rewrap client ou nouvelle VMK + cinq collections rechiffrées ; transaction de huit fichiers avec révisions | `internal/auth/password_test.go`, `internal/filestore/credentials_test.go`, `tests/account-lifecycle.{ts,mjs}` |
| Révocation après rotation | Sessions, flux login antérieurs, secrets activés et passerelles invalidés ; écritures anciennes refusées sous le verrou des fichiers | Tests de génération d’authentification, requêtes avec l’ancien cookie et ancien mot de passe |
| Brouillons après rotation | Migration des métadonnées et pièces jointes IndexedDB ; anciennes clés conservées sept jours dans la nouvelle config chiffrée pour les autres appareils ; aucun effacement sur erreur | Chromium réel ; migration conditionnelle protégeant les autosaves concurrentes |
| A03 — Worker Argon2 | Worker dédié, terminaison au lock, paramètres inchangés et bornés ; repli async coopératif hors Worker | Chromium : équivalence avec la référence, événements UI pendant calcul, annulation et redémarrage ; appareils mobiles réels encore à mesurer |
| A05 — Inscriptions | `REGISTRATION_DISABLED` côté serveur et UI, options publiques sans cache, refus des deux endpoints | Tests config/API et interface ; invitations non implémentées |
| A02 — Bundles | Routes lazy et chunks séparés ; aucun JS > 500 kB | Deux builds comparés SHA-256, 21 fichiers identiques ; contrôle ajouté en CI |
| A06 — Sauvegarde / restauration | `--check-data` sans création, permission modifiée ni listener ; inspection setup OPAQUE/index/auth/coffres/passerelles/journaux ; refus des états incohérents | Copie d’instance arrêtée, vérification non mutante puis nouvelle instance et déchiffrement réel des cinq collections ; négatifs clé absente, permissions ouvertes, setup corrompu |
| MA-101 — Flags / lots | Seen/Flagged/Answered via PERMANENTFLAGS ; sélection explicite jusqu’à 50 messages ; arrêt au premier échec, résultats confirmés comptés ; annulation sur lock/changement de boîte | Tests TLS IMAP, API CSRF/isolation, tests du composant de sélection |
| MA-102 partiel — Dossiers | COPY/MOVE intra-compte, CREATE/RENAME/DELETE/SUBSCRIBE/UNSUBSCRIBE ; nom exact exigé pour supprimer un dossier ; INBOX protégé | UIDVALIDITY, Unicode/hiérarchie, MOVE et repli UIDPLUS, copie échouée sans suppression de source ni EXPUNGE global |
| Exploitation | Compose : limite mémoire, swap interdit au conteneur, core dumps désactivés ; référence nginx sans spooling ; runbook et politique de divulgation | Deux configurations Compose parsées, documentation API/OpenAPI/sécurité et CI actualisée |

## Validation effectuée

- Suite Go complète avec `-race -count=1`, puis tests ciblés après les derniers changements ; `go vet ./...`.
- Lint et TypeScript, 287 tests frontend complets dans 40 fichiers puis ajout/vérification d’un quatrième test de sélection et d’un test de migration partielle des brouillons (289 au total).
- Chromium réel : IndexedDB, pièces jointes, rotation, concurrence et Worker Argon2.
- Interop JS/Go OPAQUE : inscription/login, lock/révisions, import groupé/révocation/suppression ; mot de passe, rotation et restauration avec le vrai chiffrement frontend.
- Reprise du journal de rotation simulée à chaque point entre 0 et 8 fichiers remplacés ; conflit sans commit partiel, corruption refusée.
- Build de production reproductible, budget JS, interface embarquée synchronisée ; OpenAPI parsé, Compose de base et avec passerelles parsés, `git diff --check` et gofmt propres.

## Travaux toujours ouverts

La copie inter-comptes, les vues unifiées supplémentaires et leur pagination globale, Drafts IMAP, OAuth Gmail/Microsoft, signatures et contacts, ainsi que les fonctions avancées P2 n’ont pas été implémentés par ce lot. Le formatage/découpage général du frontend et les scénarios E2E visuels/multi-onglets/mobile restent à poursuivre.

La qualification de tous les fournisseurs réels, l’audit cryptographique **indépendant**, la matrice complète d’isolation/charge, les scans/SBOM/signatures de livraison et l’audit du proxy/volumes/swap réellement déployés ne sont pas acquis. Les tests utilisent des fournisseurs synthétiques et des instances isolées ; les fichiers de configuration fournis ne certifient pas un hôte existant. Les extensions P3 demeurent facultatives.

## Additive backlog execution — 2026-10-25

The current local implementation and explicit qualification limits are tracked in
[todo-progress.md](todo-progress.md), [todo.md](../todo.md) and
[OPTIONAL_FEATURES.md](OPTIONAL_FEATURES.md). They supersede older feature-gap
statements in this document; real-provider qualification is separate from local
implementation. No PHP/Composer, scheduled sending, EWS or autonomous provider
worker is included.
