/* ---------- Leaderboards: the free play's best, at /top ---------- */
// GET /api/top?board=&seconds=&rounds=&period= is one board: each player's best on one setting, over the last seven
// days or all time, the first hundred and the viewer's own row. Only games on a timer are on a board.
//
//   Leaders.show(el)                      draws the page into el: the /top screen's page gives it
//   Leaders.open({ board, seconds, rounds }) opens the page on a board, from the profile's places
//   Leaders.label({ board, seconds, rounds }) "Blitz · 5 places": the name of a board and its setting
//
// A name with a handle opens the mini profile (profile.js listens for [data-who]). Everything else stays inside this
// file: nothing here is a global but window.Leaders.
window.Leaders = (() => {
  // The boards and the settings each keeps a table of, as internal/api/top.go has them; the first is the default.
  const BOARDS = [
    { id: "streak", name: "free.streak", what: "lead.what_streak", unit: "lead.unit_streak", secs: [30, 60], rounds: [] },
    { id: "timed", name: "lead.timed", what: "lead.what_timed", unit: "lead.unit_timed", secs: [180], rounds: [] },
    { id: "blitz", name: "free.blitz", what: "lead.what_blitz", unit: "lead.unit_points", secs: [30], rounds: [5, 3, 10] },
    { id: "classic", name: "free.classic", what: "lead.what_classic", unit: "lead.unit_points", secs: [60, 90, 120], rounds: [5, 3, 10] },
  ];
  const ROUNDS = { 3: "free.r3", 5: "free.r5", 10: "free.r10" };
  const STORE = "maply.lead";
  const board = (id) => BOARDS.find((b) => b.id === id) || BOARDS[0];

  // What the page shows: the board, the period, and the setting picked on each board, kept for the next visit.
  let pick = { board: "streak", period: "all", s: {}, r: {} };
  try { pick = Object.assign(pick, JSON.parse(localStorage.getItem(STORE)) || {}); } catch {}
  const keep = () => { try { localStorage.setItem(STORE, JSON.stringify(pick)); } catch {} };

  let box = null, ticket = 0;
  // Replies for a minute, so going back to a tab draws at once; a board changes only when a game ends.
  const cache = new Map();

  // The board's setting as picked, a setting it does not have its first.
  const secsOf = (b) => b.secs.includes(pick.s[b.id]) ? pick.s[b.id] : b.secs[0];
  const roundsOf = (b) => b.rounds.includes(pick.r[b.id]) ? pick.r[b.id] : b.rounds[0] || 0;
  function query() {
    const b = board(pick.board), r = roundsOf(b);
    return "board=" + b.id + "&period=" + pick.period + "&seconds=" + secsOf(b) + (r ? "&rounds=" + r : "");
  }

  function label(p) {
    const b = board(p.board), parts = [T(b.name)];
    if (p.board === "streak" || p.board === "classic") parts.push(T("free.sec", { n: fmt(p.seconds) }));
    if (p.rounds && ROUNDS[p.rounds]) parts.push(T(ROUNDS[p.rounds]));
    return parts.join(" · ");
  }

  function show(el) {
    box = el;
    const page = !el.closest("#sheet");
    // The page's own Back, to the menu, at the start of the title: on a phone or a tablet in a browser only. Telegram
    // has its Back button, a computer its header (leaders.css hides this one under html.desk), the sheet its own.
    const back = page && !inTg ? '<button class="sh-b lead-back" aria-label="' + esc(T("free.back")) + '">' + (fwd() === "→" ? "←" : "→") + '</button>' : "";
    // On a page of its own the computer lays it out in columns; in the sheet it keeps the phone's one.
    box.innerHTML = '<div class="lead' + (page ? " lead-wide" : "") + '">' +
      '<header class="lead-head"><div class="lead-top">' + back + '<h2 class="lead-h">' + esc(T("lead.title")) + '</h2></div><p class="lead-sub">' + esc(T("lead.sub")) + '</p></header>' +
      '<div class="lead-boards" role="tablist" aria-label="' + esc(T("lead.title")) + '">' + BOARDS.map((b) =>
        '<button role="tab" id="lead-t-' + b.id + '" data-board="' + b.id + '" aria-controls="leadPanel"><b>' + esc(T(b.name)) + '</b><small>' + esc(T(b.what)) + '</small></button>').join("") + '</div>' +
      '<div class="lead-main" id="leadPanel" role="tabpanel"><div class="lead-opts" id="leadOpts"></div><div class="lead-list" id="leadList"></div></div>' +
      '<aside class="lead-side"><div class="lead-you" id="leadYou" aria-live="polite"></div><p class="lead-rule">' + esc(T("lead.rule")) + '</p>' +
      '<a class="lead-play" href="/play" data-route>' + esc(T("lead.play")) + ' ' + fwd() + '</a></aside>' +
      '</div>';
    if (back) box.querySelector(".lead-back").onclick = up;
    const tabs = box.querySelector(".lead-boards");
    tabs.onclick = (e) => { const b = e.target.closest("[data-board]"); if (b) choose({ board: b.dataset.board }); };
    tabs.onkeydown = tabKey;
    box.querySelector("#leadOpts").onclick = (e) => {
      const c = e.target.closest("[data-k]");
      if (!c || c.getAttribute("aria-pressed") === "true") return;
      const v = c.dataset.v;
      if (c.dataset.k === "period") choose({ period: v });
      else choose({ [c.dataset.k]: Number(v) });
    };
    load();
  }

  // Up to the menu: back through the history when the menu is the page before, so this Back and the browser's
  // agree; from another page the menu comes as the next one, and on the first page of a visit it takes its place.
  let before = "";
  Router.on((name, from) => { if (name === "top") before = from; });
  function up() {
    haptic("light");
    if (before === "menu") Router.back("/"); else Router.go("/", { replace: !before });
  }

  // A change of board, period or setting: the tabs and the options show it at once, the rows when they come.
  function choose(c) {
    if (c.board) pick.board = board(c.board).id;
    if (c.period) pick.period = c.period === "week" ? "week" : "all";
    if (c.seconds) pick.s[pick.board] = c.seconds;
    if (c.rounds) pick.r[pick.board] = c.rounds;
    keep();
    haptic("select");
    load();
  }

  // The arrows move between the boards, the other way in a right-to-left language; Home and End go to the ends.
  function tabKey(e) {
    const rtl = document.documentElement.dir === "rtl", i = BOARDS.findIndex((b) => b.id === pick.board);
    const to = { ArrowRight: rtl ? i - 1 : i + 1, ArrowLeft: rtl ? i + 1 : i - 1, Home: 0, End: BOARDS.length - 1 }[e.key];
    if (to == null) return;
    e.preventDefault();
    choose({ board: BOARDS[(to + BOARDS.length) % BOARDS.length].id });
    box.querySelector("#lead-t-" + pick.board).focus();
  }

  async function load() {
    if (!box || !box.isConnected) return;
    const mine = ++ticket, q = query();
    paintTabs();
    paintOpts();
    const hit = cache.get(q);
    if (hit && Date.now() - hit.at < 60000) return paint(hit.d);
    box.querySelector("#leadList").setAttribute("aria-busy", "true");
    let d;
    try { d = await getJSON("/api/top?" + q); } catch (e) {
      if (mine !== ticket || !box.isConnected) return;
      const list = box.querySelector("#leadList");
      list.removeAttribute("aria-busy");
      ready();
      list.innerHTML = '<p class="lead-wait">' + esc(errorText(e)) + '</p><button class="lead-retry">' + esc(T("free.retry")) + '</button>';
      list.querySelector(".lead-retry").onclick = load;
      return;
    }
    cache.set(q, { d, at: Date.now() });
    if (mine === ticket && box.isConnected) paint(d);
  }

  function paintTabs() {
    box.querySelectorAll(".lead-boards [data-board]").forEach((b) => {
      const on = b.dataset.board === pick.board;
      b.setAttribute("aria-selected", on); b.tabIndex = on ? 0 : -1;
    });
    box.querySelector("#leadPanel").setAttribute("aria-labelledby", "lead-t-" + pick.board);
  }

  // The period, then the timer and the length where the board has more than one, shortest first.
  function paintOpts() {
    const b = board(pick.board), up = (list) => list.slice().sort((x, y) => x - y);
    const chips = (k, list, now, text, name) => list.length < 2 ? "" : '<div class="lead-chips" role="group" aria-label="' + esc(name) + '">' +
      list.map((v) => '<button data-k="' + k + '" data-v="' + v + '" aria-pressed="' + (v === now) + '">' + esc(text(v)) + '</button>').join("") + '</div>';
    box.querySelector("#leadOpts").innerHTML =
      chips("period", ["week", "all"], pick.period, (v) => T(v === "week" ? "lead.week" : "lead.all"), T("lead.period")) +
      chips("seconds", up(b.secs), secsOf(b), (v) => T("free.sec", { n: fmt(v) }), T("free.time")) +
      chips("rounds", up(b.rounds), roundsOf(b), (v) => T(ROUNDS[v]), T("free.places"));
  }

  // The tables need nothing of the day: opened at the start, the loading screen goes with the first board, not with
  // the day's picture.
  const ready = () => { if (typeof hideLoader === "function") hideLoader(); };

  function paint(d) {
    const b = board(d.board), list = box.querySelector("#leadList");
    list.removeAttribute("aria-busy");
    ready();
    const me = d.rows.find((r) => r.me);
    let h = '<div class="lead-th up"><span>#</span><span>' + esc(T("lead.player")) + '</span><span>' + esc(T(b.unit)) + '</span></div>';
    if (!d.rows.length) h += '<p class="lead-wait">' + esc(T(d.period === "week" ? "lead.empty_week" : "lead.empty")) + '</p>';
    else h += '<ol class="lead-rows">' + d.rows.map((r, i) => {
      // A rank that skips ahead: a dashed line says there are rows between.
      const cls = [r.me ? "me" : "", r.rank === 1 ? "m1" : "", i && r.rank > d.rows[i - 1].rank + 1 ? "skip" : ""].filter(Boolean).join(" ");
      const name = r.me ? T("you") : r.name || T("guest");
      return '<li' + (cls ? ' class="' + cls + '"' : "") + '><em>' + fmt(r.rank) + '</em><span class="lead-n"' +
        (r.who && !r.me ? ' data-who="' + esc(r.who) + '" role="button" tabindex="0"' : "") + '>' + (r.anon && !r.me ? SRC_ANON : "") +
        // A name in its own direction, inside a row that keeps the page's: an Arabic name stays at the start.
        (r.me ? esc(name) : '<bdi lang="' + nameLang() + '">' + esc(name) + '</bdi>') + '</span><b>' + fmt(r.score) + '</b></li>';
    }).join("") + '</ol>';
    if (d.total) h += '<p class="lead-count">' + esc(T("lead.players", { n: fmt(d.total) })) + '</p>';
    list.innerHTML = h;
    box.querySelector("#leadYou").innerHTML = youHTML(d, me, b);
  }

  // The viewer's place on the board, or why they are not on it.
  function youHTML(d, me, b) {
    if (d.guest) return '<p class="lead-wait">' + esc(T("lead.guest")) + '</p>';
    if (!me) return '<p class="lead-wait">' + esc(T("lead.you_none")) + '</p>';
    return '<span class="up">' + esc(T("lead.you")) + '</span><b>' + esc(T("prof.place_of", { rank: fmt(me.rank), of: fmt(d.total) })) + '</b>' +
      '<small>' + esc(T(b.unit)) + ': ' + fmt(me.score) + '</small>';
  }

  // From the profile: the /top page where the router has one, else the page in the sheet over the profile.
  function open(p) {
    if (p && p.board) {
      pick.board = board(p.board).id;
      if (p.seconds) pick.s[pick.board] = p.seconds;
      if (p.rounds) pick.r[pick.board] = p.rounds;
      pick.period = "all";
      keep();
    }
    if (Router.nav().some((n) => n.href === "/top")) { Router.go("/top"); return; }
    const saved = typeof keepSheet === "function" ? keepSheet() : null;
    sheet(esc(T("lead.title")), '<div id="leadSheet"></div>', saved ? () => restoreSheet(saved) : null);
    $("sheet").dataset.kind = "lead";
    show($("leadSheet"));
  }

  return { show, open, label };
})();
