"use strict";

// Same-origin API (served by the Go backend), so no CORS and no config needed.
const API = "";

// Default season ids used by the standings shortcut (adjust to current seasons).
const SEASON = { cricket: 1715, football: 23000 };

// Actual team names (used for filtering matches by team).
const TEAMS_FOOTBALL = ["arsenal", "chelsea", "liverpool", "manchester", "man city", "tottenham", "spurs"];
const TEAMS_CRICKET = ["india", "england", "australia", "sweden", "portugal", "south africa",
  "new zealand", "sri lanka", "bangladesh", "pakistan", "west indies", "zimbabwe", "ireland", "afghanistan"];

// Sport-detection keywords = sport words + the team names (NOT used as team filters).
const KW_FOOTBALL = ["football", "soccer", "goal", "premier", "epl", "la liga", "bundesliga", ...TEAMS_FOOTBALL];
const KW_CRICKET = ["cricket", "t20", "odi", "test", "wicket", "innings", "ipl", ...TEAMS_CRICKET];

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
  const res = await fetch(API + path);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body;
}

// ---------- intent routing ----------

function detectSport(q) {
  if (KW_FOOTBALL.some((w) => q.includes(w))) return "football";
  if (KW_CRICKET.some((w) => q.includes(w))) return "cricket";
  return null; // unknown -> both
}

// Only real team names count as a team filter (never "cricket"/"football"/etc.).
function detectTeam(q) {
  return [...TEAMS_FOOTBALL, ...TEAMS_CRICKET].find((t) => q.includes(t)) || null;
}

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

// YYYY-MM-DD, `offset` days from today (UTC).
function isoDate(offset) {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() + offset);
  return d.toISOString().slice(0, 10);
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

async function route(query) {
  const q = query.toLowerCase().trim();
  const sport = detectSport(q);
  const team = detectTeam(q);

  if (/(stand|table|points)/.test(q)) return handleStandings(sport || "football");
  if (/rank/.test(q)) return handleRankings(q);

  // A named cricket format (test/oneday/t20) implies cricket + a type filter.
  const format = detectFormat(q);
  const effSport = format ? "cricket" : sport;

  let action = "live";
  if (/(match|fixture|schedule|upcoming|result|game|list|near|next)/.test(q)) action = "matches";
  else if (/(live|score|now|playing)/.test(q)) action = "live";
  else if (team || format) action = "matches"; // bare team/format -> show fixtures
  return handleMatches(effSport, action, team, format);
}

// ---------- handlers ----------

async function handleMatches(sport, action, team, format) {
  const wantCricket = sport !== "football";
  const wantFootball = sport !== "cricket";
  const live = action === "live";

  let cricket = [], football = [];
  if (wantCricket) {
    let path = live ? "/api/v1/livescores" : "/api/v1/matches";
    // Widen the window for a format query so scheduled Tests/ODIs actually appear
    // (the default is only the next 7 days).
    if (!live && format) path = `/api/v1/matches?from=${isoDate(0)}&to=${isoDate(120)}`;
    cricket = (await api(path)).data || [];
    if (format) cricket = cricket.filter((m) => matchesFormat(m.type, format));
  }
  if (wantFootball) {
    football = (await api(live ? "/api/v1/football/livescores" : "/api/v1/football/matches")).data || [];
  }

  if (team) {
    cricket = cricket.filter((m) => hit(m.localTeam, team) || hit(m.visitorTeam, team));
    football = football.filter((m) => hit(m.homeTeam, team) || hit(m.awayTeam, team));
  }

  const total = cricket.length + football.length;
  const fmt = format ? `${format} ` : "";
  if (!total) {
    addBotText(`No ${fmt}${live ? "live " : ""}matches found${team ? ` for “${cap(team)}”` : ""}. Try “live football” or a team name.`, "err");
    return;
  }

  const grid = document.createElement("div");
  grid.className = "cards" + (total > 1 ? " two" : "");
  cricket.forEach((m) => grid.appendChild(cricketCard(m)));
  football.forEach((m) => grid.appendChild(footballCard(m)));
  const label = live ? "Live now" : `${fmt}Fixtures & results`;
  addBotNode(`${label} — ${total} match${total > 1 ? "es" : ""}${team ? ` · ${cap(team)}` : ""}`, grid);
  wireTilt(grid);
}

async function handleStandings(sport) {
  const season = SEASON[sport];
  const path = sport === "football" ? `/api/v1/football/standings?season=${season}` : `/api/v1/standings?season=${season}`;
  const rows = (await api(path)).data || [];
  if (!rows.length) { addBotText("No standings available.", "err"); return; }

  const isFb = sport === "football";
  const head = isFb
    ? ["#", "Team", "P", "W", "D", "L", "GD", "Pts"]
    : ["#", "Team", "P", "W", "L", "Pts"];
  const body = rows.map((r) => isFb
    ? [r.position, r.team, r.played, r.won, r.draw, r.lost, r.goalDifference, r.points]
    : [r.position, r.team, r.played, r.won, r.lost, r.points]);
  const card = tableCard(`${cap(sport)} standings`, head, body, isFb ? [0, 2, 3, 4, 5, 6, 7] : [0, 2, 3, 4, 5]);
  addBotNode(`${cap(sport)} table`, card);
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
  const live = m.live && m.status !== "NS" && m.status !== "Finished";
  const inns = (m.innings || []).reduce((acc, i) => (acc[i.team] = `${i.runs}/${i.wickets} (${i.overs})`, acc), {});
  const c = card();
  c.innerHTML = `
    <div class="comp"><span>🏏 ${esc(m.type || "Cricket")} · ${esc(m.round || "")}</span>${live ? '<span class="live-badge">LIVE</span>' : `<span>${esc(m.status)}</span>`}</div>
    ${m.startingAt ? `<div class="when">📅 ${esc(fmtDateTime(m.startingAt))}</div>` : ""}
    <div class="teams">
      ${teamRow(m.localTeam, inns[m.localTeam])}
      <div class="divider"></div>
      ${teamRow(m.visitorTeam, inns[m.visitorTeam])}
    </div>
    ${m.note ? `<div class="result">🏆 ${esc(m.note)}</div>` : ""}`;
  return c;
}

function footballCard(m) {
  const live = (m.statusShort || "").startsWith("INPLAY");
  const c = card();
  c.innerHTML = `
    <div class="comp"><span>⚽ Football</span>${live ? '<span class="live-badge">LIVE</span>' : `<span>${esc(m.status || m.statusShort)}</span>`}</div>
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
  try {
    await route(query);
  } catch (e) {
    addBotText("⚠️ " + esc(e.message), "err");
  } finally {
    typing.remove();
  }
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
