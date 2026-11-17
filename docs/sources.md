---
title: Source settings
editUrl: https://github.com/alampros/jargonary/edit/main/docs/sources.md
---

See the [annotated config](../examples/config.yaml) for all settings.

## Run with one source

```sh
./jargonary -f ./terms/payments.yaml
./jargonary --input-file ./terms/payments.json
```

`-f` and `--input-file` accept a single local glossary file, replace configured sources and discovery, and hide the bundled project glossary. The source ID is `glossary`. Format is inferred from the file extension, defaulting to JSON. Content is polled using the normal source rules.

You can combine it with `-config` for other settings. The source file path resolves from the working directory, even when the config is elsewhere. Configured exports must reference `glossary` or omit source selectors. For HTTP(S) sources, authentication, or an explicit format override, use a configured source instead.

## Polling and limits

| Setting | Default | Range |
| --- | --- | --- |
| `scrape_interval` | `5m` | `1m`–`24h`; overridable per source |
| `timeout` | `15s` | `1s`–`1m` per attempt |
| `max_bytes` | `8388608` (8 MiB) | 1 byte–64 MiB |

Format comes from the file/URL extension, defaulting to JSON; `format` overrides it. HTTP redirects are rejected.

Up to four sources fetch concurrently. Failed fetches retry with backoff and retain the last accepted snapshot; a valid empty glossary clears it. Removing a configured source retires its cache on restart. Keep source IDs stable; identical entry IDs across sources remain distinct.

## Discover local files

Omitting `sources` loads `.json`, `.yaml`, and `.yml` files directly in `terms_dir` (default `/terms`). Filenames without extensions become source IDs and must be unique. Subdirectories and symlinks are ignored.

Discovery also looks for `glossary.yaml` and `glossary.json` in the current working directory, even when the config file is elsewhere. These use source ID `glossary`; keep only one format and avoid another discovered file with that ID. A file already found through `terms_dir` is loaded only once.

Discovery runs at startup. Deleting a file while running retains its cache. Explicit `sources` replaces discovery; `sources: []` disables it.

## Protected sources

```yaml
sources:
  - id: payments
    url: https://payments.internal/.well-known/glossary
    auth:
      shared_secret:
        file: /run/secrets/payments
        # Or: env: PAYMENTS_SECRET
```

Authenticated URLs require HTTPS; requests send `Authorization: Bearer <secret>`. Use random secrets of at least 32 non-whitespace characters. Secret files are trimmed; rotation requires a restart. Only operators should configure URLs, which can reach internal networks.

## Bundled project glossary

Source ID `project` is reserved. `hide_jargonary_glossary: true` hides it when company sources exist; with none configured or discovered, it remains the fallback. Empty or failed scrapes do not activate the fallback.

The root `/.well-known/glossary` serves this glossary, or 404 when disabled. Company content uses [named exports](publishing.md). The path is a proposed convention, not a registered standard or automatic network discovery.
