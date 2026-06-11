<p align="center">
  <img src="assets/marlin_logo.png" alt="marlin" width="200" />
</p>

<h1 align="center">marlin</h1>

<p align="center">An opinionated CLI for managing local LLM inference. Handles vLLM (systemd) and NIM (Docker/Podman/nerdctl) model switching, live health checks, hardware detection, and registry searches.</p>

## Features

- **Two provider types** — `vllm` (systemd service + env-file symlink) and `nim` (NIM containers via Docker, Podman, or nerdctl)
- **Interactive TUI** — fuzzy model picker, multi-step add wizard, and search result picker (bubbletea)
- **Atomic symlink swap** — zero-gap `model.env` rotation for vLLM
- **NIM container lifecycle** — pull, stop old, start new; host cache dir prepared with correct GID=0 permissions
- **Registry search** — HuggingFace and NGC/NIM catalog with VRAM estimates and fit indicators
- **Hardware detection** — GPU VRAM, compute capability, UMA/unified-memory architecture (GB10, GH200, GB200), RAM, disk
- **Validation** — quantization mismatch, GPU memory, served-model-name alias checks
- **Privilege escalation** — prompts for `sudo` only when needed (like `systemctl`)
- **State tracking** — persists active model, provider, and container ID

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

Key paths (all overridable in config):

| Setting | Default | Purpose |
|---|---|---|
| `paths.models_dir` | `/etc/marlin/models` | TOML model configs |
| `paths.active_symlink` | `/etc/marlin/model.env` | Symlink → active model's rendered env file |
| `paths.secrets_env` | `/etc/marlin/secrets.env` | `HF_TOKEN` and `NGC_API_KEY` |
| `paths.state_file` | `/var/lib/marlin/state.toml` | Active model/provider/container state |
| `paths.nim_cache` | `/var/cache/nim` | Host path mounted into NIM containers as model cache |

Secrets (also manageable via `marlin configure`):

```
HF_TOKEN=hf_...
NGC_API_KEY=nvapi-...
```

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
```

### `marlin switch [model]`

Switch the active inference model. Shows an interactive fuzzy picker when no argument is given. For vLLM: validates the config, renders the env file, atomically replaces the active symlink, and restarts the systemd unit. For NIM: prepares the host cache directory (sets GID=0 and group-write permissions), pulls the image, stops the old container, and starts a new one.

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

Run validation checks without switching. Warns on quantization mismatches, excessive GPU memory utilization, and served-model-name alias issues.

```bash
marlin validate qwen25-72b-awq
# [warn] serve.gpu_memory_utilization 0.970 is very high (>0.95)
```

### `marlin status`

Shows the active model, live container state (NIM), API health, and detected hardware. For NIM, when the API is not ready it shows the last container log line and a hint if an OOM or UMA failure pattern is detected.

```
active model : llama-3.1-8b-nim
provider     : nim
container    : a1b2c3d4e5f6  (running)
api health   : ready at http://127.0.0.1:8000/v1

gpu[0]       : NVIDIA H100 80GB HBM3  vram 74 GiB free / 80 GiB total
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

Stop and remove one or all ad-hoc containers started with `marlin run`.

```bash
marlin stop llama-3.1-8b-nim  # stop a specific model
marlin stop                    # stop all marlin-managed containers
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

### `marlin completion`

Generate shell completion scripts.

```bash
marlin completion bash   | sudo tee /etc/bash_completion.d/marlin
marlin completion zsh    | sudo tee /usr/local/share/zsh/site-functions/_marlin
marlin completion fish   > ~/.config/fish/completions/marlin.fish
```

## Model config format

Each model is a TOML file in `paths.models_dir`.

vLLM model:

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

NIM model:

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

## NIM container requirements

NIM containers run as UID=1000, GID=0. The host cache directory (`paths.nim_cache`) must be owned or group-accessible by GID=0. `marlin switch` handles this automatically — it runs `chgrp -R 0` and `chmod -R g+rwX` on the cache dir, prompting for `sudo` if needed.

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
  config/               Global config + per-model TOML schema
  provider/             Provider interface + VLLMProvider + NIMProvider + ContainerdNIMProvider + AdhocRunner
  service/              Systemd wrapper (enable, start, stop, logs)
  state/                Persistent active-model state
  ui/                   bubbletea TUI (fuzzy picker, add wizard, search picker)
  validate/             Model config validation
  registry/             HuggingFace + NGC registry clients
  secrets/              Dotenv secrets loader
  privilege/            Sudo escalation (file write, mkdir, NIM cache prep)
  sysinfo/              GPU (VRAM, compute cap, UMA detection), RAM, disk
  vllm/                 OpenAI-compatible health + model list client
pkg/
  render/               Env file renderer (model config → KEY=VALUE)
```

## License

GNU General Public License v3.0 — see [LICENSE](LICENSE).
