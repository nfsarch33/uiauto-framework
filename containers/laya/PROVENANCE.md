# Provenance — the laya decision-layer container

- Source: https://github.com/NandhaKishorM/laya (Apache-2.0, Convai Innovations), distributed via PyPI as `laya`
- Role: DECISION layer only (choice / score / noul, single forward pass) — never the executor; browser-use or the CDP lane executes
- Installed: pinned in this directory's Containerfile (`LAYA_VERSION` build arg), nothing vendored from the upstream repo — the image installs the PyPI wheel at build time and freezes the full transitive set into `/versions.txt` for build-to-build diffing
- Pin: laya 0.3.5 (PyPI, recorded 2026-10-01)
  - wheel `laya-0.3.5-py3-none-any.whl` sha256 `4c57f64cbaf893bb5c7b4affddc2bf21a819f55df51941689f11868583be2903`
  - sdist `laya-0.3.5.tar.gz` sha256 `5e8a4c2b38dbddc0febe7443f26f74fd9d7571fb172648b1481830c667d59219`
- Torch pin (the other half of the reproducible image): `TORCH_VERSION=2.14.0` from `https://download.pytorch.org/whl/cpu` — 24 wheels published for that version; the CPU wheel that this image actually resolves is recorded per-build in `/versions.txt` (the container-registry digest is the immutable record for a built image)
- Review: read before pinning — typed-decision API (no text generation), CPU-only torch build, `/healthz` reports the baked laya version so pin drift is observable without exec; no network egress beyond PyPI/pytorch.org at BUILD time, the served container listens on its port only
- Next review: 2027-01-01 (quarterly, aligned with the vendored-skills cycle), or on any upstream laya release touching the `choice`/`score`/`noul` contracts, or any torch security advisory in the pinned line

To update: bump `LAYA_VERSION` (and the sha256 lines above from the PyPI JSON API — never from memory), rebuild, diff `/versions.txt`, check `/healthz` reports the new version, and re-run `scripts/laya_bench.py` against the recorded baseline.
