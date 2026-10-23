# Exécution du backlog — 25–26 octobre 2026

Implémentation locale réalisée à partir de la révision
`1d9147742aeebfa4dc4af49ef2d4ff0d4c78ade3`. Les modifications déjà présentes ont
été conservées, notamment les références privées de dossiers, les contacts et les
corrections de verrouillage. Aucun déploiement, redémarrage du service existant
ou changement sur un fournisseur réel n’a été effectué.

## Fonctions ajoutées ou finalisées

- Sources et pagination séparées par compte/dossier, clés incluant UIDVALIDITY,
  concurrence bornée ; vues Non lus, Envoyés, Brouillons, Favoris, Tout, dossiers
  choisis et périmètre de recherche. L’INBOX agrégée reste la vue initiale.
- Recherche AND/OR/NOT/groupes, recherches sauvegardées, historique opt-in et
  alertes limitées à la vue visible déverrouillée ; surlignage de texte échappé.
- Tags, snooze applicatif et visites récentes opt-in : références chiffrées,
  requêtes exactes par UID, limites et suppression des références périmées.
- Quotas à la demande, profils fournisseur modifiables, densité/disposition,
  couleurs par compte, raccourcis opt-in, recherche dans les paramètres et
  choix bornés d’accent/taille de texte, sans CSS arbitraire.
- Modèles chiffrés, glisser-déposer, transfert du MIME original en pièce jointe,
  auto-Bcc visible, suggestions/collecte To/Cc opt-in après envoi confirmé,
  images PNG/JPEG inline et trois en-têtes sortants autorisés.
- Signatures texte et Reply-To conservés et validés. Les brouillons chiffrés et
  IMAP préservent les pièces jointes, Content-ID et en-têtes pris en charge.
- OAuth Google/Microsoft : Authorization Code + PKCE, état/session/révision,
  prise du résultat par POST, tokens chiffrés dans le coffre et renouvellement
  à la demande ; XOAUTH2 IMAP/SMTP. SCRAM-SHA-256 IMAP explicite sans repli LOGIN.
- TOTP optionnel après OPAQUE, état intermédiaire sans cookie de session,
  codes de secours à usage unique, vérificateur `mfa.json` chiffré séparément,
  réauthentification explicite et révocation des autres sessions web.
- ManageSieve optionnel : TLS/STARTTLS, scripts, validation, activation explicite,
  sauvegardes chiffrées, éditeur ALL/ANY, ordre/stop, règles depuis un message,
  listes Like/Screen, dossier explicite et simulation limitée aux en-têtes.
- CardDAV, LDAP paginé et People API Google en lecture seule, sur demande,
  prévisualisation séparée et import explicite ; flags serveur désactivés par
  défaut et aucune ouverture de port implicite.
- Import CSV de comptes avec aperçu/limites/doublons, chiffrement navigateur,
  transaction existante et confirmation de la déconnexion des sessions.
- Indices BEC explicables ; cadrage MTA-STS/TLS-RPT comme responsabilité du
  fournisseur. API/OpenAPI, exploitation, sécurité et limites documentées.

## Validations exécutées

- `go test -race ./...` réussi ; après les derniers ajouts IMAP/LDAP, tests ciblés
  avec `-race` réussis sur ces packages. `go vet ./...` réussi.
- **384 tests frontend réussis, 60 fichiers** avec Vitest 4.1.12.
- Lint et TypeScript réussis ; build Vite 7.3.7 réussi. Le build reproductible et
  les scénarios Chromium sont contrôlés séparément sur les assets de production.
- Tests Chromium : brouillons et pièces jointes chiffrés, migration de VMK,
  concurrence, Argon2 hors du fil principal, reprise de navigation après chunk
  manquant, absence de boucle et navigation mobile.
- Crypto client réelle contre un serveur Go isolé : OPAQUE, révisions de lock,
  import groupé, suppression ; changement du mot de passe, rotation VMK,
  révocation, cinq collections, enrôlement TOTP sans credentials mail activés,
  login protégé par code de secours et restauration d’une sauvegarde avec MFA.
- Transports simulés : IMAP XOAUTH2/SCRAM sans LOGIN de repli, SMTP XOAUTH2 en
  TLS/STARTTLS, Sieve TLS/STARTTLS avec capacités rafraîchies, CardDAV HTTPS et
  refus des redirections/certificats non fiables, LDAP TLS/filtres échappés/cookie
  de pagination, QUOTA et réponse trop volumineuse.
- MIME : octets exacts des pièces jointes, groupe multipart/related, Content-ID,
  assainissement, Bcc privé et Reply-To ; sauvegarde/reprise IMAP des images et
  en-têtes. Des APPEND appliqués avec réponse perdue ne sont ni répétés ni suivis
  d’une suppression de la source ou de l’ancien brouillon.
