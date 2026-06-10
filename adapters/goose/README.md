# Goose adapter

Installed by:

```sh
onibi install-hooks --agent goose
```

Writes `~/.agents/plugins/onibi/hooks/hooks.json` or `$ONIBI_GOOSE_HOOKS`.

Support tier: event bridge. Goose lifecycle hooks emit events and turn-complete
signals to Onibi; blocking approval is not claimed.
