# Svelte Skills Migration

The repo now tracks per-agent Svelte skill links in:

- `.agents/skills/`
- `.claude/skills/`

Older clones may still have untracked generated directories at those same paths from the previous `make svelte-skills` workflow. Git will not replace those local directories with tracked symlinks during checkout or pull.

## One-time cleanup

Before checking out or pulling a revision that tracks the symlinks, remove the old generated directories:

```bash
rm -rf .agents/skills/svelte-code-writer \
  .agents/skills/svelte-core-bestpractices \
  .claude/skills/svelte-code-writer \
  .claude/skills/svelte-core-bestpractices
```

Then rerun:

```bash
make svelte-skills
```

## After migration

- `skills/` remains the checked-in shared source of truth.
- `make svelte-skills` refreshes shared skills, prunes removed upstream skills, and recreates per-agent symlinks.
- If you intentionally maintain a local override at one of the per-agent paths, the sync script leaves that existing non-symlink path in place.
