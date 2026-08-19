# Leo-One — Agent Windows

Le programme (`leo-agent.exe`) qui tourne sur chaque poste Windows à
superviser : collecte des métriques (CPU/RAM/disque/réseau), inventaire
matériel/logiciel, exécution de scripts et de commandes à distance
(installation de paquets, redémarrage), connexion sécurisée (mTLS) au
serveur Leo-One.

- **Binaire fourni** : `leo-agent.exe` — 64 bits, Windows 7 ou plus récent.
- **Compilé depuis** : `agent/` à la racine du dépôt (cross-compilé avec
  mingw-w64). Pour recompiler vous-même après une modification du code,
  voir `agent/CMakeLists.txt` et `agent/cmake/mingw-toolchain.cmake`.

## Installation

**1. Copier les fichiers.** Sur le poste cible, créez le dossier de
configuration et placez-y l'exécutable :

```powershell
mkdir C:\ProgramData\LeoOne
copy leo-agent.exe C:\ProgramData\LeoOne\leo-agent.exe
```

**2. Générer un token d'enrollment.** Dans l'interface d'administration
Leo-One (voir `release/server/`) : **Machines → Enrôler un agent → Générer
un token**. Le token est à usage unique et de courte durée.

**3. Configurer l'enrollment.** Copiez `agent_bootstrap.conf.example` vers
`C:\ProgramData\LeoOne\agent_bootstrap.conf` et éditez-le :

```
enrollment_token=<le token généré à l'étape 2>
api_endpoint=https://votre-serveur-leo-one.example.com
```

**4. Premier lancement (test).** Ouvrez une invite de commandes et lancez :

```powershell
C:\ProgramData\LeoOne\leo-agent.exe
```

L'agent s'enregistre auprès du serveur, reçoit son certificat client, et se
connecte. Les logs s'écrivent dans `C:\ProgramData\LeoOne\logs\agent.log`.
Vérifiez dans l'interface (onglet Machines) que le poste apparaît. `Ctrl+C`
pour arrêter ce test.

**5. Installer comme service Windows** (pour qu'il démarre automatiquement,
y compris avant toute session utilisateur) — depuis une invite PowerShell
**en administrateur** :

```powershell
sc create leo-agent binPath= "C:\ProgramData\LeoOne\leo-agent.exe" start=auto DisplayName= "Leo-One RMM Agent"
sc start leo-agent
```

Vérifier :

```powershell
sc query leo-agent
```

## Désinstaller

```powershell
sc stop leo-agent
sc delete leo-agent
rmdir /s C:\ProgramData\LeoOne
```

## Notes

- **Droits** : exécuté comme service (compte SYSTEM par défaut via `sc
  create`), l'agent peut installer des paquets et redémarrer la machine à la
  demande de l'administrateur Leo-One. Ne l'installez que sur des postes que
  vous administrez réellement.
- **Pare-feu** : l'agent initie toujours la connexion vers le serveur
  (sortant uniquement) — aucun port entrant à ouvrir sur le poste client.
- **Aucune dépendance runtime** : `leo-agent.exe` est lié statiquement
  (OpenSSL, libjpeg-turbo, runtime pthread mingw) — pas de Visual C++
  Redistributable ni de DLL tierce à installer, `leo-agent.exe` seul suffit.
- Si l'agent ne se connecte pas, vérifiez d'abord
  `C:\ProgramData\LeoOne\logs\agent.log` — l'erreur la plus fréquente est un
  `api_endpoint` incorrect ou un token déjà expiré/utilisé (regénérez-en un).
