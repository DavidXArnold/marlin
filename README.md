<p align="center">
  <img src="assets/marlin_logo.png" alt="marlin" width="200" />
</p>

<h1 align="center">marlin</h1>

<p align="center">An opinionated CLI for managing local LLM inference. Handles vLLM (systemd), NIM (Docker/Podman/nerdctl), and llama.cpp/GGUF model switching, live health checks, hardware detection, and registry searches.</p>

## Features

- **Three provider types** — `vllm` (systemd + env-file symlink), `nim` (NIM containers via Docker, Podman, or nerdctl), and `llamacpp` (llama-server via a dedicated systemd unit)
- **Interactive TUI** — fuzzy model picker, multi-step add wizard, search result picker, and live `marlin top` dashboard (bubbletea)
- **Atomic symlink swap** — zero-gap `model.env` rotation for vLLM and llama.cpp
- **NIM container lifecycle** — pull, stop old, start new; host cache dir prepared with correct GID=0 permissions
- **NIM digest pinning** — `marlin update` pulls the latest image, detects digest changes, and auto-switches when a new version lands
- **Model config inheritance** — `extends = "slug"` to inherit shared settings; `abstract = true` hides base configs from the picker
- **Quant advisor** — `marlin advise <model>` shows per-quantization VRAM estimates with GPU-fit indicators
- **Benchmark suite** — `marlin bench` measures TTFT and decode throughput across multiple runs
- **Registry search** — HuggingFace and NGC/NIM catalog with VRAM estimates and fit indicators
- **Hardware detection** — GPU VRAM, compute capability, UMA/unified-memory architecture (GB10, GH200, GB200), RAM, disk; plus live power draw, temperature, and clock via nvidia-smi
- **Validation** — quantization mismatch, GPU memory, served-model-name alias checks
- **Privilege escalation** — prompts for `sudo` only when needed (like `systemctl`)
- **State tracking** — persists active model, provider, container ID, and pinned digest
- **Event history** — append-only JSONL log of every switch/stop event with elapsed times and session durations
- **Environment health checks** — `marlin doctor` audits runtime, GPU, secrets, paths, disk, and config
- **Disk reclamation** — `marlin prune` scans HuggingFace and NIM caches and removes stale data
- **Post-start smoke tests** — optional completion/streaming/tool-call validation after the API becomes ready

## Installation

### From release (recommended)

```bash
# .deb (Ubuntu/Debian)
curl -LO "https://github.com/DavidXArnold/marlin/releases/latest/download/marlin_linux_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').deb"
sudo dpkg -i marlin_linux_*.deb

# .rpm (RHEL/Fedora)
curl -LO "https://github.com/DavidXArnold/marlin/releases/latest/download/marlin_linux_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').rpm"
sudo rpm -i marlin_linux_*.rpm
```

### From source

```bash
git clone https://github.com/DavidXArnold/marlin.git
cd marlin
make install
```

## Configuration

Copy the example config and edit for your environment:

```bash
sudo cp configs/marlin.toml.example /etc/marlin/config.toml
sudo $EDITOR /etc/marlin/config.toml
```

Config is loaded from `/etc/marlin/config.toml` by default. Override with `--config` or `MARLIN_CONFIG`. All settings have built-in defaults — an empty or missing config file is valid.

Secrets are managed via `marlin configure` or written directly to `paths.secrets_env`:

```
HF_TOKEN=hf_...
NGC_API_KEY=nvapi-...
```

### `[behavior]`

| Key | Default | Description |
|---|---|---|
| `switch_prompt` | `true` | Confirm before switching to a different model |
| `add_auto_detect` | `false` | Auto-populate model params in the add wizard (YMMV) |
| `log_tail_lines` | `100` | Lines shown by `marlin logs` without `-f` |
| `allow_type_switch` | `true` | Allow switching between vllm and nim providers |
| `warn_unmanaged_containers` | `true` | Warn in `marlin status` if unmanaged inference containers are found |
| `check_updates` | `true` | Notify when a newer marlin release is available on GitHub |
| `global_install` | `false` | Write new model configs to `global_models_dir` (requires sudo) |
| `warn_on_system_resources` | `true` | Warn before switch if system load is high |
| `system_load_threshold` | `0.8` | Load-average fraction that triggers the resource warning |
| `max_runtime` | `""` | Auto-stop the model after this duration (e.g. `"2h"`, `"30m"`); empty = disabled |
| `smoke_test` | `false` | Run API smoke tests (completion, streaming, tool-call) after startup |
| `smoke_test_timeout` | `"30s"` | Timeout for the full smoke test suite |
| `smoke_test_skip` | `[]` | Tests to skip; valid values: `"streaming"`, `"tool_call"` |

