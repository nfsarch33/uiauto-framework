# Laya decision layer (containerized)

[Laya](https://github.com/NandhaKishorM/laya) (Apache-2.0) is a typed-decision
engine, NOT a browser driver: given a state dict and questions, it returns
structured decisions — `choice` (label + calibrated probability distribution),
`score` (ordinal rubric), `noul` (yes/no probability) — in a single forward
pass with no text generation. This framework uses it as the DECISION layer;
browser-use (or the CDP lane) stays the EXECUTOR.

Same category as TypeSafe AI's Jev "System One" model (early access, closed):
fast structured decisions for agent action selection. Laya is the
self-hostable, open-weight option.

## Build and run

    podman build --layers containers/laya -t hlxn-laya:latest
    podman run --rm hlxn-laya:latest

CPU-only torch keeps the image ~2 GB. The English checkpoint
(`convaiinnovations/laya`) is baked at build time, so first start needs no
network.

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
