# sfw-sasuke — Architecture & Migration Plan

This document describes the target architecture for evolving `sfw-sasuke` from a
single-process Discord bot with file-based, globally-registered commands into a
two-process system (bot + self-service web app) with per-guild gifs, Discord
OAuth, object storage, and a system-admin console.

> "Gif" is the product term, but assets are arbitrary images/animations
> (the current set includes `.png`, `.jpeg`, `.jpg`, `.gif`). The schema keys
> off `content_type`, not the extension.

---

## 1. Current state (as of the existing code)

- **Language/deps:** Go 1.19, `bwmarrin/discordgo` v0.26.1, `joho/godotenv`.
- **Single process:** `cmd/` is one `package main` (`main.go` + `handlers.go`).
- **Command model:** each entry in `env/files-metadata.json` becomes one
  `ApplicationCommand`, registered **globally** (or to one test guild via the
  `-guild` flag) at startup and **deleted at shutdown**. A command maps to one
  *or more* files (e.g. `sfw` → 4 crops).
- **Delivery:** the handler reads bytes from `ASSETS_DIR` (`static/`) and uploads
  them as `discordgo.File` attachments.
- **Config:** `pkg/config` loads the JSON into `map[string][]filesConfig`.
- **Gifs are not tied to any guild** — there is one shared, global command set.
- **Deploy:** Dockerfile, docker-compose, systemd unit, GitHub Actions workflow.
  Secrets via `env/secrets.env` (`BOT_TOKEN`); `env/config.env` holds
  `ASSETS_DIR`, `CMD_METADATA_PATH`.
- **Known quirks:** the `v := v` loop-variable workaround (pre-Go 1.22); the
  `checkErr` → `log.Fatal` pattern (fine at boot, dangerous in handlers); a stale
  `replace` directive in `go.mod` pointing at a nonexistent `./config`.

---

## 2. Target architecture

Two long-running processes sharing one Go module and an `internal/` data layer,
fronted by Caddy for TLS. Two processes (not one binary) so a web deploy never
drops the gateway connection.

```
cmd/bot/        gateway + single /gif command + autocomplete + guild upsert
cmd/web/        Discord OAuth, CRUD API, frontend, /admin console
cmd/seed/       one-off: migrate files-metadata.json + static/ into bucket + DB
internal/store/     DB repository + embedded migrations
internal/storage/   S3-compatible client (get / put / presign / delete)
internal/discord/   OAuth helpers, permission-bit checks, guild intersection
internal/authz/     Can(), role resolution, system-admin bootstrap
internal/model/     domain types (Guild, User, Gif, GifFile, GuildSettings, ...)
internal/web/       handlers, sessions, template rendering
web/templates/      html/template files
web/static/         css / js (htmx)
```

### 2.1 The central change: one dynamic command, not a dynamic command list

The current "one global command per gif" model breaks on all three goals
(per-guild scoping, the ~100-command cap, slow re-registration on every upload).
Fix it by making the **data** dynamic while the **command** stays static:

- Register **one** `/gif` command, once, globally. It never changes when gifs do.
- `/gif name:<autocomplete>` — the autocomplete handler queries the DB for gif
  names in `interaction.GuildID` matching the typed prefix.
- On submit, look up the row(s) by `(guild_id, name)`, fetch bytes from object
  storage, and respond with attachments (same response shape as today).
- Optional `/gif-list` to enumerate a guild's gifs.

Uploads via the web app become invisible to Discord's command system — they
just add rows. Scoping is inherently per-guild because it happens at lookup time
from `interaction.GuildID`.

### 2.2 Storage & delivery (members-only)

Gifs must be visible only to a server's members, so:

- Bucket is **private**; no public URLs. Store bytes in object storage, metadata
  + object key in the DB.
- **Bot path:** download object → upload as Discord attachment. Channel
  membership gates visibility, exactly as today.
- **Web preview path:** verify the user is a current member of the guild (or a
  system admin), then serve a preview via a **short-lived presigned GET URL**
  (simple; leaks-if-shared like any Discord CDN link) or by **proxying bytes**
  through the authenticated session (stricter, zero-leak). Default to presigned
  with a short expiry.
- **Provider:** Cloudflare R2 (S3 API, free tier, no egress fees) is the
  recommended pick since the bot re-fetches bytes per use; Oracle object storage
  keeps everything on one provider. Same S3 client either way.

### 2.3 Data layer: SQLite, with a clean write split

At this scale (tens of guilds, few hundred gifs, human-paced writes) SQLite in
WAL mode with `busy_timeout` set is the right default. Contention is avoided by
splitting write ownership:

- **Web service owns** writes to `users`, `gifs`, `gif_files`, `guild_settings`,
  `audit_log`.
- **Bot owns** writes to `guilds` (upsert on `GuildCreate`, remove on
  `GuildDelete`); it only reads everything else.

Postgres (self-hosted or a free Neon/Supabase tier) is the upgrade path if write
volume grows. Do **not** store multi-MB gif bytes as DB blobs — that lives in
object storage.

### 2.4 Authentication & sessions

