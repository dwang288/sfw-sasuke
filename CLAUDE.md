# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository. Read `ARCHITECTURE.md` for the full design rationale,
data model, and the phased migration checklist.

> **This repo is mid-migration.** It is moving from the **original** design (one
> global slash command per gif, files on local disk, static JSON config) to a
> **target** design (two processes — bot + self-service web app — with per-guild
> gifs, Discord OAuth, object storage, and a system-admin console). Both states
> are documented below. When current and target conflict, the target is the
> direction of travel — but the code on disk is still the original until the
> relevant migration phase lands. Don't reintroduce patterns marked "being
> replaced," and ask before guessing which world a change belongs to.

---

## What this is

`sfw-sasuke` is a Discord bot written in Go (`bwmarrin/discordgo`). It serves
image files (GIFs, PNGs, JPEGs) into servers via slash commands. The target adds
a self-service web app where users sign in with Discord and upload gifs scoped to
servers they belong to. Runs on a single Oracle Always-Free instance. "gif" is
the domain term, but assets are arbitrary images — key off `content_type`, not
file extension.

---

## CURRENT STATE (today's code)

How the bot works **right now**, before migration. Accurate until the phase that
rewrites each piece.

### Current code map
```
cmd/
  main.go      — entry point; loads env, creates discordgo session, calls Run()
  handlers.go  — builds slash commands and handlers from ConfigMap; serves files
pkg/config/
  config.go    — reads files-metadata.json into ConfigMap (map[string][]filesConfig)
env/
  files-metadata.json — declares each slash command: name, description, filenames[]
static/        — image assets served by the bot
```

### How it works
The `ConfigMap` drives everything: `buildCommands` and `buildHandlers` both
iterate over it to register Discord application commands and wire up their
response handlers. Each handler opens the files listed under `filenames`, detects
content type via `http.DetectContentType`, and attaches them to the Discord
interaction response. A command maps to one *or more* files (e.g. `sfw` → 4
crops). Commands are registered globally (or to one test guild via `-guild`) at
startup and **deleted at shutdown**.

All file paths are resolved relative to the executable using `getAbsolutePath`,
which calls `os.Executable()` — this matters when the binary is run from a
directory other than the one it lives in.

### Current command workflow — being replaced (Phase 2)
Today, adding a command is: (1) add the image to `static/`, (2) add an entry to
`env/files-metadata.json` with `name`, `description`, `filenames`. No Go changes.
**Do not extend this pattern** — Phase 2 replaces per-gif command registration
with a single dynamic `/gif` command and moves config into the DB. The JSON file
becomes a one-time seed input for `cmd/seed`.

---

## TARGET STATE (what we're migrating to)

### Summary
- **Two processes, one module:** `cmd/bot` (gateway) and `cmd/web` (OAuth + CRUD +
  frontend + `/admin`), plus `cmd/seed` (one-off migration). Shared logic in
  `internal/`. Two processes so a web deploy never drops the gateway connection.
- **One dynamic command:** a single global `/gif name:<autocomplete>` — NOT one
  command per gif. Autocomplete and lookup are scoped by `interaction.GuildID`
  and read from the DB. Uploads add rows; they never register commands.
- **Storage:** private S3-compatible bucket (Cloudflare R2 or Oracle) for bytes;
  SQLite (WAL + `busy_timeout`) for metadata. Gif bytes never go in the DB.
- **Delivery (members-only):** bot downloads objects and uploads them as Discord
  attachments. Web previews go through short-lived presigned URLs or an
  authenticated proxy. The bucket is never public.
- **Auth:** Discord OAuth (`identify`, `guilds`); server-side sessions; the
  `guilds` scope's `permissions` bitfield gives `MANAGE_GUILD` (`0x20`).

### Target directory map
```
cmd/bot/    gateway, /gif + autocomplete, guild upsert on join/leave
cmd/web/    OAuth, CRUD API, frontend, /admin console
cmd/seed/   migrate files-metadata.json + static/ -> bucket + DB
internal/store/     DB repo + embedded migrations
internal/storage/   S3 client (get/put/presign/delete)
internal/discord/   OAuth helpers, permission bits, guild intersection
internal/authz/     Can(), roles, system-admin bootstrap
internal/model/     domain types
internal/web/       handlers, sessions, templates
web/templates/      html/template
web/static/         css/js (htmx)
```

### Write-ownership split (important for SQLite)
- **`cmd/web` owns** writes to `users`, `gifs`, `gif_files`, `guild_settings`,
  `audit_log`.
- **`cmd/bot` owns** writes to `guilds` only (upsert on `GuildCreate`, remove on
  `GuildDelete`); it reads everything else.

Keeping writes from overlapping is what makes two-process SQLite safe here. Don't
add bot-side writes to web-owned tables without revisiting this.

### Authorization
All access decisions go through `internal/authz.Can(session, action, guildID)`.
Two authority axes: per-guild (`MANAGE_GUILD`, gif ownership, `upload_policy`) and
**system admin** (operator of the whole service, full cross-guild CRUD + usage).
See the capability table in `ARCHITECTURE.md` §3. System admin is bootstrapped
from `SYSTEM_ADMIN_IDS` (env) — NOT a DB flag, and must not be made editable
in-app. The `/admin` area is a separate route group with its own guard and a
guild picker over all guilds.

