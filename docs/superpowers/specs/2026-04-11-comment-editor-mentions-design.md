# Comment Editor Mentions Design

## Goal

Make the pull request and issue comment editors feel closer to GitHub by adding inline autocomplete for `@username` mentions and `#` references, while keeping the editor visually and behaviorally close to a normal plain-text markdown box.

## Constraints

- The editor remains plain-text-first. We do not want a rich text surface, formatting toolbar, or rendered markdown editing experience.
- Submitted comment bodies must remain plain markdown strings.
- Existing draft persistence and `Cmd`/`Ctrl+Enter` submission behavior must remain intact.
- Suggestion data must come from the existing SQLite data already synced into the app.
- `#` completion should search both pull requests and issues within the current repository.

## Chosen Approach

Use a shared Tiptap-based comment editor component, but configure it to behave like a plain markdown text box. Tiptap is used only for caret-aware suggestion handling, popup state, and keyboard interactions. Mention selections insert literal text such as `@alice` and `#123` into the editor content instead of introducing rich mention nodes that would require custom markdown serialization.

This keeps the editor upgrade narrowly focused on GitHub-style completion while avoiding a broad migration to rich text editing.

## Alternatives Considered

### 1. Keep the textarea and build custom autocomplete

This would be the smallest dependency change and would preserve the plain-text model naturally. It was rejected because caret positioning, popup anchoring, and keyboard behavior inside a multiline textarea would require more custom interaction code than we want to carry.

### 2. Adopt a fuller markdown editor stack

This would open the door to richer formatting features, but it would solve the wrong problem. We do not want preview-style editing or a different storage model, and it would add complexity unrelated to mentions.

## Editor Behavior

### Shared editor component

- Replace the duplicated PR and issue comment textarea markup with a shared `CommentEditor` component used by both existing comment box wrappers.
- The wrappers continue to own submission, pending state, and error handling so the rest of the detail flow stays unchanged.

### Plain-text presentation

- Style the Tiptap editor to look like the existing textarea: same spacing, border, placeholder, minimum height, and simple vertical growth behavior.
- Do not add formatting controls, inline markdown rendering, or GitHub-style decorated prose output.
- Preserve line breaks and direct text entry expectations.

### Mention and reference triggers

- Typing `@` opens username suggestions.
- Typing `#` opens issue and pull request suggestions from the current repo.
- Suggestions stay scoped to the active token near the caret instead of scanning the entire document.
- Matching should be case-insensitive for usernames and title text.

### Selection behavior

- Arrow keys move through the list.
- `Enter` or `Tab` accepts the highlighted suggestion when the popup is open.
- `Escape` closes the popup.
- Mouse click selects a suggestion.
- `Cmd`/`Ctrl+Enter` still submits the comment.
- Accepting a suggestion inserts plain text and leaves the caret in a natural typing position after the inserted token.

## Data Source and API

Add a repo-scoped autocomplete endpoint backed by existing database data.

### Username suggestions

Collect distinct usernames already present in the repo from:

- pull request authors
- issue authors
- pull request event authors
- issue event authors

The endpoint filters by the typed text after `@`, de-duplicates results, and returns a capped list ordered by direct prefix matches first, then most recent observed activity in the repo, then login for stable ties.

### `#` suggestions

Collect both pull requests and issues from the current repo and filter by:

- numeric prefix after `#`
- title substring matches after `#`

Each result includes:

- item type: pull request or issue
- number
- title
- state

The inserted text remains `#<number>`, while the popup label shows type and title for context.

## Frontend Data Flow

- When a comment detail view is active, the editor requests suggestions only for the current repository.
- Suggestion loading is driven by the active trigger token rather than preloading all possible users and items into global state.
- Responses are small and capped to keep the popup responsive.
- In-flight suggestion requests should be superseded by newer keystrokes so stale responses do not overwrite newer result sets.

## Backend Shape

- Add a Huma route under the existing repo-scoped API namespace for comment autocomplete suggestions.
- Add database query helpers that return repo-scoped username and item suggestions.
- Keep the response shape simple and purpose-built for the editor instead of exposing raw DB records.

## Testing

### Backend

- Database query tests for username de-duplication and repo scoping.
- Database query tests for mixed issue and pull request `#` suggestions.
- API tests for route behavior, filtering, and response shape.

### Frontend

- Component tests for `@` suggestion popup opening and selection.
- Component tests for `#` suggestion popup opening and selection.
- Keyboard interaction tests for arrow keys, accept, and dismiss.
- Regression tests ensuring existing draft persistence still works across remounts and item switches.

### End-to-end

- Add e2e coverage that exercises the real HTTP API and SQLite-backed suggestion data through the comment editor.

## Out of Scope

- Markdown preview
- Rich text formatting controls
- Slash commands
- Emoji pickers
- Cross-repository `#` references
- Persisting structured mention metadata

## Implementation Notes

- Prefer literal text insertion over rich mention nodes to avoid markdown serialization drift.
- Keep the new editor component narrowly scoped so it can be reused by both comment boxes without changing surrounding store contracts.
- If Tiptap's mention extension assumes node-based rendering too strongly, fall back to its suggestion utilities while still inserting plain text from the command handler.