### `[paths]`

| Key | Default | Description |
|---|---|---|
| `models_dir` | `~/.config/marlin/models` | Per-user TOML model config directory |
| `global_models_dir` | `/etc/marlin/models` | Read-only system-wide model library |
| `active_symlink` | `/etc/marlin/model.env` | Symlink → active model's rendered env file (vLLM) |
| `secrets_env` | `~/.config/marlin/secrets.env` | `HF_TOKEN` and `NGC_API_KEY` |
| `state_file` | `~/.local/share/marlin/state.toml` | Active model, provider, and container ID |
| `nim_cache` | `/var/cache/nim` | Host path mounted into NIM containers as model cache |
| `history_file` | `~/.local/share/marlin/history.jsonl` | Append-only JSONL event log; auto-rotated at 50 MiB |
| `llamacpp_env_file` | `/etc/marlin/llamacpp.env` | Symlink → active llama.cpp model's rendered env file |

`models_dir`, `secrets_env`, and `state_file` default to user-local paths (no sudo required). Override to `/etc/marlin/…` or `/var/lib/marlin/…` for shared system deployments.

### `[service]`

| Key | Default | Description |
|---|---|---|
| `systemd_unit` | `"marlin"` | Systemd unit name managed by `marlin start --enable` |
| `docker_container` | `"marlin"` | Name given to managed NIM containers |
| `vllm_image` | `"nvcr.io/nvidia/vllm:26.05.post1-py3"` | Docker image used for ad-hoc `marlin run` with vLLM |
| `container_runtime` | `"docker"` | Container runtime: `docker`, `podman`, or `containerd` |
| `container_socket` | `""` | Custom socket path for Docker/Podman; empty = auto-detect |
| `llamacpp_unit` | `"marlin-llamacpp"` | Systemd unit for llama-server (llamacpp provider) |

### `[server]`

| Key | Default | Description |
|---|---|---|
| `host` | `"localhost"` | Host where the inference API is listening |
| `port` | `8000` | Port where the inference API is listening |
| `alias` | `"local"` | Short name shown in `marlin status` |
| `health_path` | `"/health"` | Path polled by `marlin start` to detect API readiness. Common alternatives: `/healthz`, `/ready`, `/v1/health/ready` |

### `[registries.*]`

| Key | Default | Description |
|---|---|---|
| `registries.huggingface.enabled` | `true` | Include HuggingFace in `marlin search` |
| `registries.ngc.enabled` | `true` | Include NGC/NIM catalog in `marlin search` |
| `registries.modelscope.enabled` | `false` | Include ModelScope in `marlin search` |

## Commands

### `marlin configure`

Interactively set or update API keys. Prompts for `HF_TOKEN` (HuggingFace gated models) and `NGC_API_KEY` (NIM containers and NGC search). Optionally runs `docker login nvcr.io` after saving an NGC key.

```bash
marlin configure
```

### `marlin list`

```
SLUG                           TYPE   STATUS     MODEL ID
----                           ----   ------     --------
qwen25-72b-awq                 vllm   working    Qwen/Qwen2.5-72B-Instruct-AWQ  ◀ active
llama-3.1-8b-nim               nim    untested   nvcr.io/nim/meta/llama-3.1-8b-instruct:latest
```

### `marlin start [model]`

Start the inference service. Without a model argument, restarts the already-active model (or shows an interactive picker if nothing is active). With a model argument, behaves like `marlin switch`.

```bash
marlin start                    # restart active model or pick one
marlin start qwen25-72b-awq     # switch and start
marlin start --enable           # also enable systemd unit at boot (vLLM)
marlin start --logs             # stream container logs while waiting for the API
marlin start -vvv               # stream container logs to stderr alongside the spinner
marlin start --max-runtime 2h   # auto-stop after 2 hours
```

After switching, `marlin start` polls `server.health_path` (default `/health`) until the API responds 200, showing a spinner. If `behavior.smoke_test = true`, a quick completion/streaming/tool-call validation runs immediately after the API becomes ready.

Configure the health path for non-vLLM containers that use a different endpoint:

```toml
[server]
health_path = "/v1/health/ready"   # NIM
# health_path = "/healthz"         # other common alternatives
```

### `marlin switch [model]`

