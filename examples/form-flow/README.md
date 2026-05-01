# form-flow example

Local visible-browser demo for the `ui-agent` CLI. It serves a small HTML page
from this directory and runs a deterministic scenario that exercises:

- `verify` for visible assertions.
- `type` for text input.
- `click` for form submission.
- `wait` for async UI state.

## Run

Start Chrome or Chromium with a DevTools port:

```bash
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
  --remote-debugging-port=9222 \
  --user-data-dir=/tmp/chrome-uiauto
```

Then run:

```bash
./scripts/run-form-flow-demo.sh
```

The script serves `index.html` at `http://127.0.0.1:8018/`, runs
`form-flow-001`, and writes run artifacts to `~/uiauto/tests/`.

## Expected result

The browser should visibly load the form, type `Ada Lovelace`, click submit,
and show the greeting result. Raw and annotated screenshots are written under
the scenario run directory for review.
