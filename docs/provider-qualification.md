# Qualification des fournisseurs

L’implémentation locale est distincte d’une compatibilité certifiée avec un
fournisseur réel. Aucun compte réel ni application OAuth n’a été fourni pour ces
essais. Conserver `D3` ouvert dans [todo.md](../todo.md).

| Parcours | Preuve locale | Essai réel restant |
| --- | --- | --- |
| IMAP/SMTP par mot de passe | TLS/STARTTLS, LOGIN/PLAIN existants, lecture/actions/envoi, leases et verrouillage ; Go et tests frontend | Fournisseurs habituels, limites et politiques propres |
| Gmail OAuth | PKCE, état/session/révision, callback sans cookie cross-site, rotation simulée ; XOAUTH2 IMAP/SMTP local | Application enregistrée, consentement/validation Google, lecture/envoi, expiration, déconnexion et révocation |
| Outlook.com OAuth | Broker Microsoft à endpoints fixes ; dialogue XOAUTH2 local | Compte personnel, scopes, lecture/envoi et consentement révoqué |
| Microsoft 365 OAuth | Même broker ; profil distinct explicite | Tenant choisi, consentement administrateur, IMAP/SMTP AUTH désactivés, politique MFA fournisseur |
| SCRAM-SHA-256 IMAP | Dialogue TLS simulé, preuve serveur, absence de repli LOGIN, bornes PBKDF2 | Serveur annonçant réellement le mécanisme, identifiants Unicode et politique locale |
| QUOTA | Ressources KiB/items, non-support normal, réponse fournisseur excessive refusée | Racines et ressources propres au fournisseur ; aucune modification du quota |
| ManageSieve | TLS et STARTTLS avec nouvelles capacités, lecture/validation/écriture, conflit, réponse perdue, limites de parseur | Syntaxe/actions/extensions réelles, script actif, dossier Screen, exécution navigateur fermé chez le fournisseur |
| CardDAV | REPORT HTTPS, certificat vérifié, refus des redirections et destinations privées, XML borné | URL exacte de collection, droits, vCard réelles, dépassement de limites ; pas de sync bidirectionnelle |
| LDAP | Bind TLS et filtre échappé, attributs fixes, contrôle de pagination et cookie opaque | Annuaire choisi, STARTTLS, CA déployée, base DN, pages successives, limites administrateur et erreurs d’accès |
| Google contacts | Scope readonly distinct, endpoint People fixe, une page explicite, refus après verrouillage | People API activée, consentement, plusieurs pages, absence de scope et révocation |
| Brouillons IMAP et transferts | MIME/pièces jointes/Bcc/Content-ID, UIDVALIDITY, remplacement, UIDPLUS absent, APPEND appliqué mais réponse perdue | Deux clients/appareils, spécificités Drafts/Sent, destinations indisponibles et politiques de taille |

La qualification réelle doit employer des messages de test et des comptes choisis
par le propriétaire. Vérifier avant chaque changement distant le dossier/compte
cible et l’état du script actif. Ne jamais répéter automatiquement un APPEND,
transfert, envoi ou changement Sieve dont la réponse est inconnue.

Le test isolé avec le vrai serveur Go et la crypto du client valide OPAQUE,
révisions de verrouillage, import groupé, suppression, changement de mot de passe,
rotation VMK, TOTP, codes de secours et restauration d’une sauvegarde. Il ne
contacte aucun de ces fournisseurs et ne vaut pas audit de sécurité indépendant.
