# sports-sdv — Architecture

A Go backend that wraps external sports-data providers (SportMonks **Cricket v2**
and **API-Football / api-sports.io v3**), normalizes their responses into its own DTOs, caches them,
and re-exposes a clean REST API — plus an embedded, dependency-free **chatbot
web UI** for querying live scores, fixtures, tables and rankings.

- **Language / runtime:** Go 1.26 (standard library only — no external deps)
- **HTTP routing:** stdlib `net/http.ServeMux` (Go 1.22+ method+pattern routing)
- **Frontend:** vanilla HTML/CSS/JS, embedded in the binary via `go:embed`
- **Data mode:** live-only (no mock/offline mode)

---

## 1. High-level architecture

```
                         ┌───────────────────────────────────────────┐
  Browser (chatbot UI)   │                 Go server                  │
  ───────────────────►   │                                           │
   GET /                 │  httpapi (router + handlers + middleware) │
   GET /api/v1/...       │        │                                   │
                         │        ▼                                   │
                         │   sports.Service / sports.FootballService  │
                         │        │        (map upstream → DTOs)      │
                         │        ▼                                   │
                         │   cache (TTL) ──hit──► return DTOs         │
                         │        │ miss                              │
                         │        ▼                                   │
                         │   provider client ── HTTP ──► SportMonks   │
                         │   (sportmonks / football)      (upstream)  │
                         └───────────────────────────────────────────┘
```

Everything the browser sees is a **DTO** shaped by this service, never the raw
upstream JSON. The chatbot UI is served from the same origin as the API, so
there is no CORS and no separate frontend deployment.

### Request flow (example: `GET /api/v1/livescores`)
1. `httpapi` router matches the route → `Handlers.livescores`.
2. Handler calls `sports.Service.Livescores(ctx)`.
3. Service builds a cache key; on hit it returns cached DTOs immediately.
4. On miss it calls `sportmonks.Client.Livescores`, which does the HTTP request
   (with retry) and decodes the SportMonks envelope.
5. Service maps upstream `Fixture`s → `MatchDTO`s (resolving team names, innings,
   and — for in-play matches — live scorecard detail).
6. Result is cached with a TTL and returned as JSON.

---

## 2. Package layout

```
cmd/server/main.go              Entry point: load config, wire clients/services/router, serve, graceful shutdown

internal/config/                Env-based configuration
  config.go

internal/cache/                 TTL cache behind a Cache interface (in-memory now; swappable for Redis)
  cache.go

internal/sportmonks/            Provider client: SportMonks Cricket API v2.0
  client.go                       HTTP, auth (api_token query param), envelope, retry, error sanitizing
  types.go                        Upstream response types (Fixture, Run, Batting, Bowling, Standing, RankingType, ...)

internal/football/              Provider client: API-Football (api-sports.io) v3
  client.go                       HTTP, auth (x-apisports-key header), get/response envelope, retry
  types.go                        Upstream v3 types (Fixture, League, StandingsResponse, ...)

internal/sports/                Business layer: orchestration + mapping to public DTOs
  service.go                      Cricket service; cache-aside; live-scorecard computation
  models.go                       Cricket DTOs (MatchDTO, ScorecardDTO, StandingDTO, RankingDTO, ...)
  service_football.go             Football service + cache-aside helpers
  models_football.go              Football DTOs

internal/httpapi/               HTTP layer
  router.go                       Route table + middleware chain + UI catch-all
  handlers.go                     Cricket handlers + shared helpers (writeJSON, mapUpstreamError)
  handlers_football.go            Football handlers
  middleware.go                   Request logging + panic recovery

internal/webui/                 Embedded chatbot single-page UI
  webui.go                        go:embed of web/ served via http.FileServer
  web/index.html | styles.css | app.js
```

### Layer responsibilities
- **Provider clients** (`sportmonks`, `football`) — the *only* code that knows
  the upstream wire format. They do auth, requests, retries, envelope decoding.
  They do **not** cache.
- **Service** (`sports`) — cache-aside orchestration and the mapping from
  upstream types to the app's own DTOs. All business logic (e.g. live run-rate
  math, current-batsmen detection) lives here.
- **httpapi** — transport: routing, query parsing, status codes, JSON encoding,
  error mapping. No business logic.
- **webui** — static assets embedded in the binary.

The strict separation means a **provider swap is a one-package rewrite**: replace
`internal/sportmonks` (or add a new client) that produces the same DTOs, and the
service/handlers/UI are untouched. This is exactly how Football (v3) was added
alongside Cricket (v2).

---

## 3. HTTP API

Cricket (unprefixed, backward-compatible):

