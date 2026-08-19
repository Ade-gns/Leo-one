# Leo-One — Agent Linux

Le programme (`leo-agent`) qui tourne sur chaque machine Linux à superviser :
collecte des métriques (CPU/RAM/disque/réseau), inventaire matériel/logiciel,
exécution de scripts et de commandes à distance (mise à jour des paquets via
apt ou dnf selon la distribution, redémarrage), connexion sécurisée (mTLS)
au serveur Leo-One.

- **Binaire fourni** : `leo-agent` — x86-64, glibc (Ubuntu/Debian et
  dérivés ; compilé et testé sur Ubuntu).
- **Compilé depuis** : `agent/` à la racine du dépôt. Pour recompiler
  vous-même après une modification du code : `cmake -B build && cmake
  --build build` depuis `agent/` (voir `agent/CMakeLists.txt`).

## Prérequis

Le binaire est dynamiquement lié à quelques bibliothèques système —
absentes par défaut sur une installation serveur minimale (contrairement à
un poste de bureau, où elles sont déjà présentes). Sans elles, `leo-agent`
refuse de démarrer (erreur de l'éditeur de liens dynamique).

```bash
sudo apt install -y libx11-6 libxext6 libxtst6 libturbojpeg0
```

`libx11-6`/`libxext6`/`libxtst6` sont nécessaires même sur une machine sans
écran/serveur X actif — elles ne servent qu'à la fonctionnalité bureau à
distance, mais le binaire ne démarre pas sans elles (dépendance au chargement,
pas seulement à l'usage). L'agent est aussi lié à `libssl.so.3` (OpenSSL) —
non listée ci-dessus volontairement : son paquet exact varie selon la
version d'Ubuntu/Debian (`libssl3`, `libssl3t64`…) mais elle est quasi
systématiquement déjà présente, tirée par d'autres paquets du système
(curl, ssh…). Si le démarrage échoue quand même sur `libssl.so.3:
impossible d'ouvrir le fichier objet partagé`, installez le paquet OpenSSL
correspondant à votre distribution (`apt search libssl3` pour le nom exact).

## Installation

**1. Copier le binaire.**

```bash
sudo mkdir -p /opt/leo-one/agent /opt/leo-one/logs
sudo cp leo-agent /opt/leo-one/agent/leo-agent
sudo chmod +x /opt/leo-one/agent/leo-agent
```

`/opt/leo-one/logs` doit exister avant le premier lancement — sans lui,
l'agent ne peut pas créer `agent.log` et se rabat silencieusement sur
stderr (visible seulement en mode console, voir étape 4 ; capturé par
`journalctl` en service, voir étape 5). `/opt/leo-one/certs` n'a pas besoin
d'être créé à l'avance : l'agent le crée lui-même à l'enrollment.

**2. Générer un token d'enrollment.** Dans l'interface d'administration
Leo-One (voir `release/server/`) : **Machines → Enrôler un agent → Générer
un token**. Le token est à usage unique et de courte durée.

**3. Configurer l'enrollment.**

```bash
sudo cp agent_bootstrap.conf.example /opt/leo-one/agent_bootstrap.conf
sudo chmod 600 /opt/leo-one/agent_bootstrap.conf
sudo nano /opt/leo-one/agent_bootstrap.conf   # renseigner enrollment_token et api_endpoint
```

**4. Premier lancement (test).**

```bash
sudo /opt/leo-one/agent/leo-agent
```

L'agent s'enregistre auprès du serveur, reçoit son certificat client, et se
connecte. Les logs s'écrivent dans `/opt/leo-one/logs/agent.log`. Vérifiez
dans l'interface (onglet Machines) que la machine apparaît. `Ctrl+C` pour
arrêter ce test.

**5. Installer comme service systemd** (démarrage automatique au boot,
redémarrage automatique en cas de crash) :

```bash
sudo cp leo-agent.service.example /etc/systemd/system/leo-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now leo-agent
```

Vérifier :

```bash
sudo systemctl status leo-agent
journalctl -u leo-agent -f
```

## Désinstaller

```bash
sudo systemctl disable --now leo-agent
sudo rm /etc/systemd/system/leo-agent.service
sudo systemctl daemon-reload
sudo rm -rf /opt/leo-one
```

## Notes

- **Droits** : l'agent tourne en root (`User=root` dans l'unité systemd) —
  nécessaire pour installer des paquets, redémarrer la machine, et lire
  certaines informations matérielles (`/sys/class/dmi/id/product_serial`).
  Ne l'installez que sur des machines que vous administrez réellement.
- **Pare-feu** : l'agent initie toujours la connexion vers le serveur
  (sortant uniquement) — aucun port entrant à ouvrir.
- **Gestion des paquets** : la gestion des mises à jour (patch management)
  détecte automatiquement `apt` (Debian/Ubuntu) ou `dnf` (Fedora/RHEL) sur la
  machine. L'inventaire logiciel, lui, ne lit que `dpkg-query` pour l'instant
  (Debian/Ubuntu uniquement) — vide sur une distribution dnf. Le reste
  (métriques, inventaire matériel, scripts) fonctionne sur toute distribution
  Linux glibc.
- **Bureau à distance sur serveur headless** : capture X11 uniquement
  (Wayland non supporté). Un serveur sans session graphique active peut
  installer l'agent normalement (les autres fonctionnalités marchent), mais
  une tentative de bureau à distance échouera faute de display à capturer.
- Si l'agent ne se connecte pas, vérifiez d'abord
  `/opt/leo-one/logs/agent.log` — l'erreur la plus fréquente est un
  `api_endpoint` incorrect ou un token déjà expiré/utilisé (regénérez-en un).
