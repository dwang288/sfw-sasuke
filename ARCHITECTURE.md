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

Two long-running processes sharing one Go module, fronted by Caddy for TLS. Two
processes (not one binary) so a web deploy never drops the gateway connection.
The code is organized in a **hexagonal (ports & adapters)** style so the database
and object store can be swapped without touching domain logic (see §2.1).

```
cmd/bot/     composition root: build adapters, inject into core, run the bot
cmd/web/     composition root: build adapters, inject into core, run the web app
cmd/seed/    composition root: one-off migration of files-metadata.json + static/

internal/
  core/                      the hexagon — no third-party SDK imports anywhere here
    domain/      entities, value objects, domain errors (Gif, GifFile, Guild,
                 User, GuildSettings, AuditEntry, GuildUsage; ErrGifNotFound, ...)
    port/        interfaces the core depends on (driven ports): Store, Repos,
                 GifRepository, GuildRepository, UserRepository,
                 SettingsRepository, AuditRepository, BlobStore, Presigner,
                 DiscordDirectory
    app/         application services / use cases that orchestrate ports:
                 GifService, AdminService, Authz (Can), UploadService, ...
  adapter/                   the edges — the ONLY place provider SDKs are imported
    postgres/    implements the *Repository / Store ports (jackc/pgx v5)
    objstore/    implements BlobStore for any S3-compatible endpoint (aws-sdk-go-v2)
    discordapi/  implements DiscordDirectory (OAuth token exchange, guild list)
    memory/      in-memory implementations of the ports, for fast core tests
    discordbot/  DRIVING adapter: gateway events + /gif + autocomplete -> app
    httpweb/     DRIVING adapter: OAuth flow + HTTP handlers + templates -> app

web/templates/   html/template files
web/static/      css / js (htmx)
```

### 2.1 Hexagonal structure (ports & adapters)

One-directional dependency: **adapters depend on the core; the core never imports
an adapter.** The core declares the interfaces it needs (ports) in terms of
`domain` types only; adapters at the edge implement them; `main` (the composition
root) constructs the concrete adapters and injects them. Swapping Postgres →
SQLite, or S3 → local filesystem, means writing one new adapter and changing one
line in each `cmd/*/main.go` — nothing in `core/` changes.

Rules that keep this honest:

- **No provider SDK import outside `internal/adapter/`.** `jackc/pgx`,
  `aws-sdk-go-v2`, the `discordgo` REST client, etc. appear only in adapters.
- **Ports speak `domain` + stdlib only.** No `*sql.DB`, `s3.Client`, or driver
  error types in any port signature.
- **Domain types carry no adapter coupling.** No `db:"..."` / `json:"..."` tags on
  `domain` structs; adapters map to their own local row/DTO structs.
- **Adapters translate errors inward.** `pgx.ErrNoRows` → `domain.ErrGifNotFound`,
  S3 `NoSuchKey` → `domain.ErrObjectNotFound`, so the core never switches on a
  driver-specific error.
- **Transactions stay provider-agnostic** via a `Store.Tx` port (below), so
  multi-table writes are atomic without leaking the driver.

The persistence and storage ports. `GifRepository` holds the only non-trivial
queries (prefix search, usage aggregation, delete-returning-row); the other three
repositories are thin upsert/get surfaces, split by table so the `Repos` bundle
and `Store.Tx` stay clean — collapse them into `Store` directly if the ceremony
feels heavy:

