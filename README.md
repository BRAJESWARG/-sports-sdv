# sports-sdv

A multi-sport Go backend that wraps SportMonks, caches responses, and re-exposes
its own clean REST API:

- **Cricket** — [SportMonks Cricket API v2.0](https://docs.sportmonks.com/v2/cricket-api):
  live scores, schedule/results, scorecards, standings, ICC rankings.
- **Football** — [SportMonks Football API v3](https://docs.sportmonks.com/football):
  live scores, schedule/results, match detail, league tables, leagues.

Each sport has its own upstream client (cricket is v2 with `{"data":...}` include
wrappers; football is v3 with inline includes and header auth) but shares the
cache, config, and HTTP patterns. Cricket lives at `/api/v1/...`; football is
namespaced under `/api/v1/football/...`.

## Architecture

```
client ──HTTP──► httpapi ──► sports.Service ──► cache (hit? return)
                                        │
                                        └─miss─► sportmonks.Client ──HTTP──► SportMonks Cricket
```

- **`internal/sportmonks`** — typed client for the upstream v2.0 API (auth via
  `api_token`, envelope + nested `{"data": ...}` include decoding).
- **`internal/cache`** — TTL cache behind a `Cache` interface (in-memory now; swap for Redis later).
- **`internal/sports`** — business layer: cache-aside orchestration + mapping upstream types to your own DTOs.
- **`internal/httpapi`** — router, handlers, logging/recover middleware.
- **`internal/config`** — env-based config.
- **`cmd/server`** — entrypoint with graceful shutdown.

The upstream response shapes (`sportmonks` types) are kept separate from your
public DTOs (`sports` models), so you can change providers without breaking your
API consumers.

## Setup

1. Get a token from https://www.sportmonks.com/ (Cricket API).
2. Set your token in `.env`:

   ```bash
   cp .env.example .env
   # edit .env -> SPORTMONKS_API_TOKEN=...  (API_CRICKET_KEY also accepted)
   ```

3. Run:

   ```bash
   make run        # or: go run ./cmd/server   (serves on :8090)
   ```

## Endpoints

### Cricket (`/api/v1/...`)

| Method | Path                                   | Query params                              |
|--------|----------------------------------------|-------------------------------------------|
| GET    | `/healthz`                             | —                                         |
| GET    | `/api/v1/livescores`                   | — (matches in play / today)               |
| GET    | `/api/v1/matches`                      | `date`, `from`+`to`, `league`, `season`, `team` |
| GET    | `/api/v1/matches/{id}/scorecard`       | — (full batting/bowling scorecard)        |
| GET    | `/api/v1/standings`                    | `season` (numeric season id, required)    |
| GET    | `/api/v1/rankings`                     | `type` (TEST/ODI/T20I), `gender` (men/women) |
| GET    | `/api/v1/leagues`                      | —                                         |

### Football (`/api/v1/football/...`)

| Method | Path                                   | Query params                              |
|--------|----------------------------------------|-------------------------------------------|
| GET    | `/api/v1/football/livescores`          | — (matches in play)                       |
| GET    | `/api/v1/football/matches`             | `date`, `from`+`to`, `season`, `league`   |
| GET    | `/api/v1/football/matches/{id}`        | — (single match detail)                   |
| GET    | `/api/v1/football/standings`           | `season` (numeric season id, required)    |
| GET    | `/api/v1/football/leagues`             | —                                         |

`date`, `from`, `to` are `YYYY-MM-DD`. If `/api/v1/matches` is called with no
filter, it defaults to the next 7 days so it never dumps the whole catalogue.
`league`/`season`/`team` take SportMonks numeric ids (find them via `/leagues`).

### Examples

```bash
curl "http://localhost:8090/api/v1/livescores"
curl "http://localhost:8090/api/v1/matches?from=2026-07-01&to=2026-07-08"
curl "http://localhost:8090/api/v1/matches/70025/scorecard"
curl "http://localhost:8090/api/v1/standings?season=1715"
curl "http://localhost:8090/api/v1/rankings?type=TEST&gender=men"
curl "http://localhost:8090/api/v1/leagues"
```

## Troubleshooting: `x509: certificate signed by unknown authority`

The server can't verify SportMonks' TLS cert. Diagnose the issuer:

```bash
echo | openssl s_client -connect cricket.sportmonks.com:443 \
  -servername cricket.sportmonks.com 2>/dev/null | openssl x509 -noout -issuer
```

- **Issuer is a real CA** (Let's Encrypt, Google Trust Services, …) → your CA
  store is stale: `sudo apt-get install -y ca-certificates && sudo update-ca-certificates`.
- **Issuer is a proxy/company/self-signed** → a TLS-intercepting proxy is in the
  path. Either trust its CA, set `SPORTMONKS_INSECURE_SKIP_VERIFY=true` (dev only,
  works only if the proxy forwards to SportMonks), or move to a network that can
  reach `cricket.sportmonks.com` directly.

## Notes

- **Auth**: the SportMonks token is sent as the `api_token` query param on every
  upstream request (handled in `internal/sportmonks/client.go`).
- **Include wrapper**: SportMonks usually wraps included relations as
  `{"data": ...}`, but not uniformly (e.g. livescores returns `runs` as a bare
  array). `Data[T].UnmarshalJSON` tolerates both shapes.
- **Caching**: static data (standings, leagues, schedule) uses `CACHE_TTL`
  (default 5m); volatile data (livescores, today's matches, scorecards) uses
  `CACHE_TTL_LIVE` (default 20s). Protects your upstream quota.
- **`live` flag on matches**: SportMonks sets `live: true` on any fixture that
  *will* have live coverage, not only those in play right now — the DTO passes it
  through verbatim. Use `status` (e.g. `NS`, `1st Innings`, `Finished`) for state.
- **Cricket rankings**: verified against live data. The stats live in a nested
  `ranking` object (`{position, matches, points, rating}`) per team — mapped in
  `internal/sportmonks/types.go` (`RankedTeam`/`RankingStats`).
- **Football standings columns (verify)**: SportMonks v3 exposes played/won/draw/
  lost/goals as `details` keyed by numeric `type_id`. The ids in
  `internal/sports/service_football.go` (`ftType*`) are best-effort; confirm them
  against a live standings payload before relying on those columns. Position,
  team, and points are read directly and are safe. (Requires a football token.)
- **Next steps**: persist to Postgres for history, add a scheduled sync
  (goroutine + ticker) to pre-warm the cache, and add auth/rate-limiting on your
  own endpoints.
```
