---
title: Source settings
editUrl: https://github.com/alampros/jargonary/edit/main/docs/sources.md
---

See the [annotated config](../examples/config.yaml) for all settings.

## Run with one source

```sh
./jargonary -f ./terms/payments.yaml
./jargonary -input-file ./terms/payments.json
```

`-f` and `-input-file` accept a single local glossary file, replace configured sources and discovery, and hide the bundled project glossary. The source ID is `glossary`. Format is inferred from the file extension, defaulting to JSON; `-format yaml` or `-format json` overrides it. File content is polled using the normal source rules.

You can combine it with `-config` for other settings. The source file path resolves from the working directory, even when the config is elsewhere. Configured exports must reference `glossary` or omit source selectors. For HTTP(S) or Git sources, use a configured source instead.

## Read from stdin

Use a literal `-` to read one complete glossary from stdin:

```sh
# Validate and exit, returning a nonzero status on invalid input.
./jargonary -validate - -format yaml < terms/payments.yaml

# Serve a generated JSON glossary in the web UI.
generate-glossary | ./jargonary -f -

# Serve YAML from stdin with other settings from a config file.
./jargonary -config config.yaml -f - -format yaml < terms/payments.yaml
```

Stdin defaults to JSON; use `-format yaml` for YAML. Input is read only when explicitly requested with `-`. Serving waits for EOF, validates the complete document, and loads it before opening listeners. Invalid, empty, or oversized input fails startup; a valid glossary with `entries: []` serves an empty snapshot. The `max_bytes` limit applies when serving.

The stdin snapshot uses source ID `glossary`, replaces configured sources and discovery, and hides the bundled project glossary, just like `-f` with a file. It is never polled or reread; the server keeps running until stopped. Restart with new input to replace it. `-validate` exits after validation and cannot be combined with `-f` or `-input-file`.

For Docker, keep stdin open with `-i` and omit `-t`:

```sh
docker run --rm -i -p 8080:8080 ghcr.io/alampros/jargonary:0.2.0 -f - -format yaml < terms/payments.yaml
```

## Git repositories

```yaml
sources:
  - id: jargonary
    git: alampros/jargonary
    path: glossary/project.json
  - id: platform
    git: git@example.com:platform/vocabulary.git
    path: glossary.yaml
    ref: main
```

Choose exactly one of `file`, `url`, or `git` per source. `git` accepts GitHub shorthand (`owner/repo`, expanded to `https://github.com/owner/repo.git`) or any fully qualified repository URI understood by Git, including HTTPS, SSH, SCP-style addresses, and remote helpers. URIs are passed directly to the external `git` executable without a shell.

`path` names one committed regular file relative to the repository root and defaults to `glossary.yaml`. Set it to `glossary.json` for a root JSON glossary, or a nested path as above. Symlinks and submodules are not followed. Format is inferred from this path, defaulting to JSON for extensionless paths; `format` overrides it. `ref` selects a branch, tag, or commit and defaults to the remote `HEAD`.

Each poll fetches a shallow snapshot into a temporary bare repository, reads the file, and removes the temporary repository. No working tree is checked out. Changes, invalid content, empty glossaries, and failures follow the same snapshot rules as other sources. The fetch and file read share the configured `timeout`; `max_bytes` limits the glossary content, not the repository transfer.

Git sources require `git` on the application's `PATH` and a writable temporary directory. Git inherits the process environment and uses its own configuration, credential helpers, SSH agent, SSH configuration, proxies, and URL rewriting. Configure authentication and routing for the user running Jargonary; `auth.shared_secret` is only for HTTP sources. Repository URIs and Git stderr are omitted from diagnostics to avoid exposing credentials.

The stock scratch-based Docker images do not include Git. To use Git sources in a container, supply a runtime image with Git and any required transport programs (such as SSH), plus the appropriate Git configuration and credentials for the runtime user.

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