```go
// internal/core/port

// Persistence port. A Postgres adapter implements this same interface.
type Store interface {
    // Tx runs fn inside one transaction, exposing transaction-scoped repos.
    Tx(ctx context.Context, fn func(Repos) error) error
    Repos // also usable outside a transaction for single reads/writes
}

type Repos interface {
    Gifs() GifRepository
    Guilds() GuildRepository
    Users() UserRepository
    Settings() SettingsRepository
    Audit() AuditRepository
}

type GifRepository interface {
    Create(ctx context.Context, g *domain.Gif) error // g.Files inserted in same tx
    GetByName(ctx context.Context, guild domain.GuildID, name string) (*domain.Gif, error)
    SearchNames(ctx context.Context, guild domain.GuildID, prefix string, limit int) ([]string, error)
    ListByGuild(ctx context.Context, guild domain.GuildID) ([]domain.Gif, error)
    ListAll(ctx context.Context) ([]domain.Gif, error)               // admin
    Delete(ctx context.Context, id domain.GifID) (*domain.Gif, error) // returns row for object cleanup
    UsageByGuild(ctx context.Context) ([]domain.GuildUsage, error)
}

type GuildRepository interface {
    Upsert(ctx context.Context, g *domain.Guild) error   // GuildCreate / seed
    Get(ctx context.Context, id domain.GuildID) (*domain.Guild, error)
    List(ctx context.Context) ([]domain.Guild, error)    // membership intersect + admin picker
    Delete(ctx context.Context, id domain.GuildID) error // GuildDelete
}

type UserRepository interface {
    Upsert(ctx context.Context, u *domain.User) error    // login / seed "system" user
    Get(ctx context.Context, id domain.UserID) (*domain.User, error)
}

type SettingsRepository interface {
    // Get returns the manage_guild default when no row exists — never ErrNotFound.
    Get(ctx context.Context, guild domain.GuildID) (domain.GuildSettings, error)
    Upsert(ctx context.Context, s domain.GuildSettings) error
}

type AuditRepository interface {
    Append(ctx context.Context, e domain.AuditEntry) error
    ListByGuild(ctx context.Context, guild domain.GuildID, limit int) ([]domain.AuditEntry, error)
    ListAll(ctx context.Context, limit int) ([]domain.AuditEntry, error) // system admin
}

// Object-store port: only what every backend can honor and what core/app
// actually calls. A localfs / GCS / MinIO adapter implements this same interface.
type BlobStore interface {
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error) // ErrObjectNotFound
    Delete(ctx context.Context, key string) error
}

// Presigner is an OPTIONAL capability, not part of BlobStore: some backends
// (S3/R2/OCI) can hand out a signed, expiring URL; others (localfs) cannot.
// It's consumed only by the web preview path and discovered by type assertion.
type Presigner interface {
    PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}
```

`PresignGet` is deliberately **not** on `BlobStore`. Requiring it would force a
non-signing adapter to return a runtime "unsupported" error for a method it can't
honor (interface pollution), and no `core/app` service even calls it — only the
`httpweb` preview path does. Following the stdlib idiom (`http.Flusher`,
`io.ReaderFrom`), it's an optional capability discovered by assertion. The web
adapter resolves a preview strategy **once at startup** so handlers never branch:

```go
// httpweb setup / composition root
var preview PreviewURLProvider
if p, ok := blobs.(port.Presigner); ok {
    preview = presignPreview{p}    // redirect to a short-lived signed URL
} else {
    preview = proxyPreview{gifs}   // stream via an authenticated /preview handler
}
```

The proxy fallback streams through an app-service method (not `BlobStore.Get`
directly) so the membership/authz check still runs. Swapping in a non-signing
adapter changes only this one branch.

Composition root sketch (`cmd/bot/main.go`):

```go
db    := postgres.Open(ctx, cfg.DatabaseURL)  // -> port.Store
blobs := objstore.NewS3(cfg.S3)               // -> port.BlobStore
gifs  := app.NewGifService(db, blobs)         // core depends on ports only
bot   := discordbot.New(session, gifs)        // driving adapter
bot.Run(ctx)
```

Swapping persistence means a different `port.Store` adapter (`postgres.Open(...)`
→ e.g. `sqlite.Open(...)`); swapping storage a different `BlobStore`
(`objstore.NewS3(...)` → `localfs.New(...)`). `app` and `domain` are untouched —
moving this design from SQLite to Postgres changed only the adapter package and
these constructor lines, which is the hexagonal layer earning its keep.

### 2.2 The central change: one dynamic command, not a dynamic command list

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

### 2.3 Storage & delivery (members-only)

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
  keeps everything on one provider. Same S3 client either way — both are just the
  `objstore` adapter behind the `BlobStore` port, so a later move (or a localfs
  adapter for tests/dev) changes only the constructor in `main`.

