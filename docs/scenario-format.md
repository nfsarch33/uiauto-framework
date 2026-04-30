# Scenario JSON format

A scenario file is a JSON array of one or more scenario objects. Each scenario
is a self-contained sequence of natural-language steps with parallel arrays
for action types, values, and selectors.

## Schema

```json
[
  {
    "id": "smoke-001",
    "name": "Example.com smoke",
    "natural_language": [
      "Verify the example.com landing heading is present",
      "Click the More information link in the body"
    ],
    "page_objects": ["page.heading", "page.more_link"],
    "selectors_used": ["h1", "a[href*=\"iana.org\"]"],
    "source": "framework",
    "tags": ["smoke", "navigation"],
    "action_types": ["verify", "click"],
    "action_values": ["", ""]
  }
]
```

| Field | Required | Description |
|---|---|---|
| `id` | yes | Unique scenario identifier. |
| `name` | yes | Human-readable label. |
| `natural_language` | yes | Per-step instruction in plain English. |
| `selectors_used` | yes | Parallel CSS or XPath selectors per step. |
| `action_types` | no | Parallel action types. Defaults to `click`. Allowed: `click`, `type`, `verify`, `wait`, `frame`, `evaluate`, `read`. |
| `action_values` | no | Parallel per-step values (text to type, selector to wait for, etc.). |
| `page_objects` | no | Logical page-object names for documentation only. |
| `source` | no | Origin (e.g. "framework", "manual", "imported-from-x"). |
| `tags` | no | Free-form tags for filtering. |

## Action types

| Type | Behaviour |
|---|---|
| `click` | Click the resolved element. |
| `type` | Type `action_values[i]` into the resolved element. |
| `verify` | Pass if the selector resolves to a visible element within 2s. |
| `wait` | Block until `action_values[i]` (a selector) becomes visible, or step times out. |
| `frame` | Switch execution context to the iframe at the resolved selector. Use `Children` actions in code, or follow with steps that target elements inside the frame. |
| `evaluate` | Run JavaScript via CDP. Result captured to scenario telemetry. |
| `read` | Read text content of the resolved element. |

## Defaults

When `action_types` is shorter than `natural_language`, missing entries
default to `click`. When `action_values` is shorter, missing entries default
to the empty string.

## Tips

- Keep `natural_language` focused on what a user would say (no CSS, no JS).
- Update `selectors_used` whenever the target page changes; the Light tier
  caches them.
- Use `verify` liberally to assert intermediate states; cheap to run, makes
  failures specific.
