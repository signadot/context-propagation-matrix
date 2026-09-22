## What does this change?

## Which variants does it affect?

List variant ids (from `variants.yaml`) whose results could change, or
"none" for harness-only changes.

## Checklist

- [ ] Relay code still imports no header or tracing API on the request path
      (naive-app contract in `DESIGN.md`), unless this is a deliberate
      `in-code` variant
- [ ] Versions are pinned exactly (no ranges, no `latest`)
- [ ] `variants.yaml` updated if a variant was added, renamed or re-tiered
- [ ] Ran `./run.sh <variant>` locally for each affected variant and attached
      or summarised the resulting cells
- [ ] `annotations/` updated if a non-success cell gained or lost an
      explanation
