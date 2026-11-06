/* ---------- The free game with friends ---------- */
// Everyone asks the server every two seconds what has changed; an answer that changes nothing is the version and the
// clock. Which phase the party is in (the countdown, a place, its reveal, the end) is worked out here from the server's
// times, so a phone that was away a moment draws the same screen as the others. Nobody's pin, and nothing of the place,
// comes before the place has closed.
// Every mode is played together. A classic or a day of the archive has its places one after another for all at once.
// A streak is the same, with whoever missed the country watching on. Against the clock each player goes at their own
// pace inside one shared three minutes: the server sends them only the places they have reached, and the others'
// pins come when the game is over for everyone.
(() => {
  "use strict";
  const K = window.FreeKit;
  const EVERY = 2000;
  let P = null, CODE = "", pollT = 0, tickT = 0, drawn = "", drawnV = -1, mine = null, sending = false, sentN = 0;
  // Which screen was up before this one: a reveal straight after its own place keeps the picture.
  let drawnBefore = "";
  // Against the clock, the latest answer whose result the player has looked at; at the end, the place they opened.
  let seen = 0, review = 0;

  /* ---------- Talking to the server ---------- */
  function stop() { clearTimeout(pollT); clearInterval(tickT); pollT = tickT = 0; CODE = ""; P = null; drawn = ""; drawnV = -1; sentN = 0; mine = null; sending = false; seen = review = 0; }
  // An answer older than the party on screen (two requests that crossed) is dropped.
  const newer = (d) => d.code === CODE && (!P || d.v >= P.v);
  function run(view) {
    stop();
    P = view; CODE = view.code;
    tickT = setInterval(render, 250);
    render();
    pollT = setTimeout(poll, EVERY);
  }
  async function poll() {
    clearTimeout(pollT);
    const code = CODE;
    if (!code) return;
    try {
      const d = await K.req("/api/party/" + code + "?v=" + (P ? P.v : 0));
      if (code !== CODE) return;
      if (newer(d)) P = d;
    } catch (e) {
      if (code !== CODE) return;
      if (["not_found", "not_a_member", "game_over"].includes(e.message)) { gone(e); return; }
    }
    // A screen that fails to draw (no WebGL, a picture library error) must not stop the polling.
    try { render(); } finally { if (code === CODE) pollT = setTimeout(poll, EVERY); }
  }
  // A write answers with the party as it is now.
  async function send(path, body) {
    const d = await K.req("/api/party/" + CODE + path, body || {});
    if (newer(d)) { P = d; render(); }
    return d;
  }
  // A party that cannot be opened. A guest of the site may not join one: records and friends come with an account.
  function gone(e) {
    stop();
    K.top(T("party.title"), "");
    const sign = e.message === "sign_in" ? K.signIn() : "";
    K.page((sign || '<p class="fr-wait">' + esc(K.ferr(e)) + '</p>') + '<div class="fr-go"><button class="' + (sign ? "fr-sec" : "fr-pri") + '" data-act="pHome">' + esc(T("free.solo")) + '</button></div>');
  }

  /* ---------- Who is who, and what is played ---------- */
  // A member is a person as every table has them (faceOf, nameOf in app.js), with their number in the party.
  const member = (m) => P.members.find((x) => x.m === m) || { m, name: "?" };
  const nameAt = (m) => nameOf(member(m));
  const meM = () => member(P.me);
  const present = () => P.members.filter((m) => !m.left);
  const isHost = () => P.host === P.me;
  const streak = () => P.settings.mode === "streak";
  const timed = () => P.settings.mode === "timed";
  const roundOf = (n) => P.rounds.find((r) => r.n === n);
  // The number a standing is read by: the streak, the countries found, the points.
  const value = (l) => streak() ? l.streak || 0 : timed() ? l.countries || 0 : l.points;
  // In a streak, a player who was not among those standing for the place only watched it.
  const outOf = (m, r) => streak() && !(r.playing || []).includes(m);
  // Against the clock a place is answered when its result is there; otherwise when the server lists the player.
  const answered = (r) => timed() ? !!r.place : (r.answered || []).includes(P.me);
  // Who the party still waits for in this place, besides you.
  const waiting = (r) => present().filter((m) => m.m !== P.me && m.from <= r.n && !m.away && !outOf(m.m, r) && !(r.answered || []).includes(m.m)).length;

  /* ---------- The phase, from the clock ---------- */
  function phase() {
    if (!P) return { k: "wait" };
    if (P.status === "lobby") return { k: "lobby" };
    if (P.status === "done") {
      const r = review && roundOf(review);
      return r && r.place ? { k: "review", r } : { k: "final" };
    }
    if (P.status !== "playing") return { k: "over" };
    if (timed()) return own();
    const rs = P.rounds, cur = rs[rs.length - 1];
    if (!cur) return { k: "wait" };
    if (cur.closed_at) return { k: "reveal", r: cur, last: true };
    const prev = rs.length > 1 ? rs[rs.length - 2] : null;
    if (K.msTo(cur.starts_at) > 0) {
      if (prev && K.msTo(prev.closed_at) + P.reveal * 1000 > 0) return { k: "reveal", r: prev, next: cur };
      return { k: "count", r: cur };
    }
    // A player who came in during a place plays from the next one.
    if (meM().from > cur.n) return { k: "later", r: cur };
    return { k: "round", r: cur };
  }
  // Against the clock the server sends the places the player has answered and the one they are on. After the
  // countdown they go from one to the next at their own pace, looking at each result as long as they like.
  function own() {
    const rs = P.rounds, cur = rs.find((r) => !r.place) || null, last = rs.filter((r) => r.place).pop() || null;
    const first = cur || last;
    if (!first) return { k: "wait" };
    if (K.msTo(first.starts_at) > 0) return { k: "count", r: first };
    if (last && last.n > seen) return { k: "treveal", r: last, next: cur };
    if (cur) return { k: "round", r: cur };
    return { k: "tend", r: last };
  }
  function render() {
    if (!P) return;
    const ph = phase(), id = ph.k + (ph.r ? ":" + ph.r.n : "");
    const fresh = id !== drawn;
    if (fresh || (["lobby", "final", "later", "tend"].includes(ph.k) && drawnV !== P.v)) {
      if (fresh) drawnBefore = drawn;
      drawn = id; drawnV = P.v;
      DRAW[ph.k](ph, fresh);
    }
    TICK[ph.k] && TICK[ph.k](ph);
  }

  /* ---------- The screens ---------- */
  // The lobby is a page of the top, its way up the menu. Back from a place or the end is the free game's settings,
  // where the party stays open under "Back to friends". The free game keeps one entry of the history, so the menu is
  // what comes before either. Out of a game under way the free game asks first (free.js).
  const back = () => Router.go("/play", { replace: true });
  const placeTitle = (n) => P.day ? K.no("#" + fmt(P.day.n)) : P.settings.rounds ? T("free.place_n", { n: fmt(n), total: fmt(P.settings.rounds) }) : T("free.place", { n: fmt(n) });
  // The line under the title in the game: what the player has so far.
  function sub() {
    const l = P.totals.find((x) => x.m === P.me);
    if (timed()) return T("free.timed_now", { n: fmt(meM().countries || 0) });
    if (streak()) return T("free.streak_now", { n: fmt(l ? l.streak || 0 : 0) });
    return T("party.title");
  }
  // What the mode is, for the lobby; a day from the archive says which day.
  function about() {
    const s = P.settings;
    if (P.day) return '<p class="fr-chby">' + esc(T("free.archive") + " " + K.no("#" + fmt(P.day.n)) + " · " + K.dayDate(P.day.date)) + '</p>' +
      '<p class="fr-hint">' + esc(T("party.day_hint")) + '</p>';
    return '<p class="fr-hint">' + esc(s.mode === "streak" ? T("party.streak_hint") : s.mode === "timed" ? T("party.timed_hint") : T(K.HINTS[K.modeOf(s)])) + '</p>';
  }
  const DRAW = {
    wait() { K.top(T("party.title"), ""); K.wait(); },
    // The players first on a phone; on a computer the settings on the left and the players beside them.
    lobby(ph, fresh) {
      K.top(T("party.title"), T("party.code", { code: P.code }));
      const people = present();
      let html = '<div class="fr-r"><p class="fr-l up">' + esc(T("free.players", { n: fmt(people.length) })) + '</p><ul class="fr-mem">' + people.map((m) => '<li' + (m.m === P.me ? ' class="me"' : '') + '>' + faceOf(m) + '<b lang="' + nameLang() + '">' + esc(nameOf(m)) + '</b>' +
        (m.m === P.host ? '<span class="up">' + esc(T("party.host")) + '</span>' : m.m === P.me ? '<span class="up">' + esc(T("you")) + '</span>' : '') + '</li>').join("") + '</ul></div>';
      html += '<div class="fr-a">' + about() + K.settings(P.settings, true, !isHost());
      if (P.length > 0) html += '<p class="fr-best">' + esc(T("party.length", { n: fmt(Math.max(1, Math.round(P.length / 60))) })) + '</p>';
      html += '<div class="fr-go">';
      if (isHost()) {
        if (people.length < 2) html += '<p class="fr-hint">' + esc(T("party.need_two")) + '</p>';
        html += '<button class="fr-pri" data-act="pStart"' + (people.length < 2 ? " disabled" : "") + '>' + esc(T("party.start")) + '</button>';
      } else html += '<p class="fr-hint">' + esc(T("party.wait_host", { name: nameAt(P.host) })) + '</p>';
      html += '<button class="fr-sec" data-act="pInvite">' + esc(T("party.invite")) + '</button>' +
        '<button class="fr-link" data-act="pLeave">' + esc(T("party.leave")) + '</button></div></div>';
      K.page(html, "fr-lobby", !fresh);
    },
    count(ph) {
      K.top(placeTitle(ph.r.n), T("party.title"), back);
      K.page('<div class="fr-big fr-count"><b id="frCount"></b><span class="up">' + esc(placeTitle(ph.r.n)) + '</span></div>' +
        (ph.r.repeat ? '<p class="fr-hint">' + esc(T("free.repeat")) + '</p>' : ''), "fr-mid");
    },
    later(ph) {
      K.top(placeTitle(ph.r.n), T("party.title"), back);
      K.page('<p class="fr-wait">' + esc(T("party.from", { n: fmt(meM().from) })) + '</p>', "fr-mid");
    },
    // A place: the day's game screen, with the party's clock in the header and who has answered over the picture.
    // Out of a streak, the player still sees the place and only cannot pin it.
    round(ph) {
      const r = ph.r;
      mine = null; sending = false;
      K.top(placeTitle(r.n), sub(), back);
      K.round(r);
      if (r.repeat) toast(T("free.repeat"));
      K.goTo("pSend");
      if (outOf(P.me, r)) K.watch(waiting(r));
      else if (answered(r)) { sentN = r.n; K.sent(waiting(r)); }
      else {
        K.pinned((p) => { mine = p; });
        // Against the clock a place may be passed, which costs the time it took.
        if (timed()) K.skip("pPass", T("free.pass"));
      }
      ribbonNow(r);
      K.clock(r.ends_at, P.settings.seconds, () => {
        // At zero a pin set but not sent goes by itself; without one the place counts as missed when it closes.
        if (mine && !answered(r)) answer(r, true);
      });
    },
    // A place closed: the day's result, with everyone's answer to it and the standing; the next place starts by itself.
    reveal(ph, fresh) {
      K.stopClock(); K.ribbon(null);
      K.top(placeTitle(ph.r.n), sub(), back);
      // Opened in the middle of a reveal, the screen has not shown the place's picture yet.
      if (drawnBefore !== "round:" + ph.r.n) K.round(ph.r);
      showAnswer(ph.r, false);
      if (fresh) haptic("medium");
    },
    // Against the clock: the result of the player's own answer, with the others' progress under it, until they go on.
    treveal(ph, fresh) {
      const r = ph.r, g = (r.guesses || []).find((x) => x.m === P.me) || null;
      mine = null;
      K.top(placeTitle(r.n), sub(), back);
      if (drawnBefore !== "round:" + r.n) K.round(r);
      K.ribbon(null);
      const more = !!ph.next && K.msTo(r.ends_at) > 0;
      K.answer({
        place: r.place, me: g, note: noteOf(g), others: [], call: fmt(g && g.lat != null ? g.points : 0),
        top: '<div class="fr-live">' + K.table(liveRows(), false) + '</div>',
        side: '<div class="fr-live" data-faces="1">' + K.table(liveRows(), true) + '</div>',
        bot: more ? "" : '<p class="pl up">' + esc(T("party.wait_end")) + '</p>',
        go: more ? { act: "pNext", label: T("free.next") } : undefined,
      });
      K.clock(r.ends_at, P.settings.seconds, poll);
      if (fresh) haptic("medium");
    },
    // Against the clock, time is up for this player (or the places ran out): the others are still answering.
    tend() {
      K.stopClock(); K.ribbon(null);
      K.top(T("party.title"), sub(), back);
      K.page('<p class="fr-wait">' + esc(T("party.wait_end")) + '</p>' + '<p class="fr-l up">' + esc(T("party.standing")) + '</p>' + K.table(liveRows(), true));
    },
    final(ph, fresh) {
      K.top(T("party.final"), K.setLabel(P.settings, P.day), back);
      const me = P.totals.find((l) => l.m === P.me), others = P.totals.length - 1;
      const rank = me ? T("party.rank", { rank: fmt(me.rank), of: fmt(P.totals.length) }) : "";
      const unit = streak() ? "free.streak_len" : timed() ? "free.timed_len" : "";
      let html = '<div class="fr-big">';
      if (me) html += '<b>' + fmt(value(me)) + '</b><span class="up">' + esc(unit ? T(unit) : rank) + '</span>';
      html += '</div>';
      if (me && unit) html += '<p class="fr-best">' + esc(rank) + '</p>';
      if (me && me.beaten > 0) html += '<p class="fr-rec">' + esc(T("party.above", { n: fmt(Math.round(100 * me.beaten / others)) })) + '</p>';
      html += standing(0);
      if (P.best) {
        const r = P.rounds.find((x) => x.n === P.best.n);
        html += '<p class="fr-best">' + esc(T("party.best", { name: nameAt(P.best.m), km: kmT(P.best.km), where: r && r.place ? K.whereOf(r.place) : "" })) + '</p>';
      }
      html += places();
      html += '<div class="fr-go">' + (P.day ? '<button class="fr-pri" data-act="pArchive">' + esc(T("free.to_archive")) + '</button>'
        : '<button class="fr-pri" data-act="pRematch">' + esc(T(P.next ? "party.to_rematch" : "party.rematch")) + '</button>') +
        '<button class="fr-sec" data-act="pShare">' + esc(T("party.share")) + '</button>' +
        '<button class="fr-link" data-act="pHome">' + esc(T("free.solo")) + '</button></div>';
      K.page(html, "fr-final", !fresh);
      if (fresh) haptic("success");
    },
    // A place of the finished game, with everyone's pin: opened from the result and back to it.
    review(ph) {
      K.top(placeTitle(ph.r.n), K.setLabel(P.settings, P.day), () => { review = 0; render(); });
      K.round(ph.r);
      showAnswer(ph.r, true);
    },
    over() {
      K.top(T("party.title"), "");
      K.page('<p class="fr-wait">' + esc(T("party.over")) + '</p><div class="fr-go"><button class="fr-pri" data-act="pHome">' + esc(T("free.solo")) + '</button></div>');
    },
  };
  // The parts that move between two drawings: the countdown, who has answered, the seconds to the next place.
  const TICK = {
    count(ph) { const c = $("frCount"); if (c) c.textContent = fmt(Math.max(1, Math.ceil(K.msTo(ph.r.starts_at) / 1000))); },
    round(ph) {
      ribbonNow(ph.r);
      if (drawnV === P.v) return;
      drawnV = P.v;
      if (outOf(P.me, ph.r)) K.watch(waiting(ph.r));
      else if (answered(ph.r)) { sentN = ph.r.n; K.sent(waiting(ph.r)); }
    },
    reveal(ph) {
      const ms = ph.next ? K.msTo(ph.next.starts_at) : K.msTo(ph.r.closed_at) + P.reveal * 1000;
      const t = T(ph.next ? "party.next_in" : "party.final_in", { n: fmt(Math.max(0, Math.ceil(ms / 1000))) });
      document.querySelectorAll(".fr-next").forEach((e) => { if (e.textContent !== t) e.textContent = t; });
    },
    treveal() {
      if (drawnV === P.v) return;
      drawnV = P.v;
      document.querySelectorAll(".fr-live").forEach((e) => { e.innerHTML = K.table(liveRows(), !!e.dataset.faces); });
    },
  };
  // The answer to a place on the map and in the tables: everyone's pin and, for each, the distance, the points and
  // whether the country was right. With review, from the result of a finished game: the way on is back to it.
  function showAnswer(r, review) {
    const guesses = r.guesses || [];
    const me = guesses.find((g) => g.m === P.me) || null;
    // Nobody's answer leaves the list out.
    const others = guesses.filter((g) => g.m !== P.me).map((g) => ({ key: "m" + g.m, lat: g.lat, lon: g.lon, km: g.km, who: member(g.m) }));
    const rows = rowsOf(r);
    K.answer({
      place: r.place, me, note: me || !outOf(P.me, r) ? noteOf(me) : "", others, call: fmt(me && me.lat != null ? me.points : 0),
      top: K.table(rows, false),
      side: K.table(rows, true) + (review ? "" : standing(0) + '<p class="fr-next up"></p>'),
      bot: review ? "" : '<p class="pl fr-next up"></p>',
      go: review ? { act: "pFinal", label: T("party.to_final") } : undefined,
    });
  }
  // What happened to the player's own answer, in the mode's words.
  function noteOf(g) {
    if (!g || g.lat == null) return timed() ? T("free.passed") : T("free.missed");
    if (streak()) return T(g.country ? "free.country_ok" : "free.country_miss");
    return timed() ? T(g.country ? "free.timed_ok" : "free.timed_miss") : "";
  }
  // The answers to a place, best first, as the day's table: place, name, distance, country, points.
  function rowsOf(r) {
    const rows = (r.guesses || []).slice().sort((a, b) => b.points - a.points || (a.km ?? 1e9) - (b.km ?? 1e9) || a.time_ms - b.time_ms);
    return rows.map((g, i) => ({ rank: i + 1, who: member(g.m), me: g.m === P.me, km: g.lat != null ? g.km : null, points: g.points, ok: g.lat != null ? g.country : false }));
  }
  // The standing after the closed places: the first n lines, or all of them. A streak reads by its length and the
  // clock's game by its countries, with the points beside them.
  function standing(n) {
    const lines = n ? P.totals.slice(0, n) : P.totals;
    if (!lines.length) return "";
    return '<p class="fr-l up">' + esc(T("party.standing")) + '</p>' + K.table(lines.map((l) => {
      const m = member(l.m);
      return { rank: l.rank, who: m, me: l.m === P.me, points: value(l), sub: streak() || timed() ? T("free.points_now", { n: fmt(l.points) }) : null,
        mark: m.left ? T("party.left") : m.away ? T("party.away") : m.out ? T("party.out") : "" };
    }), true);
  }
  // Against the clock, who is where: the places answered and the countries found, with the points.
  function liveRows() {
    const list = present().slice().sort((a, b) => (b.countries || 0) - (a.countries || 0) || (b.points || 0) - (a.points || 0));
    return list.map((m, i) => ({ rank: i + 1, who: m, me: m.m === P.me, points: m.points || 0, sub: fmt(m.done || 0) + " · " + fmt(m.countries || 0) + " ✓" }));
  }
  // The places of a finished game, each one a way to its answer: where it was and how the player did.
  function places() {
    const rs = P.rounds.filter((r) => r.place);
    if (!rs.length) return "";
    return '<p class="fr-l up">' + esc(T("party.places_h")) + '</p><div class="fr-places">' + rs.map((r) => {
      const g = (r.guesses || []).find((x) => x.m === P.me);
      const mark = outOf(P.me, r) ? "" : streak() || timed() ? (g && g.lat != null && g.country ? "✓" : "✗") : fmt(g && g.lat != null ? g.points : 0);
      return '<button class="fr-pl" data-act="pReview" data-n="' + r.n + '"><span class="n">' + fmt(r.n) + '</span><span class="w">' + esc(K.whereOf(r.place)) + '</span><b>' + mark + '</b><em>' + fwd() + '</em></button>';
    }).join("") + '</div>';
  }
  // Who is in the place: white is an answer, a dashed square a player the party no longer waits for or who is out of a
  // streak; against the clock each player's places answered, countries found and points.
  function ribbonNow(r) {
    const list = present().filter((m) => m.from <= r.n);
    K.ribbon(list.map((m) => {
      if (timed()) {
        const d = m.done || 0, c = m.countries || 0, p = m.points || 0;
        return { ...m, stat: fmt(d) + " · " + fmt(c) + "✓ · " + fmt(p), title: T("party.stat_title", { name: nameOf(m), done: fmt(d), countries: fmt(c), points: fmt(p) }) };
      }
      return { ...m, done: (r.answered || []).includes(m.m), away: m.away || outOf(m.m, r) };
    }));
  }
  async function answer(r, auto, skip) {
    if (sending || sentN === r.n || (!skip && !mine)) return;
    sending = true; K.sending(true);
    try {
      await send("/guess", skip ? { n: r.n, skip: true } : { n: r.n, lat: mine.lat, lon: mine.lon, auto });
      sentN = r.n; haptic(skip ? "light" : "success");
    } catch (e) {
      if (e.message === "already_answered") sentN = r.n;
      else {
        toast(K.ferr(e));
        // Out of a streak since the screen was drawn, or a place the server does not have there: it shows now.
        if (e.message === "out" || e.message === "out_of_order") poll();
      }
    }
    sending = false;
    // The place from the answer just read: r is the one from before it and does not list this answer yet.
    const ph = P && phase();
    if (!ph || ph.k !== "round" || ph.r.n !== r.n) return;
    if (sentN === r.n) K.sent(waiting(ph.r)); else K.sending(false);
  }

  /* ---------- The buttons ---------- */
  Object.assign(K.ACTS, {
    pSend() { const ph = phase(); if (ph.k === "round") answer(ph.r, false); },
    pPass() { const ph = phase(); if (ph.k === "round" && timed()) answer(ph.r, false, true); },
    // Against the clock: on to the next place.
    pNext() { const ph = phase(); if (ph.k === "treveal" && ph.next) { seen = ph.r.n; haptic("light"); render(); } },
    pReview(b) { if (P && P.status === "done") { review = +b.dataset.n; render(); } },
    pFinal() { review = 0; render(); },
    async pStart(b) {
      b.disabled = true;
      try { await send("/start", { version: P.version }); haptic("medium"); }
      catch (e) {
        b.disabled = false;
        // The host changed the settings a moment ago in another window, or someone left: the screen shows it now.
        toast(e.message === "settings_changed" ? T("party.changed") : K.ferr(e));
        poll();
      }
    },
    async pInvite(b) {
      b.disabled = true;
      let d = null;
      try { d = await K.req("/api/party/" + CODE + "/invite", {}); } catch (e) { toast(K.ferr(e)); }
      b.disabled = false;
      if (!d) return;
      // shareMessage throws while its sheet is still open; the link is the fallback.
      if (d.id && tgAt("8.0") && tg.shareMessage) { try { tg.shareMessage(d.id); return; } catch {} }
      K.share(d.link, T("party.invite_text", { set: K.setLabel(P.settings, P.day) }));
    },
    async pLeave(b) {
      b.disabled = true;
      try { await K.req("/api/party/" + CODE + "/leave", {}); } catch (e) { b.disabled = false; toast(K.ferr(e)); return; }
      stop();
      Router.go("/play", { replace: true });
    },
    async pRematch(b) {
      b.disabled = true;
      try {
        const code = P.next || (await K.req("/api/party/" + CODE + "/rematch", {})).code;
        Router.go("/party/" + code, { replace: true });
      } catch (e) { b.disabled = false; toast(K.ferr(e)); }
    },
    pShare() {
      const me = P.totals.find((l) => l.m === P.me);
      if (!me) return;
      const key = streak() ? "party.share_streak" : timed() ? "party.share_timed" : "party.share_text";
      const text = "maply · " + K.setLabel(P.settings, P.day) + "\n" + T(key, { rank: fmt(me.rank), of: fmt(P.totals.length), points: fmt(me.points), n: fmt(value(me)) });
      K.share(K.bot() && tgAt("6.1") ? "https://t.me/" + K.bot() + "/play" : location.origin + "/play", text);
    },
    pHome() { Router.go("/play", { replace: true }); },
    pArchive() { Router.go("/play?mode=archive", { replace: true }); },
  });
  // A change of settings in the lobby goes to the server; the host's screen shows what the server kept.
  K.lobby = (b) => {
    if (!P || drawn !== "lobby") return false;
    if (!isHost()) return true;
    const set = K.choose(P.settings, b.dataset.k, b.dataset.v);
    haptic("select");
    send("/settings", set).catch((e) => toast(K.ferr(e)));
    return true;
  };
  K.stopped = stop;
  // From the lobby's first countdown to the result: what the free game asks before the player leaves.
  K.running = () => !!P && P.status === "playing";

  async function join(code) {
    if (CODE === code && P) { render(); return; }
    stop();
    K.top(T("party.title"), "");
    K.wait();
    let v;
    try { v = await K.req("/api/party/" + code + "/join", {}); } catch (e) { gone(e); return; }
    run(v);
  }
  K.party = {
    // From the solo settings: a lobby on them, with this player as its host. fail is where a refusal goes back to.
    async create(set, fail) {
      K.wait();
      let v;
      try { v = await K.req("/api/party", set); } catch (e) { toast(K.ferr(e)); (fail || K.home)(); return; }
      run(v);
      Router.go("/party/" + v.code, { replace: true });
    },
    open: join,
  };
})();
