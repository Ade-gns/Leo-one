# Leo-One — Packages prêts à l'emploi

Trois composants, chacun dans son dossier avec son propre `README.md` :

| Dossier | Contenu | À installer sur |
|---|---|---|
| [`server/`](server/README.md) | Backend + interface web + base de données (via Docker) | Votre serveur (VPS, machine dédiée, cloud) |
| [`windows-client/`](windows-client/README.md) | `leo-agent.exe` précompilé | Chaque poste Windows à superviser |
| [`linux-client/`](linux-client/README.md) | `leo-agent` précompilé | Chaque machine Linux à superviser |

## Ordre d'installation

1. **Déployez le serveur en premier** ([`server/README.md`](server/README.md))
   — l'agent a besoin d'un serveur joignable et d'un token d'enrollment pour
   fonctionner.
2. **Installez l'agent** sur chaque poste à superviser, Windows ou Linux
   selon le cas ([`windows-client/README.md`](windows-client/README.md) /
   [`linux-client/README.md`](linux-client/README.md)).

## Binaires précompilés

`windows-client/leo-agent.exe` et `linux-client/leo-agent` sont compilés
depuis le code source de `agent/` à la racine du dépôt, qui reste la
référence. Si vous préférez recompiler vous-même (après une modification du
code, ou pour vérifier la provenance du binaire), voir les instructions de
build dans le `README.md` de chaque dossier.
