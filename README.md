# KBRD Agent

Agent desktop autonome utilisé par KBRD pour découvrir, lancer et fermer les
applications de la session utilisateur. La première implémentation cible macOS;
l'interface `internal/application.Service` permettra d'ajouter Windows.

## Développement

Go 1.22 ou plus récent est requis uniquement pour compiler et tester :

```sh
go test ./...
go run ./cmd/kbrd-agent --api-url http://kbrd.local:8081
```

L'agent écoute par défaut sur le port `8090` et se ré-enregistre toutes les dix
secondes auprès de KBRD-API. Les variables `KBRD_API_URL`, `KBRD_AGENT_HOST`,
`KBRD_AGENT_PORT` et `KBRD_AGENT_NAME` peuvent remplacer les valeurs par défaut.

## Compiler depuis Linux ou une VM

```sh
make build-macos MACOS_ARCH=arm64
```

Le résultat est `dist/KBRD Agent.app`. Utiliser `MACOS_ARCH=arm64` pour un Mac
Apple Silicon et `MACOS_ARCH=amd64` pour un Mac Intel. La compilation croisée ne
requiert pas macOS. Sur un Mac équipé de `lipo`, `make build-macos-universal`
produit une application universelle.

## Déployer depuis la VM vers le Mac

Créer d'abord dans `~/.ssh/config` de la VM une cible SSH vers le Mac, par
exemple :

```sshconfig
Host mac
    HostName 192.168.1.20
    User mon-utilisateur-macos
```

Le Mac doit avoir **Réglages Système > Général > Partage > Session à distance**
activé. Puis lancer depuis la VM :

```sh
make deploy-macos \
  MACOS_HOST=mac \
  MACOS_ARCH=arm64 \
  KBRD_API_URL=http://kbrd.local:8081
```

La commande compile l'agent, copie `KBRD Agent.app` dans `~/Applications` sur
le Mac, installe le LaunchAgent utilisateur et redémarre le service. La même
commande sert à appliquer chaque modification ultérieure.

Le journal est disponible dans `~/Library/Logs/KBRD/agent.log`. Pour contrôler
le service sur le Mac :

```sh
launchctl print gui/$(id -u)/com.ubikyo.kbrd-agent
tail -f ~/Library/Logs/KBRD/agent.log
```

Le Mac doit pouvoir joindre `KBRD_API_URL`, et la machine qui exécute KBRD-API
doit pouvoir joindre le port TCP `8090` du Mac. Au premier lancement, autoriser
les connexions entrantes dans le pare-feu macOS si celui-ci le demande.

Avant une distribution à d'autres utilisateurs, l'application devra être
signée avec Developer ID puis notarisée.
