"use strict";

// Same-origin API (served by the Go backend), so no CORS and no config needed.
const API = "";

// Default cricket season id for the standings shortcut (adjust to current season).
const CRICKET_SEASON = 1715;

// Map free text to a football-data.org competition code for standings.
function footballCompetition(q) {
  if (/premier|\bepl\b|\bpl\b/.test(q)) return "PL";
  if (/la ?liga/.test(q)) return "PD";
  if (/bundesliga/.test(q)) return "BL1";
  if (/serie ?a/.test(q)) return "SA";
  if (/ligue ?1/.test(q)) return "FL1";
  if (/champions/.test(q)) return "CL";
  if (/eredivisie/.test(q)) return "DED";
  if (/primeira|portug/.test(q)) return "PPL";
  return "PL"; // default
}

// Teams we can filter matches by. Each entry is [canonical, ...aliases]: the
// canonical is a substring of the upstream team name (used to filter), and any
// alias (incl. short codes like "CSK"/"MI") lets us spot the team in a query.
// Short codes (<= 3 chars) match as WHOLE WORDS only, so "MI" doesn't hit
// "Middlesex"/"Miami" and "DC" doesn't hit a random substring.
const TEAMS = [
  // Cricket nations
  ["india"], ["england"], ["australia"], ["sweden"], ["portugal"],
  ["south africa"], ["new zealand"], ["sri lanka"], ["bangladesh"],
  ["pakistan"], ["west indies"], ["zimbabwe"], ["ireland"], ["afghanistan"],
  // IPL franchises — canonical is the city (matches "Chennai Super Kings" etc.)
  ["chennai", "csk"], ["mumbai", "mi"], ["bangalore", "rcb", "bengaluru", "royal challengers"],
  ["kolkata", "kkr", "knight riders"], ["hyderabad", "srh", "sunrisers"],
  ["delhi", "dc", "capitals"], ["punjab", "pbks", "kings xi"],
  ["rajasthan", "rr", "royals"], ["gujarat", "gt", "titans"], ["lucknow", "lsg"],
  // Football clubs
  ["arsenal"], ["chelsea"], ["liverpool"], ["manchester"], ["man city"], ["tottenham", "spurs"],
];

// Sport is detected ONLY from sport-specific words — NOT team names — so a bare
// team that plays both (e.g. "Portugal") searches BOTH cricket and football.
const KW_FOOTBALL = ["football", "soccer", "goal", "fifa", "premier", "epl", "la liga", "bundesliga", "ligue", "serie a", "champions league"];
const KW_CRICKET = ["cricket", "t20", "odi", "test", "wicket", "innings", "ipl", "the hundred", "big bash"];

const $thread = document.getElementById("thread");
const $form = document.getElementById("composer");
const $input = document.getElementById("input");

// ---------- chat plumbing ----------

function addUser(text) {
  const el = div("msg user", `<div class="bubble"></div>`);
  el.querySelector(".bubble").textContent = text;
  $thread.appendChild(el);
  scroll();
}

function addBotText(html, cls = "") {
  const el = div("msg bot", `<div class="bubble ${cls}">${html}</div>`);
  $thread.appendChild(el);
  scroll();
  return el;
}

function addBotNode(headline, node) {
  const el = div("msg bot", "");
  if (headline) {
    const b = div("bubble", "");
    b.textContent = headline;
    el.appendChild(b);
  }
  el.appendChild(node);
  $thread.appendChild(el);
  scroll();
}

function addTyping() {
  const el = div("msg bot", `<div class="bubble typing"><span></span><span></span><span></span></div>`);
  $thread.appendChild(el);
  scroll();
  return el;
}

function div(cls, html) {
  const d = document.createElement("div");
  d.className = cls;
  d.innerHTML = html;
  return d;
}
function scroll() { $thread.scrollTop = $thread.scrollHeight; }
const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

// ---------- api ----------