**Members-only is best-effort for web reads.** The preview membership check uses
the guild list from the user's OAuth session, which is a *snapshot* — a user
kicked from a guild keeps preview access until their session or guild list
refreshes. Keep the session TTL short (or re-fetch the guild list, or check the
bot's live gateway membership) if you need that window tight. The bot delivery
path has no such gap: it's gated by live channel membership.

> **S3-compatible adapter gotchas (objstore).** Current `aws-sdk-go-v2` defaults
> to integrity checksums sent via `aws-chunked` encoding, which OCI (and others)
> reject; set `RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired`.
> Use path-style (`UsePathStyle = true`) and set an explicit `Region` + `BaseEndpoint`.
> Confine all of this to the adapter — the `BlobStore` port stays provider-neutral.

### 2.4 Data layer: Postgres behind a Store port

Postgres sits behind the `Store` port as the `postgres` adapter (`jackc/pgx` v5,
via `pgxpool`). Because the port is provider-neutral, this adapter is the *only*
layer that differs from a SQLite build — `core/domain`, `core/port`, and
`core/app` are identical.

**Hosting — prefer managed, off-box.** Run Postgres on a managed free tier (Neon
or Supabase) rather than on the Oracle instance. This is what makes the durability
story consistent: object storage already keeps the bytes off the reclaimable box,
and a managed Postgres puts the *metadata* — without which those objects are
unmappable orphans — off-box too, with automated backups. Self-hosting Postgres on
the instance works but keeps a single point of failure on a reclaimable box, adds
ops burden, and competes for the free instance's ~1 GB RAM; if you self-host,
schedule `pg_dump` backups to the bucket.

Two consequences of Postgres vs the earlier SQLite plan:

- **No write-ownership split needed.** Postgres handles concurrent writers natively
  (MVCC + row locks), so both processes can read and write any table safely — the
  fussy two-process SQLite file-locking concern is simply gone. The only remaining
  division is incidental: the bot is the process that receives `GuildCreate`/
  `GuildDelete`, so it does guild-lifecycle writes; the web app does user/gif/
  settings writes. That's about which process receives which events, not a locking
  constraint.
- **Latency budget.** Autocomplete must answer within Discord's ~3 s window. On a
  scale-to-zero managed tier (e.g. Neon autosuspend) a cold start can eat into
  that — keep a connection warm (periodic ping) or pick a config that doesn't
  suspend aggressively. Use the provider's *pooled* connection string; with
  transaction-mode pooling, configure pgx accordingly (simple protocol / disabled
  statement-cache).

`Store.Tx` maps cleanly onto pgx: begin a `pgx.Tx` from the pool, hand
transaction-scoped repos to `fn`, commit or roll back. Multi-table writes (a gif +
its `gif_files`) run in one transaction. Cross-resource consistency (DB row +
bucket object) still **cannot** be atomic — object stores aren't transactional — so
the delete path stays: remove the row, then the object; on object-delete failure,
log for the reconciliation job (§4). Do **not** store gif bytes in Postgres.

Adapter-level tests run against a disposable Postgres (a throwaway container via
testcontainers/dockertest); the `memory` adapter still covers `core/app` unit
tests with no database at all.

### 2.5 Authentication & sessions

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
// internal/core/app (pure decision logic over domain types — no adapter imports)
func (a Authz) Can(s *domain.Session, action domain.Action, g domain.GuildID) bool {
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
  id         TEXT PRIMARY KEY,           -- discord guild id (snowflake, kept as text)
  name       TEXT,
  joined_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

users (
  id            TEXT PRIMARY KEY,        -- discord user id
  username      TEXT,
  avatar        TEXT,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

gifs (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  guild_id         TEXT NOT NULL REFERENCES guilds(id),
  uploader_user_id TEXT NOT NULL REFERENCES users(id),
  name             TEXT NOT NULL,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (guild_id, name)                -- names are how users invoke them
);

-- preserves the existing "one name -> many files" capability
gif_files (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  gif_id       BIGINT NOT NULL REFERENCES gifs(id) ON DELETE CASCADE,
  object_key   TEXT NOT NULL,
  content_type TEXT NOT NULL,
  size_bytes   BIGINT NOT NULL,
  ordinal      INT NOT NULL              -- display/attachment order
);

guild_settings (
  guild_id            TEXT PRIMARY KEY REFERENCES guilds(id),
  upload_policy       TEXT NOT NULL DEFAULT 'manage_guild'
                        CHECK (upload_policy IN ('manage_guild','everyone')),
  storage_quota_bytes BIGINT                                  -- NULL = unlimited
);

audit_log (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_user_id TEXT NOT NULL,
  action        TEXT NOT NULL,           -- e.g. 'gif.delete', 'settings.update'
  guild_id      TEXT,
  gif_id        BIGINT,
  detail        JSONB,                   -- structured context, optional
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- indexes
CREATE INDEX ON gif_files (gif_id);                    -- join + cascade
CREATE INDEX ON audit_log (guild_id, created_at DESC); -- admin views
-- Autocomplete does LIKE 'prefix%'; under a default collation a plain btree
-- won't serve prefix matching, so index name with text_pattern_ops (or use the
-- citext extension / a lower(name) functional index if you want case-insensitive):
CREATE INDEX ON gifs (guild_id, name text_pattern_ops);
```

Two behaviors the schema implies, easy to miss:

- **Legacy gifs need an uploader.** `gifs.uploader_user_id` is `NOT NULL`, but the
  seeded assets have no real uploader. The seeder must first insert a synthetic
  `users` row (a sentinel "system" user, or the origin admin's Discord ID) and
  attribute the migrated gifs to it, or the FK fails.
- **A guild may have no `guild_settings` row.** The column `DEFAULT` applies only
  when a row is inserted — it does not create one. A freshly-joined guild has a
  `guilds` row but no settings row, so `SettingsRepository.Get` must return the
  `manage_guild` default when the row is absent (or the bot inserts a settings row
  on `GuildCreate`). Never assume the row exists.

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
11. **Hexagonal layering holds:** no provider SDK imported outside
    `internal/adapter/`; no driver types or `db:`/`json:` tags in `internal/core`;
    `core` never imports an adapter; adapters are wired only in `cmd/*/main.go`.

---

## 7. Build order (dependency-ordered)

Features are sequenced so nothing is built before what it needs. The numbering is
a valid topological order — you can always work straight down — but items in the
same tier with no arrow between them can be done in parallel. "Depends on" lists
the *feature* prerequisites, not every transitive one.

Critical path in one line:
`scaffolding → domain+ports → data adapters → (bot slice ∥ auth) → authorization → web CRUD → admin/usage → deploy`

Two independent verticals fall out of the data layer: the **bot slice** (steps
6–8) and the **auth/web slice** (steps 9+). Once the data adapters exist they can
progress in parallel and only rejoin at deploy.

### Tier A — Foundation

**1. Scaffolding** — *depends on: nothing*
- [x] Bump Go to 1.22+ (loop-var fix; `net/http` routing; `log/slog`). *(done: bumped to 1.26.4)*
- [x] Remove the stale `replace` directive in `go.mod`.
- [x] Create the hexagonal skeleton: `cmd/{bot,web,seed}`, `internal/core/{domain,port,app}`,
      `internal/adapter/{postgres,objstore,discordapi,memory,discordbot,httpweb}`.
- [x] Keep the old bot runnable from JSON until step 7 replaces it (no behavior change yet).
- [ ] Add dependencies (suggested; confirm choices): `jackc/pgx/v5`,
      `pressly/goose`, `aws-sdk-go-v2` S3 (or `minio-go`), `golang.org/x/oauth2`,
      `alexedwards/scs`, stdlib `net/http` (1.22) or `go-chi/chi`, `html/template` + htmx.

**2. Domain + ports + in-memory fakes** — *depends on: 1*
- [ ] `internal/core/domain`: types + domain errors (no `db:`/`json:` tags, no SDK imports).
- [ ] `internal/core/port`: `Store`, `Repos`, the `*Repository` interfaces, `BlobStore`,
      the optional `Presigner`, `DiscordDirectory` — in terms of `domain` + stdlib only.
- [ ] `internal/adapter/memory`: in-memory `Store` + `BlobStore` so the core is testable
      before any real adapter exists.

**3. App services (incl. Authz decision logic)** — *depends on: 2*
- [ ] `core/app` use cases that orchestrate ports only: `GifService`, `UploadService`,
      `AdminService`.
- [ ] `Authz.Can` as **pure decision logic** over `domain` types (membership, `MANAGE_GUILD`,
      `upload_policy`, ownership, system-admin short-circuit). Real identity is wired in step 11.
- [ ] Unit-test all of the above against the `memory` adapters (no DB or bucket needed).

### Tier B — Data adapters (parallel; unblock everything downstream)

**4. DB adapter + migrations** — *depends on: 2 (wired via 3)*
- [ ] `internal/adapter/postgres` implementing `Store`/`Repos` on `jackc/pgx` v5 (`pgxpool`).
- [ ] `Store.Tx` via `pgx.Tx` for atomic multi-table writes (a gif + its `gif_files`).
- [ ] Embedded goose migrations (`//go:embed`, Provider API, dialect `postgres`).
      goose needs `database/sql`, so open a short-lived `*sql.DB` with the
      `pgx/v5/stdlib` driver (driver name `pgx`) for migrations; use `pgxpool` for
      the app. Run migrations on startup.
- [ ] Create the §4 indexes, incl. the `text_pattern_ops` index that autocomplete needs.
- [ ] Translate `pgx.ErrNoRows` → `domain.ErrGifNotFound`, etc.
- [ ] Configure a small pool; use the provider's pooled connection string.
- [ ] Note: from here on a running Postgres is required to develop and test against
      (a local container in dev; the managed prod instance is provisioned in step 19).
      Unlike the SQLite plan, the DB is now an external dependency, not a local file.

**5. Object storage adapter** — *depends on: 2* — **parallel with 4**
- [ ] `internal/adapter/objstore` implementing `BlobStore` (S3-compatible).
- [ ] Set `RequestChecksumCalculation = WhenRequired`, `UsePathStyle`, explicit `Region` +
      `BaseEndpoint`; translate `NoSuchKey` → `domain.ErrObjectNotFound`.
- [ ] Implement the optional `Presigner` capability (S3/R2/OCI can sign); confirm
      `PresignGet` works against the chosen provider. A localfs adapter omits it and
      the web layer proxies instead.

### Tier C — Bot slice (parallel with Tier D once 4 & 5 exist)

**6. Guild lifecycle** — *depends on: 4*
- [ ] Enable the `GUILDS` gateway intent (non-privileged) so `GuildCreate`/`GuildDelete`
      actually arrive.
- [ ] Bot upserts `guilds` on `GuildCreate`; removes on `GuildDelete` (so gif scoping
      references real guild rows).
- [ ] Decide settings-row policy: either insert a `guild_settings` row on `GuildCreate`
      (`ON CONFLICT DO NOTHING`), or have `SettingsRepository.Get` coalesce a missing
      row to the `manage_guild` default. Pick one; don't leave reads assuming a row.

**7. Dynamic `/gif` command + autocomplete** — *depends on: 3, 4, 5, 6*
- [ ] Register a single global `/gif`; retire per-gif commands and shutdown-time deletion.
- [ ] Autocomplete → `GifService` → `GifRepository.SearchNames(guild, prefix)`
      with `limit ≤ 25` (Discord's autocomplete choice cap); respond within ~3 s.
- [ ] On submit: look up by `(guild_id, name)`, fetch objects via `BlobStore`, upload attachments.
- [ ] Graceful ephemeral errors on miss/failure (never `log.Fatal`/panic in handlers).
- [ ] (Optional) `/gif-list`.

**8. Seed existing assets** — *depends on: 4, 5* (validated via 7)
- [ ] `cmd/seed` reads `env/files-metadata.json`, uploads each `static/` file via `BlobStore`,
      inserts the origin `guilds` row + `gifs`/`gif_files` under the **origin guild's ID**
      (supply it as a flag/env).
- [ ] First insert a synthetic uploader `users` row (sentinel "system" user, or the origin
      admin's Discord ID) and attribute the migrated gifs to it — `uploader_user_id` is NOT NULL.
- [ ] Verify `/gif` autocomplete + delivery for the migrated set.
- [ ] Retire `ASSETS_DIR` / `CMD_METADATA_PATH`.

### Tier D — Identity (parallel with Tier C)

**9. Authentication** — *depends on: 4*
- [ ] Discord OAuth (`identify`, `guilds`) via `discordapi` adapter behind `DiscordDirectory`.
- [ ] Validate the `state` param (CSRF); mint a server-side session (`scs`); don't reuse the
      Discord token per request.
- [ ] Upsert `users` on login.

**10. Guild membership resolution** — *depends on: 9, and guild rows from 6/8*
- [ ] Intersect the user's OAuth guild list with the bot's `guilds`; expose only shared guilds.
- [ ] Extract `MANAGE_GUILD` (`0x20`) from each guild's `permissions` bitfield.

**11. Authorization wiring** — *depends on: 3, 9, 10 (+ `guild_settings` from 4)*
- [ ] Wire `Authz.Can` (built in step 3) to the real session, membership, and permission bits.
- [ ] Bootstrap system admin from `SYSTEM_ADMIN_IDS`; set `IsSystemAdmin` on the session.
- [ ] Enforce `Can` server-side on every mutating route (never trust client `guild_id`).

### Tier E — Web CRUD (depends on authorization)

**12. Upload** — *depends on: 11, 4, 5*
- [ ] Validate magic-byte content-type + size limit; rate-limit.
- [ ] Enforce `upload_policy` (default `manage_guild`); write `gifs`/`gif_files` in one `Tx`;
      `Put` objects.

**13. Gif management + previews** — *depends on: 11, 4, 5*
- [ ] List / view own gifs (+ delete-any for `MANAGE_GUILD`).
- [ ] Previews via short-lived presigned URL (or authenticated proxy where the adapter can't sign).
- [ ] Delete: remove DB row, then `BlobStore.Delete` the objects; log partial failures for
      reconciliation. Introduces `audit_log` writes on destructive actions.

**14. Per-guild settings** — *depends on: 11, 4*
- [ ] Settings page for `MANAGE_GUILD` users to set `upload_policy` (loosen to `everyone`).
- [ ] Make it reachable on first admin login so a fresh guild isn't stuck with no gifs.

### Tier F — Admin & usage (depends on 11 + CRUD)

**15. System admin console** — *depends on: 11, 13*
- [ ] `/admin` route group with its own server-side `IsSystemAdmin` guard + guild picker over
      all guilds.
- [ ] Cross-guild CRUD over all gifs (reuses the step-13 CRUD with the membership check replaced
      by the role check); audit each destructive action.

**16. Storage usage** — *depends on: 4, 15*
- [ ] `GifRepository.UsageByGuild`; per-guild count + bytes dashboard in `/admin`.
- [ ] (Optional) surface a guild's own usage to its `MANAGE_GUILD` admins (step 14 page).

**17. (Optional) Quotas** — *depends on: 12, 14, 16*
- [ ] `storage_quota_bytes` on `guild_settings`; reject uploads that would exceed it.

**18. (Optional) Reconciliation job** — *depends on: 4, 5*
- [ ] List the bucket, diff against `gif_files.object_key`, report/clean orphans.

### Tier G — Deploy (depends on both verticals)

**19. Deploy** — *depends on: 7, 15 (i.e. bot + web complete)*
- [ ] Caddy reverse proxy (auto-TLS) in front of `cmd/web`.
- [ ] Second systemd unit (or docker-compose service) for `cmd/web`.
- [ ] Provision managed Postgres (Neon/Supabase free tier); set `DATABASE_URL`.
      (If self-hosting instead, add a Postgres service + volume + scheduled `pg_dump`
      backups to the bucket.)
- [ ] Add the remaining env/secrets (see CLAUDE.md §Environment).
- [ ] Update GitHub Actions to build both binaries.