---

## Invariants — do not violate

1. Never trust a client-supplied `guild_id`; re-verify membership + permissions
   server-side on every mutating request.
2. Re-check `IsSystemAdmin` server-side on every `/admin` route. Hiding UI is not
   access control.
3. System admin comes only from `SYSTEM_ADMIN_IDS`.
4. The bot scopes all gif lookups by `interaction.GuildID`.
5. Deleting a gif must also delete its bucket objects (no orphans — this keeps
   storage-usage numbers accurate).
6. Bot command/autocomplete handlers must respond with an ephemeral error on a
   bad lookup — never `log.Fatal`/panic. (The current `checkErr` → `log.Fatal`
   pattern is fine at startup, never in handlers.)
7. The bucket stays private. Bot streams attachments; web previews use presigned
   URLs (short expiry) or authenticated proxying.
8. Validate the OAuth `state` param on callback.
9. Validate uploads: magic-byte content-type sniff + size limit; rate-limit them.
10. Write `audit_log` entries for destructive admin actions.

---

## Conventions

- Go 1.22+. Prefer `log/slog` for structured logging (pairs with the audit need).
- A gif name maps to one or more files (`gif_files`, ordered by `ordinal`);
  preserve this — the original `sfw` command is four images.
- `(guild_id, name)` is unique; that's how users invoke a gif.
- Keep handlers thin; DB access in `internal/store`, object access in
  `internal/storage`, decisions in `internal/authz`.
- Don't reintroduce per-gif command registration or shutdown-time command
  deletion.

---

## Build and run

### Today (original single binary)
```sh
# Build
cd cmd && go build -o ../sfw-sasuke
# Run locally (loads env files from ./env/)
./sfw-sasuke -use-env-file <any-value>
# Run against a specific test guild only (avoids global command propagation delay)
./sfw-sasuke -use-env-file 1 -guild <GUILD_ID>
# Docker Compose (pulls latest image from Docker Hub)
docker compose up
```

### After the cmd/ split (Phase 0+)
```sh
go build ./...
go run ./cmd/bot         # needs env loaded
go run ./cmd/web
go run ./cmd/seed        # one-off migration of existing assets
go test ./...
go vet ./... && gofmt -l .
# migrations: goose up   (or the chosen runner)
```

---

## Environment / secrets

### Current (`env/`)
- `secrets.env` — `BOT_TOKEN=<discord-bot-token>` (never committed)
- `config.env` — `ASSETS_DIR=static` and `CMD_METADATA_PATH=env/files-metadata.json`

In container deployments, `ASSETS_DIR` and `CMD_METADATA_PATH` are set via `ENV`
in the Dockerfile; `secrets.env` is always loaded from the bind-mounted `env/`
volume.

### Target additions
```
DISCORD_CLIENT_ID         # OAuth client (same Discord app as BOT_TOKEN)
DISCORD_CLIENT_SECRET
DISCORD_REDIRECT_URI
WEB_BASE_URL              # public base URL of the web app
SESSION_SECRET            # server-side session signing
SYSTEM_ADMIN_IDS          # comma-separated Discord user IDs
S3_ENDPOINT               # R2 / Oracle endpoint
S3_REGION
S3_BUCKET
S3_ACCESS_KEY_ID
S3_SECRET_ACCESS_KEY
DATABASE_PATH             # sqlite file path (persistent volume)
```
`ASSETS_DIR` / `CMD_METADATA_PATH` become legacy — used only by `cmd/seed`,
retire after migration.

---

## CI / deployment

### Current
- CI (`.github/workflows/main.yml`) builds a multi-arch Docker image
  (`linux/amd64,linux/arm64`) and pushes to Docker Hub as
  `fridaymove/sfwsasuke:latest` on every push to `main`.
- Production runs via `docker compose up` pulling that image, with `./env` and
  `./static` bind-mounted.
- Alternate deployment: systemd unit in `infra/sfw-sasuke.service` (runs as user
  `opc`, passes the env-file flag).

### Target additions
- Build **both** binaries in CI (`cmd/bot`, `cmd/web`).
- Run `cmd/web` behind Caddy (auto-TLS) as a second systemd unit (or second
  docker-compose service).
- Add the new env/secrets above; ensure the SQLite file lives on a persistent
  path/volume.

---

## Settled decisions

- Default `upload_policy` is `manage_guild` — uploads are admin-gated by default;
  an admin can loosen a guild to `everyone`.
- Existing gifs are seeded under the **origin guild's ID** (no global/built-in
  flag; the model is strictly per-guild).
- `SYSTEM_ADMIN_IDS` must be provided before `/admin` is usable.

---

## Suggested dependencies (not yet locked — confirm before adding)

`modernc.org/sqlite` (no-cgo) · `pressly/goose` · `aws-sdk-go-v2` S3 (or
`minio-go`) · `golang.org/x/oauth2` · `alexedwards/scs` · stdlib `net/http` 1.22
routing or `go-chi/chi` · `html/template` + htmx.

---

## Housekeeping in the existing code

- Remove the stale `replace github.com/dwang288/sfw-sasuke/config => ./config`
  in `go.mod` (points at a nonexistent dir; real code is `pkg/config`).
- The `v := v` loop-variable workaround can go once on Go 1.22+.
