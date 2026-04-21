---
name: svelte-code-writer
description: CLI tools for Svelte 5 documentation lookup and code analysis. MUST be used whenever creating, editing or analyzing any Svelte component (.svelte) or Svelte module (.svelte.ts/.svelte.js). If possible, this skill should be executed within the svelte-file-editor agent for optimal results.
---

# Svelte 5.55.0 Code Writer

## CLI Tools

This repo currently uses Svelte `5.55.0`.

You have access to pinned `@sveltejs/mcp@0.1.22` CLI for Svelte-specific assistance. Use these commands via `npx`:

### List Documentation Sections

```bash
npx @sveltejs/mcp@0.1.22 list-sections
```

Lists all available Svelte 5 and SvelteKit documentation sections with titles and paths.

### Get Documentation

```bash
npx @sveltejs/mcp@0.1.22 get-documentation "<section1>,<section2>,..."
```

Retrieves full documentation for specified sections. Use after `list-sections` to fetch relevant docs.

**Example:**

```bash
npx @sveltejs/mcp@0.1.22 get-documentation "$state,$derived,$effect"
```

### Svelte Autofixer

```bash
npx @sveltejs/mcp@0.1.22 svelte-autofixer "<code_or_path>" [options]
```

Analyzes Svelte code and suggests fixes for common issues.

**Options:**

- `--async` - Enable async Svelte mode (default: false)
- `--svelte-version` - Target version: 4 or 5 (default: 5). For this repo, use 5 because project Svelte version is 5.55.0.

**Examples:**

```bash
# Analyze inline code (escape $ as \$)
npx @sveltejs/mcp@0.1.22 svelte-autofixer '<script>let count = \$state(0);</script>' --svelte-version 5

# Analyze a file
npx @sveltejs/mcp@0.1.22 svelte-autofixer ./src/lib/Component.svelte --svelte-version 5

# Target Svelte 4
npx @sveltejs/mcp@0.1.22 svelte-autofixer ./Component.svelte --svelte-version 4
```

**Important:** When passing code with runes (`$state`, `$derived`, etc.) via the terminal, escape the `$` character as `\$` to prevent shell variable substitution.

## Workflow

1. **Uncertain about syntax?** Run pinned `list-sections` then `get-documentation` for relevant topics
2. **Reviewing/debugging?** Run pinned `svelte-autofixer` on the code to detect issues
3. **Always validate** - Run `svelte-autofixer` with `--svelte-version 5` before finalizing any Svelte component in this repo
