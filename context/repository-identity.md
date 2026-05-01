# Repository Identity

Repository identity in the backend is the tuple:

```text
platform_host + owner + name
```

`owner` and `name` are case-folded before storage and lookup. An empty
`platform_host` at the storage boundary means `github.com`; route inputs that
omit `platform_host` are different: they are asking whether `owner/name` is
enough for that operation.

## Server Boundary

HTTP handlers should resolve repositories through the repository identity
module in `internal/server/repo_identity.go`. Handlers should not repeat:

- whether `owner/name` is enough;
- when omitted `platform_host` must be rejected as ambiguous;
- how not-found and ambiguity errors are represented;
- how a repo row becomes the stable `repo_id` used for item lookups.

The server uses strict identity resolution for routes that operate on one
repository: if the caller omits `platform_host`, `owner/name` must resolve to
exactly one known repo. `errRepoAmbiguous` is a client input problem and should
map to HTTP 400 with a message asking for `platform_host`; it should not be
collapsed into a generic 500.

## Item Resolution

Resolve repo identity before resolving pull request or issue numbers. After a
repo is resolved, item lookup must use `repo_id + number`, not another
owner/name query. This keeps same-owner/name rows on different hosts from
crossing when stale database rows exist.

`ResolveLocalItem` intentionally treats a missing local repo row as "not found
locally" instead of a hard error so `/items/{number}/resolve` can fall through
to the sync path for tracked repositories.

Before that sync fallback runs, the handler must check configured repositories
as well as local database rows. If multiple tracked repos share the same
`owner/name`, a no-host `/items/{number}/resolve` request is ambiguous even
when SQLite has not seen either repo yet. In that case the server must return
HTTP 400 instead of calling the no-host sync path, because the syncer would
otherwise pick a host using first-match `hostFor` behavior.