| Method | Path | Query params |
|---|---|---|
| GET | `/healthz` | — |
| GET | `/api/v1/livescores` | — |
| GET | `/api/v1/matches` | `date`, `from`, `to`, `season`, `league`, `team` |
| GET | `/api/v1/matches/{id}/scorecard` | — |
| GET | `/api/v1/standings` | `season` (required) |
| GET | `/api/v1/rankings` | `type` (TEST/ODI/T20I), `gender` |
| GET | `/api/v1/leagues` | — |

Football (namespaced `/football/`):

| Method | Path | Query params |
|---|---|---|
| GET | `/api/v1/football/livescores` | — |
| GET | `/api/v1/football/matches` | `date`, `from`, `to`, `competition` (code, e.g. `WC`) |
| GET | `/api/v1/football/matches/{id}` | — |
| GET | `/api/v1/football/standings` | `competition` (code, e.g. `PL`; default `PL`) |
| GET | `/api/v1/football/leagues` | — |

UI:

| Method | Path | |
|---|---|---|
| GET | `/` (and static assets) | Chatbot single-page app (catch-all; more specific API routes win) |

Responses are JSON: list endpoints return `{"count": N, "data": [...]}`; single
resources return `{"data": {...}}`; errors return `{"error": "..."}`.

---

## 4. Providers

### SportMonks Cricket API v2.0 (`internal/sportmonks`)
- **Base URL:** `https://cricket.sportmonks.com/api/v2.0`
- **Auth:** `api_token` query parameter on every request
- **Envelope:** `{"data": ..., "meta": ...}`
- **Includes:** e.g. `?include=localteam,visitorteam,runs,batting.batsman,bowling.bowler`

**Reverse-engineered quirks** (discovered by probing live data; handled in code):
- **Inconsistent include wrapping** — usually `{"data": [...]}`, but some endpoints
  (e.g. livescores `runs`) return a **bare array**. `Data[T].UnmarshalJSON`
  tolerates both shapes.
- **`filter[starts_between]` end is exclusive-at-midnight** — a same-day range
  `(Jul1,Jul1)` returns **0** results. The handler extends the end by one day
  (`nextDay`) so the whole end day is covered.
