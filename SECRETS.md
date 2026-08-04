# Secrets and provider configuration

## Required environment

| Variable | Where it must live | Notes |
| --- | --- | --- |
| `DATABASE_URL` | **Fly secret** | Postgres connection string, contains a password. Never in `fly.toml`. |
| `anon_key` | **Fly secret** | Supabase anon JWT, used to verify user tokens. Never in `fly.toml`. |
| `OPENAI_API_KEY` | **Fly secret** | Required when `AI_PROVIDER` is unset or `openai`. |
| `ANTHROPIC_API_KEY` | **Fly secret** | Required when `AI_PROVIDER=anthropic`. Also enables `/agent/import-plan`. |
| `API_URL` | `fly.toml [env]` | Public base URL. Not a credential. |
| `LOG_LEVEL` | `fly.toml [env]` | Not a credential. |
| `AI_PROVIDER` | `fly.toml [env]` or unset | `openai` (default), `anthropic`, or `mock`. |

Set secrets with:

```bash
fly secrets set DATABASE_URL='...' anon_key='...' OPENAI_API_KEY='...'
```

Fly injects secrets into the machine environment exactly like `[env]` values, so
application code reading `os.Getenv` is identical either way. The only
difference is that `[env]` is committed to git and secrets are not.

## Startup validation

`agent.ValidateProviderConfig()` runs in `main()` before the server binds. It
fails the process when:

- `AI_PROVIDER` names a backend this build does not implement;
- `AI_PROVIDER=mock` is used on a hosted runtime (detected via `FLY_APP_NAME`) —
  the mock backend returns fabricated plans and must never reach real users;
- the selected hosted provider's key variable is unset, empty, or implausibly
  short.

Errors name the offending **variable**, never any part of a value. Nothing in
this codebase logs a key, a key prefix, or a key length.

## Local development

```bash
AI_PROVIDER=mock DATABASE_URL='postgresql://postgres:postgres@127.0.0.1:54322/postgres' go run main.go
```

`AI_PROVIDER=mock` needs no key and makes no network call. It is refused on Fly.

## Rotation history

`fly.toml` previously carried literal `DATABASE_URL` and `anon_key` values in
its `[env]` block. Those literals were removed from the working tree but remain
permanently reachable in this repository's git history (introduced in `0e31082`
and `958de77`). **Both credentials must be treated as exposed and rotated by a
human with Supabase and Fly access.** Rewriting history is not a fix — the
values must be changed at the source.
