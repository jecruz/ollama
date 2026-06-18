# ocelot

Light fork of [Ollama](https://github.com/ollama/ollama) that uses
[puma.cpp](https://github.com/jeffreycruz/puma.cpp) as its inference engine
for M3 Ultra Metal optimizations and TurboQuant support.

This project file is for project-specific guidance; see `/Users/jeffreycruz/Development/CLAUDE.md`
for umbrella rules.

## Service

```bash
launchctl load ~/Library/LaunchAgents/com.ocelot.server.plist
launchctl list | grep ocelot
```

Logs: `~/Library/Logs/ocelot/{ocelot.log,ocelot.err}`

## Wrapper

`~/.local/bin/ocelot` is the user-facing CLI. It sets env vars and execs
the binary at `~/.local/share/ocelot/bin/ocelot`.

## Engine

`puma.cpp` is a personal llama.cpp fork. It provides the `llama-server` and
`llama-quantize` binaries that ocelot shells out to. The prebuilt artifacts
live in `~/.local/share/ocelot/lib/ollama/`.

## Patches

Two source patches are required for compatibility:

1. `puma.cpp/tools/quantize/quantize.cpp` — adds `llama_model_tensor_override`
   and `llama_model_imatrix_data` struct definitions (removed from public
   `llama.h` by upstream refactor).

2. `ocelot/llm/llama_server.go` — removes `--no-log-prefix` and
   `--no-log-timestamps` from `appendLlamaServerLogArgs` (puma.cpp predates
   these flags).

Both patches are documented in
`docs/plans/2026-06-16-ocelot-build.md`.

## Re-syncing with upstream

```bash
# Update ocelot (Ollama fork)
cd ~/Development/AI_INFERENCE/ocelot
git pull --rebase origin main
# Re-apply the --no-log-prefix patch if conflict
# (see docs/plans/2026-06-16-ocelot-build.md for the exact edit)

# Update puma.cpp
cd ~/Development/LLM_INFERENCE/puma.cpp
git pull --rebase
# Re-build llama-server and llama-quantize
cmake --build build --parallel 8 --target llama-server llama-quantize
# Re-install to ocelot lib dir
cp build/bin/llama-server build/bin/llama-quantize ~/.local/share/ocelot/lib/ollama/
cp build/bin/lib*.dylib ~/.local/share/ocelot/lib/ollama/
# Restart the service
launchctl unload ~/Library/LaunchAgents/com.ocelot.server.plist
launchctl load ~/Library/LaunchAgents/com.ocelot.server.plist
```

## Models

Models are stored in `~/.ocelot/models/`. Register a new model:

```bash
cat > /tmp/my-model.Modelfile <<EOF
FROM /path/to/model.gguf
PARAMETER num_ctx 32768
PARAMETER temperature 0.7
EOF
ocelot create my-model -f /tmp/my-model.Modelfile
```