async function api(path) {
  const t0 = performance.now();
  console.log("%c→ REQUEST", "color:#5b8cff", "GET " + API + path);
  const res = await fetch(API + path);
  const body = await res.json().catch(() => ({}));
  const ms = Math.round(performance.now() - t0);
  const summary = body.count !== undefined ? `${body.count} items` : (body.error ? "error: " + body.error : "ok");
  console.log("%c← RESPONSE", "color:#38e8b0", `${res.status} ${path} · ${summary} · ${ms}ms`, body);
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body;
}

// ---------- intent routing ----------

function detectSport(q) {
  if (KW_FOOTBALL.some((w) => q.includes(w))) return "football";
  if (KW_CRICKET.some((w) => q.includes(w))) return "cricket";
  return null; // unknown -> both
}

// Canonical team substrings named in the query (never "cricket"/"football"/etc.).
// A query like "CSK vs MI" yields ["chennai", "mumbai"] so we can require a match
// to involve EVERY named team rather than silently dropping one — or showing all.
function detectTeams(q) {
  const found = [];
  for (const [canonical, ...aliases] of TEAMS) {
    const named = [canonical, ...aliases].some((a) =>
      a.length <= 3 ? new RegExp(`\\b${a}\\b`).test(q) : q.includes(a));
    if (named && !found.includes(canonical)) found.push(canonical);
  }
  return found; // [] | ["chennai"] | ["chennai", "mumbai"]
}

// Named competitions/tournaments. `re` matches both the query and the upstream
// league name, so we can filter matches to a tournament (and say when none run).
const COMPETITIONS = [
  { label: "World Cup", re: /world ?cup|fifa/ },
  { label: "Champions Trophy", re: /champions trophy/ },
  { label: "IPL", re: /\bipl\b|indian premier/ },
  { label: "The Hundred", re: /the hundred|\bhundred\b/ },
  { label: "Big Bash", re: /big ?bash|\bbbl\b/ },
  { label: "Ashes", re: /\bashes\b/ },
  { label: "Champions League", re: /champions league|\bucl\b/ },
];
function detectCompetition(q) {
  return COMPETITIONS.find((c) => c.re.test(q)) || null;
}

// football-data.org competition codes for competitions that exist in football.
const FB_COMP_CODE = { "World Cup": "WC", "Champions League": "CL" };

// Map free-text cricket format to a ranking type (TEST / ODI / T20I).
// Understands synonyms: "oneday", "one day", "50 over" -> ODI; "twenty20" -> T20I.
function rankingType(q) {
  if (/t20|twenty ?20|twenty-?20/.test(q)) return "T20I";
  if (/odi|one[ -]?day|50\s*-?\s*overs?/.test(q)) return "ODI";
  return "TEST";
}

// Detect a cricket match format in a query, or null. Same synonyms as rankings.
function detectFormat(q) {
  if (/t20|twenty ?20|twenty-?20/.test(q)) return "T20I";
  if (/odi|one[ -]?day|50\s*-?\s*overs?/.test(q)) return "ODI";
  if (/\btest\b/.test(q)) return "Test";
  return null;
}

// True if a fixture's upstream "type" belongs to the requested format.
function matchesFormat(type, format) {
  const t = (type || "").toLowerCase();
  if (format === "T20I") return t.includes("t20") || t.includes("hundred") || t.includes("100");
  if (format === "ODI") return t.includes("odi") || t.includes("odm") || t.includes("list a") || t.includes("50");
  if (format === "Test") return t.includes("test") || t.includes("4day") || t.includes("first-class");
  return true;
}

// YYYY-MM-DD, `offset` days from today in IST (UTC+5:30, no DST).
// "today"/"yesterday"/"tomorrow" are the user's calendar days, not UTC's.
function isoDate(offset) {
  const IST_OFFSET_MIN = 5 * 60 + 30;
  const ist = new Date(Date.now() + IST_OFFSET_MIN * 60000);
  ist.setUTCDate(ist.getUTCDate() + offset);
  return ist.toISOString().slice(0, 10);
}