Switch the active inference model. Shows an interactive fuzzy picker when no argument is given.

- **vLLM**: validates the config, renders the env file, atomically replaces the active symlink, and restarts the systemd unit.
- **NIM**: prepares the host cache directory (sets GID=0 and group-write permissions), pulls the image, stops the old container, and starts a new one. Stores the pinned image digest in state.
- **llama.cpp**: renders the env file (`LLAMA_MODEL`, `LLAMA_NGL`, `LLAMA_CONTEXT`), updates the `llamacpp.env` symlink, and restarts the `marlin-llamacpp` unit.

Records `switch_start` and `switch_ready` events to the history log with elapsed time. Abstract models (base configs with `abstract = true`) are hidden from the picker.

```bash
marlin switch qwen25-72b-awq
marlin switch          # interactive picker
```

### `marlin add [registry-id]`

Interactive wizard for creating a new model config. For vLLM: `provider type → model ID → slug → quantization → GPU memory utilization → max model length → served model names → tool call parser → notes → confirm`. For NIM: `provider type → image → slug → extra env vars → extra volume mounts → notes → confirm`. Writes a `.toml` file to `paths.models_dir`.

```bash
marlin add
marlin add Qwen/Qwen2.5-72B-Instruct-AWQ
```

### `marlin validate <model>`

Run validation checks without switching. Warns on quantization mismatches, excessive GPU memory utilization, and served-model-name alias issues. Use `--show-config` to render the equivalent `docker run` or systemd env-file command.

```bash
marlin validate qwen25-72b-awq
# [warn] serve.gpu_memory_utilization 0.970 is very high (>0.95)

marlin validate llama-nim --show-config
# docker run --rm --gpus all -p 8000:8000 ...
```

### `marlin status`

Shows the active model, live container state (NIM), API health, and detected hardware including GPU power draw, temperature, and clock (when nvidia-smi is available).

```
active model : llama-3.1-8b-nim
provider     : nim
container    : a1b2c3d4e5f6  (running)
api health   : ready at http://127.0.0.1:8000/v1

gpu[0]       : NVIDIA H100 80GB HBM3  vram 74 GiB free / 80 GiB total
               power 312 W / 700 W  temp 67 C  clock 1980 MHz
ram          : 220 GiB free / 256 GiB total
disk (models)    : 1.2 TiB free / 2.0 TiB total
disk (nim cache) : 800 GiB free / 2.0 TiB total
```

On Grace-Blackwell/unified-memory systems (GB10, GH200, GB200):

```
gpu[0]       : NVIDIA GB10  unified memory (see RAM)  sm_121
               hint: add extra_env = ["NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9"] if model OOMs
ram          : 105 GiB free / 128 GiB total
```

### `marlin doctor`

Runs a suite of environment health checks and reports results.

```bash
marlin doctor           # check only
marlin doctor --fix     # auto-fix safe issues (file permissions, missing dirs)
marlin doctor --fix --yes  # skip confirmation when fixing
```

```
[PASS] config.loaded             /etc/marlin/config.toml
[PASS] runtime.docker            docker 27.3.1
[WARN] runtime.podman            not reachable
       hint: install podman to use this container runtime
[PASS] gpu.driver                driver 535.129.03
[PASS] gpu.compute_cap           compute cap 9.0
[WARN] secrets.hf_token_set      HF_TOKEN not set
       hint: run 'marlin configure' to set your HuggingFace token
[PASS] paths.models_dir          /home/user/.config/marlin/models
[WARN] disk.models_dir           12 GiB free (want ≥50 GiB)

3 PASS, 3 WARN, 0 FAIL
```

Checks: `config.loaded`, `runtime.docker/podman/nerdctl`, `gpu.driver`, `gpu.compute_cap`, `gpu.uma`, `secrets.file_exists/file_mode/hf_token_set/ngc_key_set`, `paths.models_dir/nim_cache/state_dir`, `disk.models_dir/nim_cache`.

### `marlin history`

Show the event log of switch and stop operations.

```bash
marlin history              # last 20 events
marlin history --last 50    # last 50 events
marlin history --slug qwen25-72b-awq  # filter by model
marlin history --since 7d   # events in the last 7 days
marlin history stats        # aggregate statistics
```

```
TIME                  EVENT           SLUG                   ELAPSED/DURATION
---------------------------------------------------------------------------
2026-06-20 10:15:02   switch_start    qwen25-72b-awq
2026-06-20 10:15:28   switch_ready    qwen25-72b-awq         26.1s
2026-06-20 14:22:01   stop            qwen25-72b-awq         14817s
```

