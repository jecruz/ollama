# ocelot — Ollama-compatible local inference with puma.cpp engine

**Built:** 2026-06-16
**Status:** Running on port 12435, end-to-end verified with gemma-2b-instruct.gguf and bge-small-en-v1.5-f16.gguf.

## What it is

`ocelot` is a light fork of [Ollama](https://github.com/ollama/ollama) that uses
[puma.cpp](https://github.com/jeffreycruz/puma.cpp) as its inference engine
instead of upstream llama.cpp. Puma.cpp is a personal llama.cpp fork that adds
M3 Ultra Metal optimizations and TurboQuant (TQ3_1S, TQ4_1S) custom types for
Apple Silicon.

The fork is "light": no Go source renames. Just a renamed binary output, custom
install paths, custom port, and one surgical source patch to skip a llama.cpp
flag that puma.cpp doesn't recognize yet.

## Why

- puma.cpp's engine improvements (Metal fusion, TurboQuant, residency sets) are
  better than stock llama.cpp on M3 Ultra
- Ollama's UX is better than the alternatives (model picker, auto-load, LRU
  eviction, transparent hot-swap, `/api/ps` introspection)
- puma-directd / prowld (prowl-llm's swap-by-restart supervisor) duplicates
  what Ollama's scheduler already does
- LLM Dynamix already has an Ollama openai-compatibility entry, so integration
  is just adding another entry

## Architecture

```
┌─────────────────┐
│  LLM Dynamix    │   reads config.yaml openai-compatibility
│  (port 12444)   │   exposes /v1/models, /v1/chat/completions
└────────┬────────┘
         │ http://127.0.0.1:12435/v1  (ocelot entry)
         ▼
┌─────────────────┐
│  ocelot         │   Go binary (~48MB)
│  (port 12435)   │   Ollama fork with custom Modelfile/JSON path
└────────┬────────┘
         │ subprocess (FindLlamaServer)
         ▼
┌─────────────────┐
│  llama-server   │   puma.cpp's prebuilt binary
│  (random port)  │   libggml-metal, libggml-cpu, libllama
└────────┬────────┘
         │ mmap
         ▼
┌─────────────────┐
│  GGUF blobs     │   ~/.ocelot/models/blobs/sha256-*
└─────────────────┘
```

## Install layout

| Path | Purpose |
|---|---|
| `~/Development/AI_INFERENCE/ocelot/` | Source (Ollama fork clone) |
| `~/.local/share/ocelot/bin/ocelot` | Compiled Go binary |
| `~/.local/share/ocelot/lib/ollama/` | puma.cpp's llama-server + dylibs |
| `~/.local/bin/ocelot` | Wrapper script (sets env vars, execs binary) |
| `~/.ocelot/` | Models dir (manifests + blobs) |
| `~/.ocelot/models/blobs/` | GGUF blobs (content-addressed) |
| `~/.ocelot/models/manifests/` | Per-model Modelfile metadata |
| `~/Library/LaunchAgents/com.ocelot.server.plist` | launchd service |
| `~/Library/Logs/ocelot/{ocelot.log,ocelot.err}` | Service logs |

## Service management

```bash
# Start
launchctl load ~/Library/LaunchAgents/com.ocelot.server.plist

# Stop
launchctl unload ~/Library/LaunchAgents/com.ocelot.server.plist

# Status
launchctl list | grep ocelot

# Logs
tail -f ~/Library/Logs/ocelot/ocelot.log
tail -f ~/Library/Logs/ocelot/ocelot.err
```

The service is `RunAtLoad` and `KeepAlive` (restart on crash, 10s throttle).

## Environment defaults (set by `~/.local/bin/ocelot` wrapper)

```bash
OLLAMA_HOST=127.0.0.1:12435
OLLAMA_MODELS=~/.ocelot
OLLAMA_KEEP_ALIVE=10m
OLLAMA_NUM_PARALLEL=2
OLLAMA_MAX_LOADED_MODELS=2
OLLAMA_MAX_QUEUE=512
OLLAMA_FLASH_ATTENTION=1
OLLAMA_KV_CACHE_TYPE=q8_0
OLLAMA_LLM_LIBRARY=metal
OLLAMA_CONTEXT_LENGTH=32768
OLLAMA_LOAD_TIMEOUT=10m
OLLAMA_NOHISTORY=1
```

## Patches applied during build

Three surgical patches were needed for the prebuilt-swap to work:

### 1. `puma.cpp/tools/quantize/quantize.cpp` — undefined struct

The upstream refactor removed the `llama_model_tensor_override` and
`llama_model_imatrix_data` structs from the public `llama.h` header, but
puma.cpp's `quantize.cpp` was never updated. They are now defined locally:

```cpp
// mirror of llama-quant.cpp internals (structs removed from public llama.h)
struct llama_model_imatrix_data { const char * name; const void * data; size_t size; };
struct llama_model_tensor_override { const char * pattern; int type; };
```

The `params.tensor_buft_overrides = ...` reference was also updated to
`params.tensor_types = ...` to match the new field name in
`llama_model_quantize_params`.

### 2. `ocelot/llm/llama_server.go` — `--no-log-prefix` flag

Ollama's `appendLlamaServerLogArgs` hardcodes `--no-log-prefix` and
`--no-log-timestamps`, which were added to upstream llama.cpp after puma.cpp's
last sync. Puma.cpp's llama-server uses positive-form flags (`--log-prefix` to
enable, default off) and doesn't recognize the negative form.

Removed both from the function; puma.cpp's default behavior matches what Ollama
wanted (log prefix off, timestamps off).

### 3. None (no Go source renames)

The Go source still says `ollama` everywhere internally. The binary is renamed
to `ocelot` at build time. The API paths, env var names, and Modelfile syntax
are all unchanged from upstream Ollama.

## Build commands

Re-running from scratch:

```bash
# 1. Clone Ollama source
cd ~/Development/AI_INFERENCE
git clone --depth 1 https://github.com/ollama/ollama.git ocelot
cd ocelot

# 2. Apply ocelot patches
#    - tools/quantize/quantize.cpp: not needed here (puma.cpp patch is upstream)
#    - llm/llama_server.go: edit appendLlamaServerLogArgs to remove --no-log-*

# 3. Build Go binary (no CMake needed; puma.cpp provides the native payload)
go build -o ~/.local/share/ocelot/bin/ocelot .

# 4. Build puma.cpp's llama-server + helpers
cd ~/Development/LLM_INFERENCE/puma.cpp
cmake -B build -DGGML_METAL=ON
cmake --build build --parallel 8 --target llama-server
cmake --build build --parallel 8 --target llama-quantize

# 5. Install puma.cpp's payload
cp build/bin/llama-server ~/.local/share/ocelot/lib/ollama/
cp build/bin/llama-quantize ~/.local/share/ocelot/lib/ollama/
cp build/bin/lib*.dylib ~/.local/share/ocelot/lib/ollama/

# 6. Create the wrapper script
cat > ~/.local/bin/ocelot <<'WRAPPER'
#!/bin/bash
# ... see ~/.local/bin/ocelot
WRAPPER
chmod +x ~/.local/bin/ocelot

# 7. Install launchd plist
cp ~/Development/AI_INFERENCE/ocelot/launchd/com.ocelot.server.plist \
   ~/Library/LaunchAgents/

# 8. Start
launchctl load ~/Library/LaunchAgents/com.ocelot.server.plist
```

## Adding a new model

```bash
# 1. Find or place the GGUF file
ls /path/to/model.gguf

# 2. Write a Modelfile
cat > /tmp/my-model.Modelfile <<EOF
FROM /path/to/model.gguf
PARAMETER num_ctx 32768
PARAMETER temperature 0.7
PARAMETER num_gpu 99
EOF

# 3. Register it (the GGUF is copied into ~/.ocelot/models/blobs/)
ocelot create my-model -f /tmp/my-model.Modelfile

# 4. Verify
ocelot list
curl http://127.0.0.1:12435/v1/models

# 5. Test
ocelot run my-model "Hello"
```

## LLM Dynamix integration

Already wired in `~/.llmdynamix/config.yaml`:

```yaml
openai-compatibility:
  - base-url: http://127.0.0.1:12435/v1
    models:
      - name: gemma2b:latest
      - name: bge-small:latest
    name: ocelot (puma.cpp)
```

LLM Dynamix's `llmdynamix-engine` will auto-discover these models on next
restart (or hot-reload if it watches the file). They appear in the model picker
alongside the existing Ollama and LM Studio entries.

## End-to-end verification log

```
22:39:42  launchctl loaded, server listening on 127.0.0.1:12435
22:39:42  M3 Ultra Metal device discovered
22:39:43  llama-server discovery: libDirs=[/Users/jeffreycruz/.local/share/ocelot/lib/ollama]
22:40:13  gemma2b created (1.7 GB blob copied, autodetected gemma-instruct template)
22:42:??  llama-quantize rebuilt from puma.cpp (was missing in initial build)
22:42:??  ocelot Go binary rebuilt (--no-log-prefix patch)
22:43:??  chat completion test: "ocelot is alive" returned by gemma2b
22:44:??  bge-small created (67 MB embedding model)
22:44:??  /api/ps shows gemma2b loaded in VRAM (2.99GB, 4096 context, 10m expires)
22:44:??  LLM Dynamix config updated with ocelot entry
```

## Known limitations

1. **No TurboQuant models in test set yet.** The gemma and bge models are
   standard Q4_K_M / F16. To verify TurboQuant support, register a model
   quantized with TQ3_1S or TQ4_1S (requires `llama-quantize` from puma.cpp
   with the new type IDs).

2. **No image/vision support yet.** puma.cpp has `llama-llava-cli` and
   `llama-qwen2vl-cli` in the build/bin directory, but ocelot's image-gen
   path uses a different code path (`imagegen.NewServer`) that bypasses
   llama-server. Vision support requires additional wiring.

3. **patch overhead on Ollama updates.** Every `git pull` in the ocelot
   source will conflict with the `llm/llama_server.go` patch. Re-apply
   with `git apply` after pulling.

4. **M3 Ultra Metal optimizations not benchmarked.** No perf comparison vs
   stock Ollama yet. The optimizations are real (per puma.cpp's docs) but
   unverified in this ocelot configuration.

5. **`/v1/models` returns `data: null` when empty** (Ollama default). OpenAI
   spec says `data: []`. Cosmetic; most clients handle both.

## Roadmap

- [ ] Add a TurboQuant-quantized model and verify the type is recognized
- [ ] Benchmark ocelot vs stock Ollama on M3 Ultra
- [ ] Add support for vision/multimodal models
- [ ] Sync with upstream Ollama (re-apply patches as needed)
- [ ] Add a `make` target that automates the full build chain
- [ ] Add a small CLI helper that surfaces the best model for a given task
      (e.g., "smallest model that can do X")

## Files of interest

- `puma.cpp/CLAUDE.md` — puma.cpp fork conventions
- `puma.cpp/tools/quantize/quantize.cpp` — has the patch (line 590-595)
- `ocelot/llm/llama_server.go` — has the patch (line 545-551)
- `~/.local/bin/ocelot` — wrapper script with env defaults
- `~/Library/LaunchAgents/com.ocelot.server.plist` — launchd service
- `~/.llmdynamix/config.yaml` — LLM Dynamix entry
