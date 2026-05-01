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

Use `repoLookupOwnerNameAllowed` for legacy routes where the path only carries
`owner/name` and the existing behavior is to select the deterministic
owner/name match from the database. Use
`repoLookupRequireUnambiguousOwnerName` for mutations where the target repo
must be unambiguous unless the caller provides `platform_host`.

## Item Resolution

Resolve repo identity before resolving pull request or issue numbers. After a
repo is resolved, item lookup must use `repo_id + number`, not another
owner/name query. This keeps same-owner/name rows on different hosts from
crossing when stale database rows exist.

`ResolveLocalItem` intentionally treats a missing local repo row as "not found
locally" instead of a hard error so `/items/{number}/resolve` can fall through
to the sync path for tracked repositories.
