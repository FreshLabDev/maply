/* ---------- Leaderboards at /top: the rating first, the free play's boards beside it ---------- */
// The rating, GET /api/rating?period=&circle=&league=&sort=, ranks the daily game over the days over everywhere
// (internal/rating): the best 20 of the last 30 by default, four cards, a league for each from five days. The free
// play's boards, GET /api/top?board=&seconds=&rounds=&period=, are each player's best on one setting, over the last
// seven days or all time; only games on a timer are on a board. Both send the first hundred and the viewer's own row.
//
//   Leaders.show(el)                      draws the page into el: the /top screen's page gives it
//   Leaders.open({ board, seconds, rounds }) opens the page on a free play board, from the profile's places
//   Leaders.label({ board, seconds, rounds }) "Blitz · 5 places": the name of a board and its setting
//
// A name with a username opens the player's profile (profile.js listens for [data-u]). Everything else stays inside this
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
  // The rating's choices, as internal/rating names them; the first of each is the default.
  const PERIODS = ["30", "7", "all"], PERIOD_T = { 7: "rate.p7", 30: "rate.p30", all: "rate.pall" };
  const CIRCLES = ["world", "groups", "played"];
  const LEAGUES = ["cartographer", "navigator", "traveler", "tourist", "novice"];
  const SORTS = ["score", "avg", "days", "km"];
  const STORE = "maply.lead";
  const board = (id) => BOARDS.find((b) => b.id === id) || BOARDS[0];
  const one = (v, list) => list.includes(v) ? v : list[0];
  // A text picked at run time, a choice's: web_test.go lists the keys this builds.
  const tk = (prefix, id) => T(prefix + id);

  // What the page shows: the view, the rating's filters, the board, its period, and the setting picked on each board,
  // kept for the next visit.
  let pick = { view: "rating", rate: {}, board: "streak", period: "all", s: {}, r: {} };
  try { pick = Object.assign(pick, JSON.parse(localStorage.getItem(STORE)) || {}); } catch {}
  pick.rate = Object.assign({ period: "30", circle: "world", league: "", sort: "score" }, pick.rate);
  const keep = () => { try { localStorage.setItem(STORE, JSON.stringify(pick)); } catch {} };

  let box = null, ticket = 0, wide = false;
  // The circles the viewer may pick, as the last reply named them: a guest has the world alone.
  let circles = ["world"];
  // Replies for a minute, so going back to a tab draws at once; a board changes only when a game ends, the rating when
  // a day is over everywhere.
  const cache = new Map();

  // The board's setting as picked, a setting it does not have its first.
  const secsOf = (b) => b.secs.includes(pick.s[b.id]) ? pick.s[b.id] : b.secs[0];
  const roundsOf = (b) => b.rounds.includes(pick.r[b.id]) ? pick.r[b.id] : b.rounds[0] || 0;
  function query() {
    if (pick.view === "rating") {
      const r = pick.rate;
      return "/api/rating?period=" + r.period + "&circle=" + r.circle + "&sort=" + r.sort + (r.league ? "&league=" + r.league : "");
    }
    const b = board(pick.board), r = roundsOf(b);
    return "/api/top?board=" + b.id + "&period=" + pick.period + "&seconds=" + secsOf(b) + (r ? "&rounds=" + r : "");
  }

  function label(p) {
    const b = board(p.board), parts = [T(b.name)];
    if (p.board === "streak" || p.board === "classic") parts.push(T("free.sec", { n: fmt(p.seconds) }));
    if (p.rounds && ROUNDS[p.rounds]) parts.push(T(ROUNDS[p.rounds]));
    return parts.join(" · ");
  }

  // A row of buttons, one pressed.
  const chips = (k, list, now, text, name) => '<div class="lead-chips" role="group" aria-label="' + esc(name) + '">' +
    list.map((v) => '<button data-k="' + k + '" data-v="' + v + '" aria-pressed="' + (v === now) + '">' + esc(text(v)) + '</button>').join("") + '</div>';

  function show(el) {
    box = el;
    const page = !el.closest("#sheet");
    wide = page;
    // The page's own Back, to the menu, at the start of the title: on a phone or a tablet in a browser only. Telegram
    // has its Back button, a computer its header (leaders.css hides this one under html.desk), the sheet its own.
    const back = page && !inTg ? '<button class="sh-b lead-back" aria-label="' + esc(T("free.back")) + '">' + (fwd() === "→" ? "←" : "→") + '</button>' : "";
    // On a page of its own the computer lays it out in columns; in the sheet it keeps the phone's one.
    box.innerHTML = '<div class="lead-page' + (page ? " wide" : "") + '">' +
      '<header class="lead-head"><div class="lead-top">' + back + '<h2 class="lead-h">' + esc(T("lead.title")) + '</h2></div><p class="lead-sub" id="leadSub"></p></header>' +
      '<div class="lead-views" role="tablist" aria-label="' + esc(T("lead.title")) + '">' +
        '<button role="tab" id="lead-v-rating" data-view="rating" aria-controls="leadView">' + esc(T("rate.view_rating")) + '</button>' +
        '<button role="tab" id="lead-v-free" data-view="free" aria-controls="leadView">' + esc(T("rate.view_free")) + '</button></div>' +
      '<div id="leadView" role="tabpanel"></div></div>';
    if (back) box.querySelector(".lead-back").onclick = up;
    const views = box.querySelector(".lead-views");
    views.onclick = (e) => { const b = e.target.closest("[data-view]"); if (b && b.dataset.view !== pick.view) setView(b.dataset.view); };
    views.onkeydown = (e) => {
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
      e.preventDefault();
      setView(pick.view === "rating" ? "free" : "rating");
      box.querySelector("#lead-v-" + pick.view).focus();
    };
    drawView();
  }

  function setView(v) {
    pick.view = v === "free" ? "free" : "rating";
    keep();
    haptic("select");
    drawView();
  }

  // The view's frame: its tabs, options and empty table, filled in by load.
  function drawView() {
    const rating = pick.view === "rating", view = box.querySelector("#leadView");
    box.querySelectorAll(".lead-views [data-view]").forEach((b) => {
      const on = b.dataset.view === pick.view;
      b.setAttribute("aria-selected", on); b.tabIndex = on ? 0 : -1;
    });
    view.setAttribute("aria-labelledby", "lead-v-" + pick.view);
    if (rating) {
      view.innerHTML = '<div class="rate' + (wide ? " rate-wide" : "") + '">' +
        '<div class="rate-cards" id="rateCards"></div>' +
        '<div class="rate-main"><div class="lead-opts rate-opts" id="leadOpts"></div><div class="lead-list" id="leadList"></div></div>' +
        '<aside class="rate-side"><div class="lead-you" id="leadYou" aria-live="polite"></div><p class="lead-rule">' + esc(T("rate.rule")) + '</p>' +
        '<a class="lead-play" href="/" data-route>' + esc(T("rate.play")) + ' ' + fwd() + '</a></aside></div>';
      const opts = view.querySelector("#leadOpts");
      opts.onclick = (e) => {
        const c = e.target.closest("[data-k]");
        if (c && c.getAttribute("aria-pressed") !== "true") rate({ [c.dataset.k]: c.dataset.v });
      };
      opts.onchange = (e) => { if (e.target.dataset.k) rate({ [e.target.dataset.k]: e.target.value }); };
    } else {
      view.innerHTML = '<div class="lead' + (wide ? " lead-wide" : "") + '">' +
        '<div class="lead-boards" role="tablist" aria-label="' + esc(T("rate.view_free")) + '">' + BOARDS.map((b) =>
          '<button role="tab" id="lead-t-' + b.id + '" data-board="' + b.id + '" aria-controls="leadPanel"><b>' + esc(T(b.name)) + '</b><small>' + esc(T(b.what)) + '</small></button>').join("") + '</div>' +
        '<div class="lead-main" id="leadPanel" role="tabpanel"><div class="lead-opts" id="leadOpts"></div><div class="lead-list" id="leadList"></div></div>' +
        '<aside class="lead-side"><div class="lead-you" id="leadYou" aria-live="polite"></div><p class="lead-rule">' + esc(T("lead.rule")) + '</p>' +
        '<a class="lead-play" href="/play" data-route>' + esc(T("lead.play")) + ' ' + fwd() + '</a></aside></div>';
      const tabs = view.querySelector(".lead-boards");
      tabs.onclick = (e) => { const b = e.target.closest("[data-board]"); if (b) choose({ board: b.dataset.board }); };
      tabs.onkeydown = tabKey;
      view.querySelector("#leadOpts").onclick = (e) => {
        const c = e.target.closest("[data-k]");
        if (!c || c.getAttribute("aria-pressed") === "true") return;
        const v = c.dataset.v;
        if (c.dataset.k === "period") choose({ period: v });
        else choose({ [c.dataset.k]: Number(v) });
      };
    }
    load();
  }

  // Out of the page as the days and the profile go, and as Escape does (menu.js): to the page before, or to the menu
  // when this was the first.
  function up() {
    haptic("light");
    Router.back("/");
  }

  // A change of the rating's period, circle, league or order.
  function rate(c) {
    const r = pick.rate;
    if (c.period) r.period = one(c.period, PERIODS);
    if (c.circle) r.circle = one(c.circle, circles);
    if ("league" in c) r.league = LEAGUES.includes(c.league) ? c.league : "";
    if (c.sort) r.sort = one(c.sort, SORTS);
    keep();
    haptic("select");
    load();
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
    const mine = ++ticket, q = query(), rating = pick.view === "rating";
    box.querySelector("#leadSub").textContent = rating ? tk("rate.sub_", pick.rate.period) : T("lead.sub");
    if (rating) paintRateOpts(); else { paintTabs(); paintOpts(); }
    const hit = cache.get(q);
    if (hit && Date.now() - hit.at < 60000) return rating ? paintRate(hit.d) : paint(hit.d);
    box.querySelector("#leadList").setAttribute("aria-busy", "true");
    let d;
    try { d = await getJSON(q); } catch (e) {
      if (mine !== ticket || !box.isConnected) return;
      const list = box.querySelector("#leadList");
      list.removeAttribute("aria-busy");
      ready();
      list.innerHTML = '<p class="lead-wait">' + esc(errorText(e)) + '</p><button class="lead-retry">' + esc(T("free.retry")) + '</button>';
      list.querySelector(".lead-retry").onclick = load;
      return;
    }
    cache.set(q, { d, at: Date.now() });
    if (mine === ticket && box.isConnected) rating ? paintRate(d) : paint(d);
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
    const some = (k, list, now, text, name) => list.length < 2 ? "" : chips(k, list, now, text, name);
    box.querySelector("#leadOpts").innerHTML =
      some("period", ["week", "all"], pick.period, (v) => T(v === "week" ? "lead.week" : "lead.all"), T("lead.period")) +
      some("seconds", up(b.secs), secsOf(b), (v) => T("free.sec", { n: fmt(v) }), T("free.time")) +
      some("rounds", up(b.rounds), roundsOf(b), (v) => T(ROUNDS[v]), T("free.places"));
  }

  // The rating's period and circle as buttons; the league and the order, which have more choices, as lists.
  function paintRateOpts() {
    const r = pick.rate;
    const list = (k, values, now, text, name) => '<label class="rate-pick"><span>' + esc(name) + '</span><select data-k="' + k + '" aria-label="' + esc(name) + '">' +
      values.map((v) => '<option value="' + v + '"' + (v === now ? " selected" : "") + '>' + esc(text(v)) + '</option>').join("") + '</select></label>';
    box.querySelector("#leadOpts").innerHTML =
      chips("period", ["7", "30", "all"], r.period, (v) => T(PERIOD_T[v]), T("lead.period")) +
      (circles.length > 1 ? chips("circle", circles, r.circle, (v) => tk("rate.c_", v), T("rate.circle")) : "") +
      '<div class="rate-lists">' +
        list("league", [""].concat(LEAGUES), r.league, (v) => v ? tk("rate.l_", v) : T("rate.l_all"), T("rate.league")) +
        list("sort", SORTS, r.sort, (v) => tk("rate.s_", v), T("rate.sort")) + '</div>';
  }

  // The tables need nothing of the day: opened at the start, the loading screen goes with the first board, not with
  // the day's picture.
  const ready = () => { if (typeof hideLoader === "function") hideLoader(); };

  // A row: the place, the face, the name with what else it has, and the number the table is ordered by.
  function row(r, i, rows, value, sub) {
    // A rank that skips ahead: a dashed line says there are rows between.
    const cls = [r.me ? "me" : "", r.rank === 1 ? "m1" : "", i && r.rank > rows[i - 1].rank + 1 ? "skip" : ""].filter(Boolean).join(" ");
    const name = r.me ? T("you") : nameOf(r);
    return '<li' + (cls ? ' class="' + cls + '"' : "") + '><em>' + fmt(r.rank) + '</em><span class="lead-a">' + faceOf(r) + '</span>' +
      '<span class="lead-w"><span class="lead-n"' + (r.u && !r.me ? ' data-u="' + esc(r.u) + '" role="button" tabindex="0"' : "") + '>' +
      // A name in its own direction, inside a row that keeps the page's: an Arabic name stays at the start.
      (r.me ? esc(name) : '<bdi lang="' + nameLang() + '">' + esc(name) + '</bdi>') + '</span>' +
      (sub ? '<small>' + esc(sub) + '</small>' : "") + '</span><b>' + esc(value) + '</b></li>';
  }

  function paint(d) {
    const b = board(d.board), list = box.querySelector("#leadList");
    list.removeAttribute("aria-busy");
    ready();
    const me = d.rows.find((r) => r.me);
    let h = '<div class="lead-th up"><span>#</span><span></span><span>' + esc(T("lead.player")) + '</span><span>' + esc(T(b.unit)) + '</span></div>';
    if (!d.rows.length) h += '<p class="lead-wait">' + esc(T(d.period === "week" ? "lead.empty_week" : "lead.empty")) + '</p>';
    else h += '<ol class="lead-rows">' + d.rows.map((r, i) => row(r, i, d.rows, fmt(r.score))).join("") + '</ol>';
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

  /* ---------- The rating ---------- */
  const leagueT = (id) => tk("rate.l_", id);
  // The number a line is ordered by.
  const measure = (r, by) => by === "avg" ? fmt(r.avg) : by === "days" ? fmt(r.days) : by === "km" ? kmT(r.km) : fmt(r.score);
  // The league, and the days or, ordered by days, the average: what a phone's row has room for beside the number.
  const subOf = (r, by) => leagueT(r.league) + " · " + (by === "days" ? T("rate.avg_n", { n: fmt(r.avg) }) : T("prof.days_n", { n: fmt(r.days) }));
  const shortDate = (iso) => typeof dayPart === "function" ? dayPart(iso, { day: "numeric", month: "short" }) : iso;

  function paintRate(d) {
    const list = box.querySelector("#leadList");
    list.removeAttribute("aria-busy");
    ready();
    // The circles the viewer has: a choice they no longer have goes back to the world.
    const had = circles.join();
    circles = (d.circles && d.circles.length ? d.circles : ["world"]).filter((c) => CIRCLES.includes(c));
    if (!circles.includes(pick.rate.circle)) { pick.rate.circle = "world"; keep(); load(); return; }
    if (circles.join() !== had) paintRateOpts();

    const by = d.sort, me = d.rows.find((r) => r.me);
    let h = '<div class="lead-th up"><span>#</span><span></span><span>' + esc(T("lead.player")) + '</span><span>' + esc(tk("rate.s_", by)) + '</span></div>';
    if (!d.rows.length) h += '<p class="lead-wait">' + esc(T("rate.empty")) + '</p>';
    else h += '<ol class="lead-rows rate-rows">' + d.rows.map((r, i) => row(r, i, d.rows, measure(r, by), subOf(r, by))).join("") + '</ol>';
    if (d.total) h += '<p class="lead-count">' + esc(T("lead.players", { n: fmt(d.total) })) + '</p>';
    list.innerHTML = h;
    box.querySelector("#rateCards").innerHTML = cardsHTML(d.cards || {});
    box.querySelector("#leadYou").innerHTML = rateYouHTML(d, me);
  }

  // The four standouts of the table as it is filtered; one nobody qualifies for keeps its place, empty.
  function cardsHTML(c) {
    const card = (k, value) => {
      const p = c[k];
      return '<div class="rate-card' + (p ? "" : " none") + '"><span class="up">' + esc(tk("rate.card_", k)) + '</span>' +
        (p ? '<span class="rate-cp"><span class="lead-a">' + faceOf(p) + '</span><span class="lead-n"' + (p.u && !p.me ? ' data-u="' + esc(p.u) + '" role="button" tabindex="0"' : "") + '>' +
          (p.me ? esc(T("you")) : '<bdi lang="' + nameLang() + '">' + esc(nameOf(p)) + '</bdi>') + '</span></span><small>' + esc(value(p)) + '</small>'
          : '<span class="rate-cp"><span class="lead-n">–</span></span><small>&nbsp;</small>') + '</div>';
    };
    return card("active", (p) => T("prof.days_n", { n: fmt(p.n) })) +
      card("accurate", (p) => T("rate.km_n", { km: kmT(p.km) })) +
      card("day", (p) => fmt(p.n) + " · " + shortDate(p.date)) +
      card("streak", (p) => T("rate.streak_n", { n: fmt(p.n) }));
  }

  // The viewer's league over the period among everyone, and how far the next one is: the average it starts at, or for
  // a new player the days still to play.
  function rateYouHTML(d, row) {
    if (d.guest) return '<p class="lead-wait">' + esc(T("rate.guest")) + '</p>';
    const me = d.me;
    if (!me) return '<p class="lead-wait">' + esc(T("rate.you_none")) + '</p>';
    let pct = 1, line = T("rate.top");
    if (me.league === "novice") {
      pct = Math.min(1, me.days / 5);
      line = T("rate.need", { n: fmt(me.need_days) });
    } else if (me.next) {
      const from = ((d.leagues || []).find((l) => l.id === me.league) || { from: 0 }).from;
      pct = Math.max(0, Math.min(1, (me.avg - from) / Math.max(1, me.next_from - from)));
      line = T("rate.next", { league: leagueT(me.next), n: fmt(me.next_from) });
    }
    return '<span class="up">' + esc(T("rate.your_league")) + '</span><b>' + esc(leagueT(me.league)) + '</b>' +
      '<span class="rate-bar" role="presentation"><i style="width:' + Math.round(pct * 100) + '%"></i></span>' +
      '<small>' + esc(T("rate.avg_n", { n: fmt(me.avg) }) + " · " + line) + '</small>' +
      (row ? '<small>' + esc(T("lead.you")) + ': ' + esc(T("prof.place_of", { rank: fmt(row.rank), of: fmt(d.total) })) + '</small>' : "");
  }

  // From the profile: the free play's board, on the /top page where the router has one, else the page in the sheet
  // over the profile.
  function open(p) {
    pick.view = "free";
    if (p && p.board) {
      pick.board = board(p.board).id;
      if (p.seconds) pick.s[pick.board] = p.seconds;
      if (p.rounds) pick.r[pick.board] = p.rounds;
      pick.period = "all";
    }
    keep();
    if (Router.nav().some((n) => n.href === "/top")) { Router.go("/top"); return; }
    const saved = typeof keepSheet === "function" ? keepSheet() : null;
    sheet(esc(T("lead.title")), '<div id="leadSheet"></div>', saved ? () => restoreSheet(saved) : null);
    $("sheet").dataset.kind = "lead";
    show($("leadSheet"));
  }

  return { show, open, label };
})();
