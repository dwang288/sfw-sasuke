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
cmd/bot/
  main.go      — entry point; loads env, creates discordgo session, calls serve()
  handlers.go  — builds slash commands and handlers from config.Map; serves files
pkg/config/
  config.go    — reads files-metadata.json into config.Map (map[string][]filesConfig)
env/
  files-metadata.json — declares each slash command: name, description, filenames[]
static/        — image assets served by the bot
```

### How it works
The `config.Map` drives everything: `buildCommands` and `buildHandlers` both
iterate over it to register Discord application commands and wire up their
response handlers. Each handler opens the files listed under `filenames`, detects
content type via `http.DetectContentType`, and attaches them to the Discord
interaction response. A command maps to one *or more* files (e.g. `sfw` → 4
crops). Commands are registered globally (or to one test guild via `-guild`) at
startup and **deleted at shutdown**.

All file paths are resolved relative to the working directory using
`filepath.Abs` — this matters when the process is
run from a directory other than the one containing `env/`/`static/` (the
systemd unit and Dockerfile both set their working directory to match, so this
is transparent in deployment; it's also why `go run ./cmd/bot` works from the
repo root even though `go run` compiles to an unrelated temp directory).

### Current command workflow — being replaced (build order step 7)
Today, adding a command is: (1) add the image to `static/`, (2) add an entry to
`env/files-metadata.json` with `name`, `description`, `filenames`. No Go changes.
**Do not extend this pattern** — the dynamic `/gif` step (build order step 7)
replaces per-gif command registration
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
  Postgres (managed free tier, off-box) for metadata. Gif bytes never go in the DB.
- **Delivery (members-only):** bot downloads objects and uploads them as Discord
  attachments. Web previews go through short-lived presigned URLs or an
  authenticated proxy. The bucket is never public.
- **Hexagonal (ports & adapters):** `internal/core` (domain + ports + app
  services) holds all logic and imports no provider SDK; `internal/adapter` holds
  the swappable implementations (postgres, objstore, discord). DB and object store
  sit behind the `Store` and `BlobStore` ports so they can be swapped without
  touching `core`.
- **Auth:** Discord OAuth (`identify`, `guilds`); server-side sessions; the
  `guilds` scope's `permissions` bitfield gives `MANAGE_GUILD` (`0x20`).

### Target directory map
```
cmd/bot/     composition root: wire adapters -> core, run the bot
cmd/web/     composition root: wire adapters -> core, run the web app
cmd/seed/    composition root: one-off migration

internal/core/                 NO provider-SDK imports anywhere under core/
  domain/    entities, value objects, domain errors (no db:/json: tags)
  port/      Store, Repos, *Repository, BlobStore, Presigner (optional),
             DiscordDirectory (driven ports)
  app/       GifService, AdminService, Authz(Can), UploadService (use cases)
