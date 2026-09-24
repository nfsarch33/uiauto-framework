# Laya decision layer (containerized)

[Laya](https://github.com/NandhaKishorM/laya) (Apache-2.0, Convai
Innovations) is a typed-decision engine, NOT a browser driver: given a
state dict and questions, it returns structured decisions — `choice`
(label + calibrated probability distribution), `score` (ordinal rubric),
`noul` (yes/no probability) — in a single forward pass with no text
generation. This framework uses it as the DECISION layer; browser-use (or
the CDP lane) stays the EXECUTOR. The Go client lives in
`pkg/uiauto/laya`; the operator surface is `ui-agent laya-decide`.

Same category as TypeSafe AI's Jev "System One" model (early access,
closed): fast structured decisions for agent action selection. Laya is
the self-hostable, open-weight option.

## Endpoints

| Route | Method | Payload |
|---|---|---|
| `/predict` | POST | `{"state": {...}, "questions": {...}}` → `{"answers": {...}}` |
| `/healthz` | GET | `{"ok": true, "laya": "<installed laya version>"}` |

`/healthz` reports the baked `laya` version so stamp drift between a
running container and the pinned build is observable without `exec`.

## Pinned versions

`torch` and `laya` are **pinned** as `Containerfile` build args; the full
transitive freeze is written to `/versions.txt` inside the image for
build-to-build diffing. An unpinned install makes the image
non-reproducible: a checkpoint or API change silently lands on the next
rebuild with nothing in the diff to review.

| Package | Pin | Notes |
|---|---|---|
| `torch` | `2.14.0` (CPU wheel) | CPU-only keeps the image ~2 GB vs 6+ GB CUDA |
| `laya` | `0.3.5` | English checkpoint `convaiinnovations/laya` baked in |

### Build and run

    make laya-image        # podman build -t localhost/hlxn-laya:latest containers/laya
    podman run --rm -p 127.0.0.1:8090:8080 localhost/hlxn-laya:latest &
    curl -s http://127.0.0.1:8090/healthz

The English checkpoint is baked at build time, so first start needs no
network.

### Upgrading

1. Bump `LAYA_VERSION` (and only if needed `TORCH_VERSION`) in the
   `Containerfile`.
2. Read the upstream [release notes](https://github.com/NandhaKishorM/laya/releases)
   for API or checkpoint changes.
3. Rebuild, check `/healthz`, run the smoke, and diff the freeze:

       podman build --layers containers/laya -t hlxn-laya:latest
       podman run --rm -p 127.0.0.1:8090:8080 -d hlxn-laya:latest
       curl -s http://127.0.0.1:8090/healthz
       podman exec <ctr> python /smoke.py
       podman exec <ctr> pip freeze   # diff against the previous build's /versions.txt

## CPU vs GPU: measured, and why CPU is the default

The image builds in two variants:

    podman build containers/laya -t laya:cpu                        # default: CPU wheels (~2 GB)
    podman build --build-arg TORCH_INDEX_URL=https://pypi.org/simple \
        containers/laya -t laya:gpu                                  # CUDA-bundled torch (~7 GB)

Measured with `scripts/laya_bench.py` (60 predicts, fixed payload, one
warmup) on the CPU container deployed for this framework:

| Runtime | Model loads | Peak VRAM | predict p50 | predict p95 |
|---|---|---|---|---|
| CPU (pinned torch CPU wheel) | yes | n/a | **566 ms** | **679 ms** |
| GPU, CUDA via CDI (RTX 3090) | yes (~26 s) | ~1.7 GiB | does not complete | does not complete |
| GPU, CUDA via CDI (RTX 2070) | yes | ~1.7 GiB | does not complete | does not complete |

On the GPU variants the model loads and allocates (~1.7 GiB) and plain
CUDA compute is healthy (10x 1024^2 matmul in 0.08 s), but the model's
forward pass never returns — a single predict exceeds 280 s on both cards,
with and without the flash/mem-efficient SDPA kernels. A faulthandler
dump puts the hang inside the forward call itself. This is a
torch 2.14 + WSL2 paravirtualised-CUDA interaction, not a laya bug: the
same wheel predicts in ~0.5 s on CPU.

Decision, by these numbers: **CPU is the deployed default.** The GPU
variant remains buildable for re-measurement whenever the host driver or
torch stack changes; if a future run completes, re-run
`scripts/laya_bench.py` against it and update this table. GPU-side
placement of this checkpoint was also measured to cost about 1.7 GiB of
VRAM when loaded, for anyone budgeting a shared card.

## Security constraint (test infra only)

The service binds `0.0.0.0:8080` inside the container and carries **no
authentication**. That is acceptable **only** while the published port is
bound to loopback (`-p 127.0.0.1:8090:8080`), making it unreachable
off-host. It must not gain a tailnet/LAN publication without adding a
bearer token at the service boundary.

## Verified smoke output (2026-09-21, container, CPU)

State: checkout page with `Payment failed - card declined` visible.

    next_action:  choice retry_payment @ 0.864
                  (change_card 0.054, assert_failure 0.053, escalate 0.028)
    error_visible: noul 0.843

That is the integration contract for the UI loop: page state in, typed
next-action + state assertion out — deterministic shape, nothing to parse,
nothing to hallucinate.

## Integration design (follow-up slices)

1. Executor captures page state (browser-use / CDP snapshot).
2. Decision questions built from the test plan (action choices, expected
   state assertions as noul/score questions).
3. `Router` or single-checkpoint `predict` returns the next action; the
   executor applies it; assertions write pass/fail evidence per iteration.
4. Guardrail presets (`laya.guard_questions()`) gate operator prompts and
   scraped content entering decisions.
