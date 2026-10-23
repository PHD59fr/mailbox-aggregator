# État après traitement du reste — 26 octobre 2026

Les quatre développements identifiés lors de la revue de `diff.md` et du code
ont été traités localement. Les tests et limites sont suivis dans
[todo-progress.md](todo-progress.md) et [todo.md](../todo.md).
Aucun déploiement ni essai sur un compte fournisseur réel n’a été effectué.

| Chantier | Résultat local | Preuves principales |
| --- | --- | --- |
| Réponses et brouillons : `References` | Lecture bornée, déduplication, parent ajouté aux réponses, conservation de la racine et des parents récents (32 références), reprise des brouillons locaux/IMAP et envoi. | [Lecture IMAP](../internal/imap/reader.go), [préparation des réponses](../web/src/api/mailActions.ts), [composeur](../web/src/views/ComposeView.vue), [brouillons chiffrés](../web/src/crypto/composeDraft.ts), [API des brouillons](../internal/api/drafts.go), tests de reprise et de transport. |
| EXT-03 : destinataires Google | Sélection à la demande dans le composeur, sans import obligatoire dans le carnet local, avec consentement distinct, pagination explicite, déduplication et budget partagé de destinataires. Résultats tardifs rejetés après verrouillage. | [Sélecteur](../web/src/components/ContactPicker.vue), [GoogleContacts](../web/src/components/GoogleContacts.vue), [client lié à la session](../web/src/api/googleContacts.ts), tests de sélection et d’annulation. |
| UX-03 : traductions | Catalogues anglais/français, anglais par défaut, préférence chiffrée par profil, réglée uniquement dans les paramètres utilisateur. Libellés, aides, erreurs API, formats locaux et pluriels. | [Internationalisation](../web/src/i18n/index.ts), [préférences](../web/src/i18n/preferences.ts), [réglages](../web/src/components/LanguageSettings.vue), contrôle `npm run test:i18n` en CI. |
| UX-04 : RTL | Direction auto/LTR/RTL, disposition et espacements logiques, adresses isolées et direction propre au texte des messages. | [Styles](../web/src/styles.css), [lecteur](../web/src/components/MessageReader.vue), tests de changement de direction et parcours Chromium. |

Le contenu des mails, les noms de contacts/dossiers définis par l’utilisateur,
les commandes Sieve et les identifiants API ne sont pas traduits. Les diagnostics
externes inconnus restent affichés tels quels. Les deux catalogues fournis sont
anglais et français ; aucune traduction arabe n’est annoncée. En mode automatique,
ces deux langues utilisent LTR ; le réglage RTL est explicite.

## Alertes MFA

Le complément de l’EPIC H est traité par une **alerte d’exploitation dans les
logs serveur** : événement WARN `mfa_failed_attempts` au cinquième échec de
vérification MFA par IP dans une fenêtre de 60 secondes, une émission par fenêtre.
L’agrégation en RAM est bornée à 4096 fenêtres ; les fenêtres expirées sont
purgées à la requête suivante. Aucun code, challenge, corps de requête ou identifiant
utilisateur n’est journalisé par cet événement. Aucun envoi de mail ni accès
supplémentaire à des secrets fournisseur n’est ajouté. Le raccordement à une
supervision et la rétention des logs restent du ressort de l’exploitant.

## Ce qui reste à qualifier

- Applications et comptes Google/Microsoft réels : consentement, politiques
  administrateur, lecture/envoi, expiration, renouvellement et révocation.
- Serveurs retenus pour Sieve, SCRAM, QUOTA, CardDAV et LDAP : capacités,
  certificats, pagination, limites et comportements réels. Sieve est exécuté par
  le fournisseur, y compris navigateur fermé.
- Deux appareils/clients : reprise des brouillons, verrouillage, renouvellements
  OAuth concurrents et modifications concurrentes.
- Revue de sécurité indépendante et accessibilité élargie : lecteur d’écran,
  contraste, langues bidirectionnelles et appareils mobiles réels.

Les scénarios sont dans la [matrice des fournisseurs](provider-qualification.md).
Les tests locaux ne permettent pas de cocher ces qualifications externes.

## Décisions conservées

Les versions bornées décrites dans [OPTIONAL_FEATURES.md](OPTIONAL_FEATURES.md)
restent le périmètre prévu : profils fournisseurs statiques, CardDAV en lecture
avec import explicite, simulation Sieve limitée aux en-têtes, signatures texte,
images inline PNG/JPEG, fenêtres de résultats chargées et alertes de recherche
pendant la session ouverte. Les passerelles gardent leur authentification
indépendante par mot de passe.

JMAP, SSO/login externe, OpenPGP, interface HTML alternative, plugins, sessions
distribuées, calendrier, flux et autres sources non-mail restent différés, de
même que CAPTCHA et CSS arbitraire. **PHP/Composer, EWS et envoi programmé
restent exclus.**