// Format an upstream timestamp (ISO or "YYYY-MM-DD HH:MM:SS", assumed UTC) for display.
function fmtDateTime(s) {
  if (!s) return "";
  let iso = s.trim().replace(" ", "T").replace(/\.\d+/, "");
  if (!/(Z|[+-]\d\d:?\d\d)$/.test(iso)) iso += "Z";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return s;
  return d.toLocaleString(undefined, { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

// Parse a relative date range from the query, or null for the default window.
function parseDateWindow(q) {
  const day = (o) => isoDate(o);
  if (/\byesterday'?s?\b/.test(q)) return { from: day(-1), to: day(-1), label: "Yesterday" };
  if (/\btomorrow'?s?\b/.test(q)) return { from: day(1), to: day(1), label: "Tomorrow" };
  if (/\btoday'?s?\b/.test(q)) return { from: day(0), to: day(0), label: "Today" };
  if (/last week|past week|last 7 days|last seven days/.test(q)) return { from: day(-7), to: day(0), label: "Last 7 days" };
  if (/next week/.test(q)) return { from: day(7), to: day(14), label: "Next week" };
  if (/this week/.test(q)) return { from: day(0), to: day(7), label: "This week" };
  if (/\b(results?|recent|past|last|latest)\b/.test(q)) return { from: day(-7), to: day(0), label: "Recent results" };
  return null;
}

async function route(query) {
  const q = query.toLowerCase().trim();
  const sport = detectSport(q);
  const teams = detectTeams(q);

  if (/(stand|table|points)/.test(q)) return handleStandings(sport || "football", q);
  if (/rank/.test(q)) return handleRankings(q);

  // A named cricket format (test/oneday/t20) or a batting/bowling question
  // implies cricket (+ a type filter for formats).
  const format = detectFormat(q);
  const effSport = format || /\b(batting|bowling|wicket|innings)\b/.test(q) ? "cricket" : sport;
  const window = parseDateWindow(q); // e.g. "yesterday", "last week", "results"
  const comp = detectCompetition(q); // e.g. "world cup", "ipl"

  let action;
  if (window) action = "matches"; // a dated query is about fixtures/results
  // "Happening now" signals beat the noun "match" (e.g. "the India match"):
  // explicit "live", "now"/"right now", "currently", or a live cricket action.
  else if (/\blive\b|\bnow\b|currently|batting|bowling|winning/.test(q)) action = "live";
  else if (/(match|fixture|schedule|upcoming|result|game|list|near|next)/.test(q)) action = "matches";
  else if (/(score|playing)/.test(q)) action = "live";
  else if (teams.length || format || comp) action = "matches";
  else action = "live";

  console.log("%c⟐ INTENT", "color:#f0b429", {
    query, sport: effSport || "both", action,
    teams: teams.length ? teams : null, format: format || null,
    competition: comp ? comp.label : null, window: window ? window.label : null,
  });
  return handleMatches(effSport, action, teams, format, window, comp);
}

// ---------- handlers ----------

async function handleMatches(sport, action, teams, format, window, comp) {
  const wantCricket = sport !== "football";
  const wantFootball = sport !== "cricket";
  const live = action === "live";
  const range = window ? `?from=${window.from}&to=${window.to}` : "";

  // Fetch each sport independently so one failing (e.g. football with no token)
  // doesn't kill the other.
  let cricket = [], football = [], cricketErr = null, footballErr = null;
  const cricketFetch = (async () => {
    if (!wantCricket) return;
    try {
      let path = live ? "/api/v1/livescores" : "/api/v1/matches";
      if (!live) {
        if (window) path = `/api/v1/matches${range}`;
        // Widen the window for a format query so scheduled Tests/ODIs actually appear.
        else if (format) path = `/api/v1/matches?from=${isoDate(0)}&to=${isoDate(120)}`;
      }
      cricket = (await api(path)).data || [];
      if (format) cricket = cricket.filter((m) => matchesFormat(m.type, format));
    } catch (e) { cricketErr = e.message; }
  })();
  const footballFetch = (async () => {
    if (!wantFootball) return;
    try {
      let fpath = "/api/v1/football/livescores";
      if (!live) {
        const p = new URLSearchParams();
        if (window) { p.set("from", window.from); p.set("to", window.to); }
        const code = comp ? FB_COMP_CODE[comp.label] : null; // e.g. World Cup -> WC
        if (code) p.set("competition", code);
        const qs = p.toString();
        fpath = `/api/v1/football/matches${qs ? "?" + qs : ""}`;
      }
      football = (await api(fpath)).data || [];
    } catch (e) { footballErr = e.message; }
  })();
  await Promise.all([cricketFetch, footballFetch]); // run both sports concurrently

  // Require the match to involve EVERY named team, so "India vs Australia" only
  // matches an India–Australia game — not every India (or Australia) fixture.
  if (teams.length) {
    cricket = cricket.filter((m) => teams.every((t) => hit(m.localTeam, t) || hit(m.visitorTeam, t)));
    football = football.filter((m) => teams.every((t) => hit(m.homeTeam, t) || hit(m.awayTeam, t)));
  }
  const teamLabel = teams.map(cap).join(" vs ");
  // Filter to a named tournament by matching the upstream league name.
  if (comp) {
    cricket = cricket.filter((m) => comp.re.test((m.league || "").toLowerCase()));
    football = football.filter((m) => comp.re.test((m.league || "").toLowerCase()));
  }

  const total = cricket.length + football.length;
  const fmt = format ? `${format} ` : "";
  const when = window ? ` ${window.label.toLowerCase()}` : "";
  if (!total) {
    // Surface an upstream error only for the sport the user actually targeted;
    // for an ambiguous query, ignore football's "no token" and just say none found.
    const relevantErr = sport === "football" ? footballErr : cricketErr;
    if (relevantErr) { addBotText("⚠️ upstream error: " + esc(relevantErr), "err"); return; }
    const forTeams = teams.length ? ` for “${esc(teamLabel)}”` : "";
    if (comp) {
      addBotText(`No <b>${esc(comp.label)}</b> matches found${forTeams}${when} — it may not be running right now.`, "err");
      return;
    }
    addBotText(`No ${fmt}${live ? "live " : ""}matches found${forTeams}${when}. Try “live football” or a team name.`, "err");
    return;
  }

  const grid = document.createElement("div");
  grid.className = "cards" + (total > 1 ? " two" : "");
  cricket.forEach((m) => grid.appendChild(cricketCard(m)));
  football.forEach((m) => grid.appendChild(footballCard(m)));
  const label = live ? "Live now" : window ? `${fmt}${window.label}` : `${fmt}Fixtures & results`;
  const suffix = (teams.length ? ` · ${teamLabel}` : "") + (comp ? ` · ${comp.label}` : "");
  addBotNode(`${label} — ${total} match${total > 1 ? "es" : ""}${suffix}`, grid);
  wireTilt(grid);
}

async function handleStandings(sport, q) {
  const isFb = sport === "football";
  let path, title;
  if (isFb) {
    const comp = footballCompetition(q || "");
    path = `/api/v1/football/standings?competition=${comp}`;
    title = `Football table (${comp})`;
  } else {
    path = `/api/v1/standings?season=${CRICKET_SEASON}`;
    title = "Cricket table";
  }
  const rows = (await api(path)).data || [];
  if (!rows.length) { addBotText("No standings available.", "err"); return; }

  const head = isFb
    ? ["#", "Team", "P", "W", "D", "L", "GD", "Pts"]
    : ["#", "Team", "P", "W", "L", "Pts"];
  const body = rows.map((r) => isFb
    ? [r.position, r.team, r.played, r.won, r.draw, r.lost, r.goalDifference, r.points]
    : [r.position, r.team, r.played, r.won, r.lost, r.points]);
  const card = tableCard(title, head, body, isFb ? [0, 2, 3, 4, 5, 6, 7] : [0, 2, 3, 4, 5]);
  addBotNode(title, card);
  wireTilt(card.parentElement || card);
}

async function handleRankings(q) {
  const type = rankingType(q);
  const all = (await api(`/api/v1/rankings?type=${type}`)).data || [];
  const tables = all.filter((t) => t.teams && t.teams.length); // drop empty (e.g. women w/ no data)
  if (!tables.length) { addBotText(`No ${type} ranking data available.`, "err"); return; }
  const grid = document.createElement("div");
  grid.className = "cards";
  tables.forEach((t) => {
    const body = (t.teams || []).map((x) => [x.position, x.team, x.matches, x.rating, x.points]);
    grid.appendChild(tableCard(`ICC ${t.type} · ${t.gender}`, ["#", "Team", "M", "Rating", "Pts"], body, [0, 2, 3, 4]));
  });
  addBotNode(`Cricket rankings (${type})`, grid);
  wireTilt(grid);
}

// ---------- card builders ----------

function cricketCard(m) {
  const live = m.live; // server already gates this on an in-play status
  const inns = (m.innings || []).reduce((acc, i) => (acc[i.team] = `${i.runs}/${i.wickets} (${i.overs})`, acc), {});
  // Show the competition name (e.g. "ICC Women's T20 World Cup") when present,
  // with the format + round as context.
  const comp = m.league || m.type || "Cricket";
  const sub = [m.league ? m.type : "", m.round].filter(Boolean).join(" · ");
  const c = card();
  c.innerHTML = `
    <div class="comp"><span>🏏 ${esc(comp)}${sub ? " · " + esc(sub) : ""}</span>${live ? '<span class="live-badge">LIVE</span>' : `<span>${esc(m.status)}</span>`}</div>
    ${m.startingAt ? `<div class="when">📅 ${esc(fmtDateTime(m.startingAt))}</div>` : ""}
    <div class="teams">
      ${teamRow(m.localTeam, inns[m.localTeam])}
      <div class="divider"></div>
      ${teamRow(m.visitorTeam, inns[m.visitorTeam])}
    </div>
    ${live ? liveDetail(m) : ""}
    ${m.note ? `<div class="result">🏆 ${esc(m.note)}</div>` : ""}`;
  return c;
}

// Live scorecard block: batting team, current batsmen, bowler, run rates.
function liveDetail(m) {
  const rates = [];
  if (m.battingTeam) rates.push(`<b>${esc(m.battingTeam)}</b> batting`);
  if (m.currentRunRate) rates.push(`CRR ${m.currentRunRate}`);
  if (m.requiredRunRate) rates.push(`RRR ${m.requiredRunRate}`);
  if (m.requiredRuns) rates.push(`Need <b>${m.requiredRuns}</b>`);
  const rateLine = rates.length ? `<div class="rates">${rates.join(" · ")}</div>` : "";

  const bats = (m.batsmen || []).map((b) =>
    `<div class="pl">🏏 ${esc(b.player || "—")}${b.onStrike ? " <em>*</em>" : ""} <span>${b.runs} (${b.balls})</span></div>`).join("");
  const bowl = m.bowler
    ? `<div class="pl">🎯 ${esc(m.bowler.player || "—")} <span>${m.bowler.wickets}/${m.bowler.runs} (${m.bowler.overs})</span></div>`
    : "";

  if (!rateLine && !bats && !bowl) return "";
  return `<div class="live-detail">${rateLine}${bats}${bowl}</div>`;
}

function footballCard(m) {
  const live = m.live; // server flags IN_PLAY/PAUSED
  const c = card();
  c.innerHTML = `
    <div class="comp"><span>⚽ ${esc(m.league || "Football")}</span>${live ? '<span class="live-badge">LIVE</span>' : `<span>${esc(m.status || m.statusShort)}</span>`}</div>
    ${m.startingAt ? `<div class="when">📅 ${esc(fmtDateTime(m.startingAt))}</div>` : ""}
    <div class="teams">
      ${teamRow(m.homeTeam, numOrDash(m.homeGoals))}
      <div class="divider"></div>
      ${teamRow(m.awayTeam, numOrDash(m.awayGoals))}
    </div>
    ${m.resultInfo ? `<div class="result">🏆 ${esc(m.resultInfo)}</div>` : ""}`;
  return c;
}

function teamRow(name, score) {
  return `<div class="team-row"><span class="name">${esc(name || "—")}</span><span class="score">${esc(score ?? "–")}</span></div>`;
}

function tableCard(title, head, rows, numCols) {
  const c = card();
  const nums = new Set(numCols || []);
  const th = head.map((h, i) => `<th class="${nums.has(i) ? "num" : ""}">${esc(h)}</th>`).join("");
  const tr = rows.map((r) => "<tr>" + r.map((v, i) => `<td class="${nums.has(i) ? "num" : ""}">${esc(v)}</td>`).join("") + "</tr>").join("");
  c.innerHTML = `<h3>${esc(title)}</h3><table class="tbl"><thead><tr>${th}</tr></thead><tbody>${tr}</tbody></table>`;
  return c;
}

function card() { const d = document.createElement("div"); d.className = "card"; return d; }
const numOrDash = (n) => (n === null || n === undefined ? "–" : n);
const hit = (v, t) => (v || "").toLowerCase().includes(t);
const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1);

// Subtle 3D "vr" tilt on hover.
function wireTilt(scope) {
  scope.querySelectorAll(".card").forEach((el) => {
    el.addEventListener("mousemove", (e) => {
      const r = el.getBoundingClientRect();
      const rx = ((e.clientY - r.top) / r.height - 0.5) * -8;
      const ry = ((e.clientX - r.left) / r.width - 0.5) * 8;
      el.style.transform = `perspective(700px) rotateX(${rx}deg) rotateY(${ry}deg) translateY(-2px)`;
    });
    el.addEventListener("mouseleave", () => { el.style.transform = ""; });
  });
}

// ---------- events ----------

async function handle(query) {
  addUser(query);
  const typing = addTyping();
  const t0 = performance.now();
  let ok = true;
  try {
    await route(query);
  } catch (e) {
    ok = false;
    addBotText("⚠️ " + esc(e.message), "err");
  } finally {
    typing.remove();
    addTiming(Math.round(performance.now() - t0), ok);
  }
}

// Show how long the query took (round-trip incl. the upstream API call).
function addTiming(ms, ok) {
  const el = div("timing", "");
  const t = ms >= 1000 ? `${(ms / 1000).toFixed(2)} s` : `${ms} ms`;
  el.textContent = `${ok ? "⚡" : "⏱"} responded in ${t}`;
  $thread.appendChild(el);
  scroll();
}

$form.addEventListener("submit", (e) => {
  e.preventDefault();
  const q = $input.value.trim();
  if (!q) return;
  $input.value = "";
  handle(q);
});

document.getElementById("suggestions").addEventListener("click", (e) => {
  const q = e.target.getAttribute("data-q");
  if (q) handle(q);
});

// Live-data build.
(() => {
  const badge = document.getElementById("modeBadge");
  badge.textContent = "live";
  badge.classList.add("live");
})();

// Greeting
addBotText("Hi! I can pull <b>live scores</b>, <b>fixtures</b>, <b>tables</b> and <b>rankings</b> for cricket &amp; football. Tap a chip below or type a team like “Arsenal” or “India”.");