- **`live: true` persists on finished/abandoned fixtures** (it's a "has live
  coverage" flag). `isInProgress(status)` gates the real live state on the status
  string instead.
- **`active` (batting) only marks the striker** and is often empty; **`wicket_id`
  is not an out-flag**. The current not-out pair is detected via
  **`fow_score`/`fow_balls` both 0**.
- **Rankings stats are nested** under a `ranking` object per team; the filter is
  `filter[type]` (not `tournament_type`, despite older docs).

### API-Football (api-sports.io) v3 (`internal/football`)
- **Base URL:** `https://v3.football.api-sports.io`
- **Auth:** `x-apisports-key` header
- **Envelope:** `{get, parameters, errors, results, response}` — errors can arrive
  with HTTP 200, so the client inspects `errors` and surfaces them.
- **Endpoints:** `/fixtures?live=all`, `/fixtures?date=`, `/fixtures?league=&season=(&from=&to=)`,
  `/fixtures?id=`, `/standings?league=&season=`, `/leagues`.
- **Competition scoping is by league id + season.** The UI's competition codes
  (WC, PL, …) are mapped to API-Football league ids in `service_football.go`
  (`fbLeagueID`); season defaults to the current year.
- **Free-plan limitation:** the free plan only serves league/season data for
  seasons **2022–2024** — current-season standings and the **2026 World Cup are
  blocked** (`plan: Free plans do not have access to this season`). `live=all`
  and `date=` (today's fixtures) still work. For 2025+ league/season data a paid
  plan is required. *(football-data.org's free tier did include WC 2026 — see §11.)*
- **Switchable provider:** `FOOTBALL_PROVIDER` selects the implementation at
  startup — `apifootball` (default) or `footballdata` (the football-data.org
  client, kept in `internal/footballdata` as a fallback). Both satisfy the
  `sports.FootballAPI` interface, so the HTTP layer is provider-agnostic. Setting
  `footballdata` also defaults the base URL to `https://api.football-data.org/v4`.

---

## 5. Live scorecard computation (`enrichLive`)

For an in-progress cricket match the service computes, from `runs` + `batting` +
`bowling` + `note`:
- **Batting team** and **over count** (current innings)
- **Batsmen** — the not-out pair (striker first, marked `onStrike`)
- **Bowler** — active → mid-over (fractional overs) → most-recent fallback
- **Current run rate** = score ÷ overs (with cricket over→decimal conversion; the
  `.1–.5` are balls, not tenths)
- **Required runs** = target − score (target parsed from `note`, fallback to
  1st-innings + 1)
- **Required run rate** = required ÷ overs remaining (over-limit inferred from
  match type: T20→20, ODI→50)

---

## 6. Chatbot UI (`internal/webui/web`)

A single-page app that turns free-text into API calls and renders **score cards**.
- **Intent routing** (`app.js`): detects sport (team/keyword lists), action
  (live / matches / standings / rankings), cricket **format** (test/oneday/t20,
  incl. synonyms like "oneday", "50 over"), **relative dates** ("yesterday",
  "today", "last week", "results"), and **named competitions** (World Cup, IPL,
  The Hundred, Big Bash, …) — matched against the match's league name so a
  tournament query returns only that tournament (or an honest "not running now").
- **Resilience:** cricket and football are fetched independently, so a football
  failure (e.g. no token) doesn't kill a cricket/ambiguous query.
- **Response time:** each answer shows its round-trip time (`⚡ responded in N ms`).
- **Cards:** match cards (teams, score, date, LIVE badge, live scorecard detail),
  and table cards (standings/rankings). Subtle 3D tilt on hover.
- Served same-origin, embedded in the binary — works from any CWD, no build step.

---

## 7. Configuration (env vars)

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8090` | HTTP listen port (8080 avoided — used by another local service) |
| `SPORTMONKS_API_TOKEN` / `API_CRICKET_KEY` | — | **Required.** Cricket token |
| `SPORTMONKS_BASE_URL` | `https://cricket.sportmonks.com/api/v2.0` | |
| `SPORTMONKS_INSECURE_SKIP_VERIFY` | `false` | Dev-only TLS bypass |
| `FOOTBALL_PROVIDER` | `apifootball` | `apifootball` or `footballdata` |
| `FOOTBALL_API_TOKEN` / `SPORTMONKS_FOOTBALL_TOKEN` | — | Football key; endpoints error without it |
| `FOOTBALL_BASE_URL` | per provider | api-sports.io v3, or api.football-data.org/v4 |
| `FOOTBALL_INSECURE_SKIP_VERIFY` | `false` | Dev-only TLS bypass |
| `CACHE_TTL` | `5m` | Static data (standings, leagues, schedule) |
| `CACHE_TTL_LIVE` | `20s` | Volatile data (livescores, today, scorecards) |
| `UPSTREAM_TIMEOUT` | `30s` | Per-request timeout |

`.env` is loaded by the Makefile (`make run`) and is git-ignored (holds secrets).

---

## 8. Cross-cutting concerns

- **Caching** — cache-aside in the service; `cache.Cache` interface with an
  in-memory TTL implementation (background janitor evicts expired entries).
  Static vs live TTLs protect the upstream quota.
- **Resilience** — `doWithRetry` retries a transient transport error once (short
  backoff) but **not** timeouts (retrying a timeout just doubles the wait).
- **Error hygiene** — transport errors are mapped to a clean `APIError`; the
  request URL (which carries the token) is **never** surfaced to clients. HTML
  responses (proxy/gateway interception) are detected and reported as such.
- **Observability** — structured JSON logs: one `request` line per incoming call
  (method, path, query, status, dur, **plus the response body sent to the client**
  for `/api/` paths), plus one `upstream` line per provider call (`provider`, the
  exact request, status, **the response body received**, dur) with `api_token`
  redacted. Bodies are truncated (~2000 chars). Logs stream to stdout **and** are
  appended to `LOG_FILE` (default `logs/server.log`, git-ignored). The browser
  console also logs the parsed intent and each API request/response. Panics are
  recovered into 500s.

---

## 9. Known limitations / caveats

- **Cricket is polling-based** (no native push) — the UI re-queries; there is no
  server→browser live streaming yet.
- **Football uses API-Football (api-sports.io)** and needs an `x-apisports-key`.
  Its **free plan only serves seasons 2022–2024** for league/season queries, so
  current standings and the **2026 World Cup are blocked** on free (paid plan
  required); `live=all` and today's `date=` fixtures still work.
- **Whole-day historical queries can be slow** upstream (mitigated by the 30s
  timeout + a clean "timed out — try again" message).
- **Standings season ids** are hardcoded defaults in the UI (`SEASON`) — should
  become user-selectable.
- **No automated tests yet** — behavior has been verified manually against live data.

---

## 10. Roadmap

- **Real-time push** — evaluated **Roanuz** (native WebSocket) as the better fit
  for a commercial, real-time cricket product; plan to migrate cricket behind the
  existing DTOs and add server→browser push (SSE/WebSocket). See §12.
- **Automated tests** — unit tests for the mapping/quirks (over conversion,
  `fow_score`, `starts_between`, `isInProgress`) + an integration smoke test.
- **Persistence** — Postgres for history; a scheduled sync (ticker) to pre-warm
  the cache and reduce upstream calls.
- **Own-API concerns** — auth/rate-limiting on the exposed endpoints.

---

## 11. Development history (what we built, in order)

1. **Initial scaffold** — Go service wrapping API-Football (api-sports.io) with
   cache + re-exposed REST endpoints.
2. **Pivot to SportMonks Cricket (v2)** — the provided key was a cricket key and
   API-Sports has no cricket; rebuilt the client, types, DTOs, and endpoints
   (livescores, matches, scorecard, standings, rankings, leagues).
3. **Fixed against live data** — corrected the rankings filter (`type`) and the
   nested ranking stats; handled the tolerant include wrapping.
4. **Offline/mock mode** — added embedded sample fixtures so the app could be
   exercised while a proxy blocked outbound traffic… then **removed it** when the
   requirement became live-only.
5. **Chatbot web UI** — embedded single-page app (intent routing → score cards),
   served by the Go server.
6. **Multi-sport** — added SportMonks **Football v3** alongside cricket under
   `/api/v1/football/...`, reusing the DTO/service/handler pattern.
7. **Live-only cleanup** — removed all mock code, sample data, and mode flags.
8. **Live scorecard detail** — current batsmen (via `fow_score`), bowler, over
   count, CRR, RRR, required runs; match dates on cards; per-query response time.
9. **Query smarts** — format filtering (test/oneday/t20 + synonyms), relative
   dates (yesterday/today/last week), empty-table filtering.
10. **Hardening** — retry (timeout-aware), 30s timeout, single-day
    `starts_between` fix, **API-key-leak fix** in error messages, and gating the
    live badge/enrichment on a real in-play status.
11. **Football provider swap** — replaced SportMonks Football v3 with
    **football-data.org v4** (free tier). The cricket key never covered SportMonks
    football, and its host was unreachable on the target network; football-data.org
    needs only a free token. DTOs/endpoints/UI stayed put — a one-package rewrite
    plus a standings change (season id → competition code).

12. **Competition filtering** — exposed the cricket **league name** on matches
    and added tournament recognition (World Cup, IPL, …) so "world cup match"
    returns only World Cup fixtures (or says none are running) instead of a
    generic list; also handle possessive dates ("todays", "yesterdays").
13. **Football provider swap #2** — moved football from football-data.org to
    **API-Football (api-sports.io) v3** at the user's request. Live scores and
    today's fixtures work; note the free plan blocks seasons 2025+ (so WC 2026 /
    current standings need a paid plan — football-data.org's free tier had WC 2026).
14. **Switchable football provider** — kept football-data.org as a fallback
    (`internal/footballdata`) behind a `sports.FootballAPI` interface; select with
    `FOOTBALL_PROVIDER`. API-Football is the default; football-data.org is one env
    var away (its free tier still serves WC 2026).

### Git history
```
(pending)  Make football provider switchable (keep football-data.org as fallback)
83e1edc    Switch football provider to API-Football (api-sports.io v3)
4b3bf05    Add competition/tournament filtering + cricket league names
a054bad    Swap football provider to football-data.org (v4)
f4333fd    Fix live badge on ended matches; tolerate per-sport fetch errors
dd5f0a2    Add live scorecard detail, relative-date queries, and response timing
6a331ec    Remove mock/offline mode: live-only SportMonks API
(first commits)  scaffold + cricket pivot + UI + football
```
Work is on branch `feature/live-sports-api`.

---

## 12. Provider evaluation (summary)

For SDV as a **commercial, real-time** product (moderate budget, both India +
international, **live WebSocket push** required):

| Provider | Verdict |
|---|---|
| **SportMonks Cricket v2** (current) | Solid, licensed, rich ball-by-ball — but **polling-only** and cricket is a secondary product for them. Good enough while polling is acceptable. |
| **Roanuz** | **Recommended** for the real-time requirement — native WebSocket/GraphQL push, built for cricket score apps/chatbots, covers India + international. Budget note: Essential ≈ ₹17,669/mo (~$210), caps at 100 matches / 400k req. |
| **EntitySport** | Strong India/IPL depth (250+ competitions); commercial. |
| **CricketData.org / CricAPI** | Cheapest (free tier); good for MVP, not a real-time commercial backend. |
| **RapidAPI (marketplace)** | Fine for discovery/prototyping; **not** for production here — REST proxy (no push), and popular cricket options are unofficial scrapers (licensing/reliability risk). |

**Direction:** keep SportMonks running; migrate cricket to Roanuz + add
server→browser push when moving to true real-time.