```bash
marlin history stats
total switches : 14
unique models  : 3
avg ready time : 24.3s
avg session    : 2h 18m
crashes        : 0
```

History is stored in `paths.history_file` (default `~/.local/share/marlin/history.jsonl`) and auto-rotated to `.1.gz` when it exceeds 50 MiB.

### `marlin prune`

Scan HuggingFace hub cache and NIM cache directories and report reclaimable disk space.

```bash
marlin prune                         # dry run — show what would be deleted
marlin prune --apply                 # delete identified directories
marlin prune --hf-cache /data/hf     # override HF cache location
```

```
prune [DRY RUN]

  hf-cache      2.3 GiB  /home/user/.cache/huggingface/hub/models--Qwen--Qwen2.5-7B
  nim-cache    14.7 GiB  /var/cache/nim/llama-3.1-8b-instruct

total reclaimable: 17.0 GiB

run with --apply to delete
```

### `marlin logs [-f] [--lines N]`

Stream inference service logs via `journalctl` (vLLM) or `docker logs` / `nerdctl logs` (NIM).

```bash
marlin logs -f
marlin logs --lines 200
```

### `marlin run <model>`

Run a model ad-hoc in a container — no systemd service required. The container is labelled so marlin can track, list, and stop it.

```bash
marlin run llama-3.1-8b-nim           # foreground — streams logs, Ctrl-C stops and removes
marlin run llama-3.1-8b-nim -d        # background — returns immediately
```

The vLLM image used for ad-hoc runs is configurable via `service.vllm_image` (default: `vllm/vllm-openai:latest`).

### `marlin ps`

List all marlin-managed ad-hoc containers.

```
MODEL                PROVIDER STATUS     PORT   CONTAINER ID
-----                -------- ------     ----   ------------
llama-3.1-8b-nim     nim      running    8000   a1b2c3d4e5f6
```

### `marlin stop [model]`

Stop the active managed model (or a specific ad-hoc container). Records a `stop` event with active session duration to the history log.

```bash
marlin stop                    # stop active model or all ad-hoc containers
marlin stop llama-3.1-8b-nim  # stop a specific model/container
```

### `marlin search <query>`

Search HuggingFace and NGC for models. Results include last-updated time, estimated VRAM requirement (derived from parameter count in the model name), and a fit indicator based on your GPU's free VRAM.

When running in a terminal, drops into an interactive TUI picker after printing the results table. Select a model to:

- **Open in browser** — opens the HuggingFace or NGC catalog page
- **Add as model profile** — derives a slug and writes a `.toml` to `paths.models_dir`
- **Run adhoc** — starts the model in a container immediately

Use `--plain` for non-interactive/scripted output.

```bash
marlin search "Qwen 72B"
marlin search --registry ngc llama
marlin search --plain "llama 8b"   # table only, no picker
```

```
[huggingface]
ID                                                   UPDATED      VRAM EST  FIT   DESCRIPTION
--                                                   -------      --------  ---   -----------
Qwen/Qwen2.5-72B-Instruct-AWQ                        3mo ago      34 GiB    ✓     Qwen2.5 72B AWQ...
Qwen/Qwen2.5-72B-Instruct                            3mo ago      144 GiB   ✗     Qwen2.5 72B fp16...

[ngc]
ID                                                   UPDATED      VRAM EST  FIT   DESCRIPTION
--                                                   -------      --------  ---   -----------
nvidia/llama-3.3-70b-instruct                        2mo ago      140 GiB   ✗
nvidia/llama-3.1-nemotron-ultra-253b-v1              -            -         ?
```

FIT legend: `✓` comfortable fit · `~` tight fit · `✗` exceeds free VRAM · `?` unknown

### `marlin update [model]`

Pull the latest NIM image for the active (or named) model, check whether the image digest has changed, and automatically switch to the new version if it has. Safe to run on a cron schedule.

```bash
marlin update                  # update active model
marlin update llama-3.1-8b-nim # update a specific model
```

```
pulling nvcr.io/nim/meta/llama-3.1-8b-instruct:latest ...
digest changed (sha256:abc123... → sha256:def456...)
switching to llama-3.1-8b-nim with new digest
```

If the digest is unchanged, prints "already up to date" and exits without switching.

### `marlin top`

Live bubbletea TUI dashboard showing GPU utilization, VRAM usage, power draw, temperature, and active model status. Refreshes every 2 seconds.