- Parser OpenAPI validé, **286 références internes résolues**.
- Outillage de développement corrigé : Vite 7.3.7, Vitest 4.1.12 ; installation
  npm signalant **zéro vulnérabilité**. Tiptap Core est déclaré explicitement,
  sans charger l’éditeur dans le parcours de login.

Les assets du dernier build sont synchronisés dans `internal/webui/dist`. Le parcours CSV a aussi un test de confirmation, de confidentialité de l’aperçu et de retour au login après import.

Les tests utilisent des processus et données temporaires. Les tests de transport
ouvrent des sockets locaux et nécessitent une exécution hors du bac à sable.
Ils n’établissent pas la compatibilité d’un fournisseur réel ni un audit indépendant.

## Compléments après revue du 26 octobre

- `References` est lu avec un budget de 32 Kio, dédupliqué et borné à 32 identifiants.
  Réponses et brouillons locaux/IMAP le conservent jusqu’à l’envoi, avec le parent.
- Le sélecteur de destinataires propose Google à la demande, sans import local
  obligatoire. Déduplication, budget commun avec les contacts locaux, scope distinct
  et annulation/ignorance des résultats après verrouillage sont couverts.
- Catalogues anglais/français (anglais par défaut), préférence chiffrée du profil,
  réglages dans les paramètres utilisateur, dates/nombres/pluriels et direction auto/LTR/RTL.
  Le contrôle `npm run test:i18n` vérifie catalogues, paramètres et extraction
  statique et fait partie de la CI. Les contenus et identifiants restent inchangés.
- Alerte serveur WARN `mfa_failed_attempts` après cinq échecs MFA par IP/minute,
  une émission par fenêtre, mémoire bornée, sans codes ni challenge dans les logs.
- Validation du complément : **401 tests frontend dans 64 fichiers**, tests Go
  complets avec `-race`, lint et TypeScript réussis. 67 tests ciblés supplémentaires ont réussi après les derniers ajustements.
  Le build reproductible contient 29 fichiers, tous les chunks JS sont sous 500 ko.
  Chromium a vérifié brouillons/pièces jointes/références chiffrés, migration de
  VMK, concurrence, Worker Argon2, reprise de chunk, anglais/français, RTL mobile,
  ordre clavier et persistance de langue. `go vet ./...` a réussi. Les assets
  finaux sont synchronisés dans `internal/webui/dist`.

## Placement des préférences utilisateur

Affichage/clavier, étiquettes/report/historique et configuration des recherches et
vues combinées sont regroupés dans les paramètres utilisateur. Les actions de
recherche restent dans les filtres de la boîte de réception ; les alertes
continuent de fonctionner lorsque ces filtres sont fermés. Le chargement des
préférences d’affichage est indépendant de leur formulaire.

Les sélecteurs de langue/direction sont uniquement dans Apparence. Login et
inscription réutilisent la dernière langue connue sans proposer de configuration.

Validation de ce déplacement : **57 tests ciblés dans huit fichiers** réussis,
lint/TypeScript et contrôle des catalogues réussis. Build reproductible (29
fichiers, chunks JS sous 500 ko) ; Chromium vérifie login/inscription sans
paramètres, langue mémorisée, RTL/mobile, parcours clavier et brouillons chiffrés.
Les assets embarqués sont synchronisés, sans déploiement ni redémarrage.

## Restes et limites

[OPTIONAL_FEATURES.md](OPTIONAL_FEATURES.md) décrit les contrats, limites,
paramètres d’activation et données stockées. La [matrice des fournisseurs](provider-qualification.md)
distingue les preuves locales des essais réels encore nécessaires.

- Les développements de la [revue du 26 octobre](remaining-work.md) sont réalisés
  localement ; les essais externes restent distincts.
- D3 reste ouvert : applications OAuth et comptes réels, politiques administrateur,
  interopérabilité et essais sur deux appareils.
- Anglais/français et direction RTL sont disponibles ; aucune autre traduction
  n’est annoncée. Le CAPTCHA reste différé, sans script tiers ni exception CSP.
- La revue de sécurité indépendante et les essais d’accessibilité élargis restent
  à faire. Les passerelles gardent leur fonctionnement indépendant par mot de
  passe ; elles ne sont pas protégées par TOTP et n’acceptent pas les nouveaux
  profils OAuth/SCRAM depuis l’interface.
- Les alertes MFA sont des événements des logs serveur. L’intégration au système
  de supervision et la rétention des logs appartiennent à l’exploitation.
- Les vues fusionnent des fenêtres chargées, sans index persistant ni instantané
  global. Tags/snooze ne déplacent pas les mails. Historique et alertes n’ajoutent
  aucun traitement autonome de messagerie navigateur fermé.
- ManageSieve ne fournit pas de CAS atomique ; la comparaison de hash détecte les
  changements précédents, pas une écriture strictement simultanée. La simulation
  des règles graphiques n’exécute pas les scripts personnalisés.

PHP/Composer, EWS et envoi programmé restent exclus. Les autres chantiers
explicitement différés dans [todo.md](../todo.md) restent hors périmètre.