- Discord OAuth with scopes `identify` + `guilds`. The bot token and the OAuth
  client come from the **same Discord application** (different credentials).
- On callback: validate the `state` param (CSRF), exchange the code, fetch the
  user and their guild list, intersect with the guilds the bot is in, and mint a
  **server-side session** (don't reuse the Discord token per request).
- The `guilds` scope returns each guild's `permissions` bitfield, so
  `MANAGE_GUILD` (`0x20`) admin status is known without extra API calls.

---

## 3. Authorization model

Two distinct authority axes:

| Capability                              | Member | `MANAGE_GUILD` | Uploader | System admin |
|-----------------------------------------|:------:|:--------------:|:--------:|:------------:|
| View / use a guild's gifs               |   ✓    |       ✓        |    ✓     |      ✓       |
| Upload (policy = `everyone`)            |   ✓    |       ✓        |    ✓     |      ✓       |
| Upload (policy = `manage_guild`)        |        |       ✓        |          |      ✓       |
| Delete own gif                          |        |       ✓        |    ✓     |      ✓       |
| Delete anyone's gif in the guild        |        |       ✓        |          |      ✓       |
| Edit guild settings / quota             |        |       ✓        |          |      ✓       |
| CRUD across **all** guilds              |        |                |          |      ✓       |
| View storage usage across all guilds    |        |                |          |      ✓       |

Everything routes through a single function:

```go
// internal/authz
func Can(s *Session, action Action, g GuildID) bool {
    if s.IsSystemAdmin { return true } // cross-guild escape hatch
    // ... membership + MANAGE_GUILD + upload_policy + ownership logic
}
```

**System admin** is an operator of the whole service — a different thing from
per-guild `MANAGE_GUILD`. It is **bootstrapped from `SYSTEM_ADMIN_IDS`** (env,
comma-separated Discord user IDs), never a DB flag editable in-app (that would
create a "who admins the admins" escalation path). Admin functionality lives in
a dedicated `/admin` route group with its own guard and its own UI (a guild
picker over *all* guilds, since the admin operates on guilds they may not belong
to). Hiding the nav link is **not** access control — re-check `IsSystemAdmin`
server-side on every `/admin` route.

---

## 4. Data model

```sql
guilds (
  id           TEXT PRIMARY KEY,   -- discord guild id
  name         TEXT,
  joined_at    TIMESTAMP
);

users (
  id           TEXT PRIMARY KEY,   -- discord user id
  username     TEXT,
  avatar       TEXT,
  first_seen_at TIMESTAMP
);

gifs (
  id                INTEGER PRIMARY KEY,
  guild_id          TEXT NOT NULL REFERENCES guilds(id),
  uploader_user_id  TEXT NOT NULL REFERENCES users(id),
  name              TEXT NOT NULL,
  created_at        TIMESTAMP,
  UNIQUE (guild_id, name)          -- names are how users invoke them
);

-- preserves the existing "one name -> many files" capability
gif_files (
  id           INTEGER PRIMARY KEY,
  gif_id       INTEGER NOT NULL REFERENCES gifs(id) ON DELETE CASCADE,
  object_key   TEXT NOT NULL,
  content_type TEXT NOT NULL,
  size_bytes   INTEGER NOT NULL,
  ordinal      INTEGER NOT NULL     -- display/attachment order
);

guild_settings (
  guild_id            TEXT PRIMARY KEY REFERENCES guilds(id),
  upload_policy       TEXT NOT NULL DEFAULT 'manage_guild', -- 'manage_guild' | 'everyone'
  storage_quota_bytes INTEGER                            -- NULL = unlimited
);

audit_log (
  id            INTEGER PRIMARY KEY,
  actor_user_id TEXT NOT NULL,
  action        TEXT NOT NULL,       -- e.g. 'gif.delete', 'settings.update'
  guild_id      TEXT,
  gif_id        INTEGER,
  detail        TEXT,                -- JSON blob, optional
  created_at    TIMESTAMP
);
```

### Storage usage by guild

```sql
SELECT g.guild_id,
       COUNT(DISTINCT g.id)   AS gif_count,
       COALESCE(SUM(f.size_bytes), 0) AS bytes
FROM gifs g
LEFT JOIN gif_files f ON f.gif_id = g.id
GROUP BY g.guild_id;
```

This is the **recorded** total; it only matches actual bucket usage if deletes
reliably remove objects (see invariant in §6). An optional reconciliation job
that lists the bucket and diffs against `gif_files.object_key` catches orphans.

`storage_quota_bytes` enables the natural extension: the upload handler sums
current usage and rejects uploads that would exceed the quota. Quotas are out of
the core scope but the two pieces (usage query + upload check) are already here.

---

## 5. Settled decisions

1. **Default upload policy is `manage_guild`** — uploads are admin-gated by
   default; only users with `MANAGE_GUILD` can upload unless an admin explicitly
   loosens a guild's policy to `everyone`.
2. **Existing gifs move under the origin guild's ID** — the seeder inserts every
   migrated asset under the original server's guild ID. No global/built-in flag;
   the data model stays strictly per-guild.
3. **System admin IDs** — must be supplied via `SYSTEM_ADMIN_IDS` before the
   `/admin` console is usable.

---

## 6. Critical invariants (must hold in every phase)

1. **Never trust a client-supplied `guild_id`.** Re-verify membership +
   permissions server-side on every mutating request.
2. **Re-check `IsSystemAdmin` server-side on every `/admin` route.**
3. **System admin comes only from `SYSTEM_ADMIN_IDS`** — not an in-app DB flag.
4. **The bot scopes all gif lookups by `interaction.GuildID`.**
5. **Deleting a gif deletes its bucket objects** (no orphans; keeps usage
   accurate). Prefer DB delete + object delete in one operation with cleanup on
   partial failure.
6. **Bot command/autocomplete handlers never `log.Fatal`/panic on a bad lookup** —
   respond with an ephemeral error message instead.
7. **Bucket stays private.** Bot streams attachments; web previews use short-lived
   presigned URLs or authenticated proxying.
8. **Validate the OAuth `state` param** on callback.
9. **Validate uploads:** magic-byte content-type sniff + size limit; rate-limit
   uploads.
10. **Audit destructive admin actions** to `audit_log`.

---

## 7. Migration checklist

Work top-to-bottom; each phase leaves the system in a runnable state.

### Phase 0 — Scaffolding
- [ ] Bump Go to 1.22+ (loop-var fix; `net/http` routing; `log/slog`).
- [ ] Remove the stale `replace` directive in `go.mod`.
- [ ] Restructure into `cmd/bot`, `cmd/web`, `cmd/seed`, and `internal/*`.
- [ ] Keep the bot running with the existing JSON config (no behavior change yet).
- [ ] Add dependencies (suggested, confirm choices):
  - DB: `modernc.org/sqlite` (pure Go, no cgo).
  - Migrations: `pressly/goose` or embedded SQL + a tiny runner.
  - Object storage: `aws-sdk-go-v2` S3 client (or lighter `minio-go`).
  - OAuth: `golang.org/x/oauth2` with Discord endpoints.
  - Sessions: `alexedwards/scs` (server-side).
  - Router: stdlib `net/http` (1.22) or `go-chi/chi`.
  - Frontend: `html/template` + htmx (CDN).

### Phase 1 — Data layer
- [ ] Implement `internal/model` domain types.
- [ ] Implement `internal/store` with embedded migrations and a repository API.
- [ ] Implement `internal/storage` (get / put / presign / delete) against R2/Oracle.
- [ ] Wire SQLite with `_journal_mode=WAL` and a `busy_timeout`.

### Phase 2 — Bot refactor
- [ ] Replace per-gif commands with a single global `/gif` + autocomplete.
- [ ] Autocomplete queries `store` by `(guild_id, prefix)`.
- [ ] On submit: look up by `(guild_id, name)`, fetch objects, upload attachments.
- [ ] Graceful ephemeral errors on miss/failure (no `log.Fatal` in handlers).
- [ ] Upsert `guilds` on `GuildCreate`; remove on `GuildDelete`.
- [ ] (Optional) `/gif-list`.

### Phase 3 — Seed/migrate existing assets
- [ ] `cmd/seed` reads `env/files-metadata.json`, uploads each `static/` file to
      the bucket, and inserts `gifs` + `gif_files` rows under the **origin guild's
      ID** (supply it as a flag/env to the seeder).
- [ ] Verify `/gif` autocomplete + delivery works for the migrated set.
- [ ] Retire `ASSETS_DIR` / `CMD_METADATA_PATH` once the seeder has run.

### Phase 4 — Web service (self-service)
- [ ] Discord OAuth (`identify`, `guilds`), `state` validation, server-side session.
- [ ] Intersect the user's guilds with the bot's guilds for the dashboard.
- [ ] Upload (validate magic bytes + size, rate-limit), enforcing `upload_policy`.
- [ ] List / view / delete own gifs (+ delete-any for `MANAGE_GUILD`).
- [ ] Previews via presigned URL (or proxy).
- [ ] Per-guild settings page (upload policy) for `MANAGE_GUILD` users.

### Phase 5 — Authz + admin console + usage
- [ ] `internal/authz.Can` with the system-admin short-circuit.
- [ ] `SYSTEM_ADMIN_IDS` bootstrap.
- [ ] `/admin` route group with its own server-side guard and guild picker.
- [ ] Cross-guild CRUD over all gifs.
- [ ] Storage-usage dashboard (per-guild count + bytes; largest gifs).
- [ ] Object deletion wired into every delete path.
- [ ] `audit_log` writes on destructive actions.
- [ ] (Optional) `storage_quota_bytes` + upload-time quota enforcement.
- [ ] (Optional) bucket↔DB reconciliation job.

### Phase 6 — Deploy
- [ ] Caddy reverse proxy (auto-TLS) in front of `cmd/web`.
- [ ] Second systemd unit (or second docker-compose service) for `cmd/web`.
- [ ] Add new env/secrets (see CLAUDE.md §Environment).
- [ ] Update the GitHub Actions workflow to build both binaries.
- [ ] Confirm the SQLite file lives on a persistent volume/path.