```bash
marlin top
```

```
marlin top — llama-3.1-8b-nim (running)  q quit

GPU[0]  NVIDIA H100 80GB HBM3  sm_9.0
  vram  ████████████████████░░░░  74 / 80 GiB  92%
  power 312 W / 700 W
  temp  67 C  clock 1980 MHz
```

### `marlin bench`

Benchmark the active model's inference performance — measures time-to-first-token (TTFT) and decode throughput across multiple runs.

```bash
marlin bench                              # single run, default prompt
marlin bench --runs 5                     # 5 runs, report mean ± stddev
marlin bench --prompt "Write a haiku"     # custom prompt
marlin bench --max-tokens 200             # limit output tokens
```

```
run 1/3  ttft 89ms   tokens 128  throughput 142 tok/s
run 2/3  ttft 91ms   tokens 128  throughput 139 tok/s
run 3/3  ttft 87ms   tokens 128  throughput 145 tok/s

ttft     89.0ms ± 2.0ms
tokens   128.0 ± 0.0
tok/s    142.0 ± 3.1
```

### `marlin advise <model-id>`

Estimate per-quantization VRAM requirements for a model and recommend the highest-quality option that fits your GPU.

```bash
marlin advise meta-llama/Llama-3.1-70B-Instruct
marlin advise Qwen/Qwen2.5-72B --no-detect   # skip VRAM detection
```

```
quant advisor: Llama-3.1-70B-Instruct  (70B params)
available VRAM: 480 GiB

QUANTIZATION    EST. VRAM   FITS?
------------    ---------   -----
fp16            156 GiB     ✓
fp8             78 GiB      ✓
nvfp4           39 GiB      ✓
awq_marlin      39 GiB      ✓
gptq            39 GiB      ✓

recommendation: fp16 (full precision) — search HF: "Llama-3.1-70B-Instruct fp16"
```

### `marlin rm <model>`

Remove a model profile from `paths.models_dir`.

```bash
marlin rm qwen25-72b-awq
```

### `marlin edit <model>`

Open a model profile in `$EDITOR` (falls back to `vi`).

```bash
marlin edit qwen25-72b-awq
```

### `marlin install`

Install the vLLM systemd unit file. Run once before using `marlin start --enable` for boot-time autostart.

```bash
marlin install
```

### `marlin restart`

Restart the active managed model's service without switching models.

```bash
marlin restart
```

### `marlin completion`

Generate shell completion scripts.

```bash
marlin completion bash   | sudo tee /etc/bash_completion.d/marlin
marlin completion zsh    | sudo tee /usr/local/share/zsh/site-functions/_marlin
marlin completion fish   > ~/.config/fish/completions/marlin.fish
```

## Model config format

Each model is a TOML file in `paths.models_dir`.

### vLLM model

```toml
[model]
id     = "Qwen/Qwen2.5-72B-Instruct-AWQ"
type   = "vllm"
status = "working"
notes  = "Best for tool-calling"

[serve]
quantization           = "awq_marlin"
gpu_memory_utilization = 0.90
max_model_len          = 131072
served_model_name      = ["local", "qwen25-72b"]
tool_call_parser       = "hermes"
```

**NVFP4 models**: set `quantization = "nvfp4"` but do not pass `--quantization` to vLLM — it is auto-detected from the model config. marlin omits the flag automatically.

### NIM model

```toml
[model]
image  = "nvcr.io/nim/meta/llama-3.1-8b-instruct:latest"
type   = "nim"
status = "untested"

[serve]
# Optional: pass extra env vars into the NIM container.
# Useful for UMA/Grace-Blackwell systems that need a higher gpu_memory_utilization:
extra_env     = ["NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9"]
extra_volumes = ["/data/models:/opt/nim/.cache"]
```

Common NIM env vars:

| Variable | Purpose |
| --- | --- |
| `NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9` | Override GPU memory fraction (needed on UMA/GB10 systems) |
| `NIM_TENSOR_PARALLEL_SIZE=2` | Multi-GPU tensor parallelism |
| `NIM_MAX_MODEL_LEN=8192` | Override max context length |
| `NIM_KVCACHE_PERCENT=0.8` | KV cache memory fraction |

### llama.cpp / GGUF model

Requires a `marlin-llamacpp.service` systemd unit that reads `/etc/marlin/llamacpp.env` and runs `llama-server`:

```ini
[Service]
EnvironmentFile=/etc/marlin/llamacpp.env
ExecStart=llama-server -m ${LLAMA_MODEL} --ngl ${LLAMA_NGL} -c ${LLAMA_CONTEXT:-4096} --port 8080
```

```toml
[model]
id     = "bartowski/Llama-3.2-3B-Instruct-GGUF"
type   = "llamacpp"
status = "working"

[serve]
gguf_path    = "/var/cache/huggingface/hub/models--bartowski--Llama-3.2-3B-Instruct-GGUF/snapshots/latest/Llama-3.2-3B-Instruct-Q4_K_M.gguf"
ngl          = 99        # GPU layers to offload; default 99 (all)
context_size = 8192
```

`marlin switch` writes `LLAMA_MODEL`, `LLAMA_NGL`, and `LLAMA_CONTEXT` to `<models_dir>/<slug>.llamacpp.env`, updates the `paths.llamacpp_env_file` symlink, and restarts the `service.llamacpp_unit` systemd unit.

### Config inheritance

Use `extends` to share common settings across multiple model configs. The child wins on any field it sets; arrays replace (not append).

```toml
# base-llama3.toml
[model]
type     = "vllm"
abstract = true   # hidden from picker and list; used as a base only

[serve]
gpu_memory_utilization = 0.90
tool_call_parser       = "llama3_json"
served_model_name      = ["local"]
```

```toml
# llama-3.1-8b.toml
[model]
id      = "meta-llama/Llama-3.1-8B-Instruct"
type    = "vllm"
extends = "base-llama3"   # inherits gpu_memory_utilization and tool_call_parser

[serve]
max_model_len = 32768
```

## NIM container requirements

The host cache directory (`paths.nim_cache`) must be world-writable so the NIM container user can create subdirectories (e.g. `local_cache`). `marlin switch` handles this automatically — it runs `chmod -R 777` (and `chgrp -R 0`) on the cache dir, prompting for `sudo` if needed.

If a container exits immediately with a `PermissionError` on `/opt/nim/.cache/local_cache`, run:

```bash
sudo chmod -R 777 /var/cache/nim
```

**Grace-Blackwell / unified-memory systems (DGX Spark GB10, GH200, GB200):** nvidia-smi reports 0 VRAM for these GPUs. NIM's memory classifier clamps `gpu_memory_utilization` to 0.50 on UMA platforms, which is too low for large models. Set `NIM_PASSTHROUGH_ARGS=--gpu-memory-utilization 0.9` in `extra_env` to override it. `marlin status` will show this hint automatically when a UMA GPU is detected.

## Development

```bash
make test          # run tests with race detector
make coverage      # coverage report + gate (85%)
make coverage-html # open HTML report in browser
make lint          # golangci-lint
make check         # lint + coverage + integration (CI gate)
make build         # compile to bin/marlin
```

Integration tests against a live server:

```bash
MARLIN_TEST_HOST=localhost:8000 make integration
```

## Architecture

```
cmd/                    Cobra commands
internal/
  advise/               Quant advisor (VRAM estimation, fit detection, recommendations)
  bench/                Benchmark engine (TTFT + decode throughput, multi-run stats)
  config/               Global config + per-model TOML schema (includes inheritance resolver)
  doctor/               Environment health checks (runtime, GPU, secrets, paths, disk)
  history/              Append-only JSONL event log with rotation
  provider/             Provider interface + VLLMProvider + NIMProvider + ContainerdNIMProvider + LlamaCppProvider + AdhocRunner
  service/              Systemd wrapper (enable, start, stop, logs)
  smoke/                Post-startup API smoke tests (completion, streaming, tool-call)
  state/                Persistent active-model state (includes pinned_digest)
  sysinfo/              GPU (VRAM, compute cap, UMA detection, power/temp/clock), RAM, disk
  top/                  bubbletea TUI dashboard (GPU/VRAM/power/temp live view)
  ui/                   bubbletea TUI (fuzzy picker, add wizard, search picker)
  update/               NIM image digest comparison and auto-switch logic
  validate/             Model config validation
  registry/             HuggingFace + NGC registry clients
  secrets/              Dotenv secrets loader
  privilege/            Sudo escalation (file write, mkdir, NIM cache prep)
  vllm/                 OpenAI-compatible health, model list, and streaming client
pkg/
  render/               Env file renderer (model config → KEY=VALUE for vLLM and llama.cpp)
```

## License

GNU General Public License v3.0 — see [LICENSE](LICENSE).
