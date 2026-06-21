---
name: project-multimodel
description: Multi-model simultaneous running — deferred to a future milestone
metadata:
  type: project
---

Multiple models running simultaneously is a planned future feature, deferred after v0.1.29.

**Why:** User wants the ability to run llama-8b and qwen-72b at the same time on different ports for A/B testing and quick experimentation, without having to stop one to start another.

**Approach options discussed:**
- **Option A (named slots):** `marlin start llama-8b --slot gpu0 --port 8000` — state.toml grows a slots map; best long-term shape for systemd multi-model support but requires state redesign.
- **Option B (marlin run only):** Keep `marlin start` single-model; `marlin run` auto-picks next free port; already Docker-based so natural fit. **Recommended for near-term.**
- **Option C (per-profile port):** each model.toml specifies its own port; user manages collisions manually.

**How to apply:** When the user asks to implement multi-model, start with Option B (extend `marlin run` with auto-port selection + `marlin ps` as the primary multi-model view), then discuss Option A for the systemd path.