internal/adapter/              ONLY place provider SDKs are imported
  postgres/  implements Store/*Repository (jackc/pgx v5)
  objstore/  implements BlobStore, S3-compatible (aws-sdk-go-v2)
  discordapi/implements DiscordDirectory (OAuth exchange, guild list)
  memory/    in-memory ports for fast core tests
  discordbot/DRIVING adapter: gateway + /gif + autocomplete -> app
  httpweb/   DRIVING adapter: OAuth + HTTP handlers + templates -> app

web/templates/   html/template
web/static/      css/js (htmx)
```

Swapping Postgres→SQLite or S3→localfs = one new package under `adapter/` plus one
changed constructor line in each `cmd/*/main.go`. `core/` does not change.

### Which process writes what (incidental, not a safety constraint)
Postgres handles concurrent writers natively (MVCC + row locks), so both processes
can safely read and write any table — there is no SQLite-style locking concern. The
division below is just which process receives which events, not a rule that makes
concurrency safe:
- **`cmd/web`** does user/gif/settings writes (login, upload, config).
- **`cmd/bot`** does guild-lifecycle writes (upsert on `GuildCreate`, remove on
  `GuildDelete`), since it's the process receiving those gateway events.

Either process may read anything. Nothing breaks if this division shifts.

### Authorization
All access decisions go through `core/app` `Authz.Can(session, action, guildID)`.
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
11. Keep hexagonal layering: no provider SDK imported outside `internal/adapter/`;
    no driver types or `db:`/`json:` tags in `internal/core`; `core` never imports
    an adapter; adapters are constructed only in `cmd/*/main.go`. Adapters
    translate driver errors to domain errors (e.g. `pgx.ErrNoRows` →
    `domain.ErrGifNotFound`).

---

## Conventions

- Go 1.22+. Prefer `log/slog` for structured logging (pairs with the audit need).
- A gif name maps to one or more files (`gif_files`, ordered by `ordinal`);
  preserve this — the original `sfw` command is four images.
- `(guild_id, name)` is unique; that's how users invoke a gif.
- Keep handlers thin. Logic lives in `core/app`; persistence behind the `Store`
  port (`adapter/postgres`); object bytes behind the `BlobStore` port
  (`adapter/objstore`); authz decisions in `core/app` `Authz`.
- Define ports in `core/port` using `domain` + stdlib types only — never expose a
  `*sql.DB`, an `s3.Client`, or a driver error through a port.
- Multi-table writes go through `Store.Tx`. DB row + bucket object can't be atomic
  (object stores aren't transactional) — orchestrate in the app service and lean
  on the reconciliation job for partial failures.
- Add an `adapter/memory` fake when introducing a new port, so `core/app` stays
  testable without a DB or bucket.
- Don't reintroduce per-gif command registration or shutdown-time command
  deletion.
- A guild may have no `guild_settings` row; treat a missing row as
  `upload_policy = manage_guild` (don't assume the row exists). Legacy seeded gifs
  need a synthetic "system" `users` row since `uploader_user_id` is NOT NULL.
- Web preview membership is best-effort: it's checked against the OAuth session's
  guild snapshot, so a kicked user keeps access until refresh. The bot delivery
  path is gated by live channel membership and has no such gap.

---

## Learning mode — how to give guidance

When the Learning output style is active and you've handed me a `TODO(human)`,
the point is that I discover the answer, not that I transcribe yours. Guidance
means **narrowing the search space**, never **closing it**.

### The rule

**Do not write the code you asked me to write.** This holds even when I ask for
help — *especially* then. "I need more guidance" means I'm stuck on a smaller
question than the whole task; find that question and answer it. It does not mean
"give up and hand over the solution."

If you genuinely think the exercise is a bad fit — too tedious, too obscure, not
actually a design decision — say so and offer to write it. Ask first. Don't
silently abandon it by degrees.

### What guidance may contain

- **Names of the right tools.** "`io.ReadFull` is the primitive you want here."
  Naming a stdlib function is a pointer, not an answer — I still have to read its
  docs and figure out how to use it.
- **The decision, stated as a decision.** "A 300-byte PNG is a legitimate short
  read. Which of `ReadFull`'s errors are *actually* failures?" Frame the fork;
  don't resolve it.
- **The failure mode being avoided.** "The old code sniffed on zero-padded bytes."
  Naming the bug lets me recognize it; it doesn't tell me the fix.
- **Where to look.** A doc comment, an existing function in this repo that solves
  the same shape of problem, the relevant `ARCHITECTURE.md` section.

### What guidance must not contain

- **The function body, in pieces.** Four code blocks that concatenate into the
  answer is the answer. Splitting it across numbered steps doesn't change that.
- **A lookup table of the thing I was supposed to look up.** If the lesson is
  "`io.ReadFull` reports partial reads via a sentinel error," then enumerating
  those errors in a table *is* the lesson, spent.
- **Filled-in snippets "to illustrate."** Illustrative code that happens to
  compile into the solution is not illustrative.

Pseudocode and structural hints are fine when they describe *shape* without
supplying *content* — "read, decide, detect, rejoin" is a skeleton; four working
lines are a solution.

### Escalation ladder

If I'm still stuck, go **one rung at a time**, and stop as soon as I'm unblocked:

1. Restate the goal and the constraint that makes it non-trivial.
2. Name the tool or the concept (`io.MultiReader`; "readers are single-use").
3. Point at a doc or a worked analogue in the repo.
4. Ask me a question that forces the decision ("what should an empty file do?").
5. Give a partial: one line of the four, and say which one it is.
6. Only then, ask whether I'd rather you just write it.

### After I contribute

Review it like a PR. If there's a bug, say so plainly and point at the line —
don't soften it, and don't silently fix it. Then give **one** insight connecting
the code to a broader pattern or a downstream effect. No praise, no recap.

---

## Build and run

```sh
# Build all binaries
go build ./...

# Run the bot locally (loads env files from ./env/)
go run ./cmd/bot -use-env-file 1
# Run against a specific test guild only (avoids global command propagation delay)
go run ./cmd/bot -use-env-file 1 -guild <GUILD_ID>

# Docker Compose (pulls latest image from Docker Hub)
docker compose up

go test ./...
go vet ./... && gofmt -l .
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
DATABASE_URL              # postgres connection string (managed, off-box; pooled)
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
- Add the new env/secrets above; provision managed Postgres (Neon/Supabase) and
  set `DATABASE_URL`. If self-hosting Postgres instead, schedule `pg_dump` backups.

---

## Settled decisions

- Default `upload_policy` is `manage_guild` — uploads are admin-gated by default;
  an admin can loosen a guild to `everyone`.
- Existing gifs are seeded under the **origin guild's ID** (no global/built-in
  flag; the model is strictly per-guild).
- `SYSTEM_ADMIN_IDS` must be provided before `/admin` is usable.

---

## Suggested dependencies (not yet locked — confirm before adding)

`jackc/pgx/v5` · `pressly/goose` · `aws-sdk-go-v2` S3 (or
`minio-go`) · `golang.org/x/oauth2` · `alexedwards/scs` · stdlib `net/http` 1.22
routing or `go-chi/chi` · `html/template` + htmx.

---

