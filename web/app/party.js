/* ---------- The free game with friends ---------- */
// Everyone asks the server every two seconds what has changed; an answer that changes nothing is the version and the
// clock. Which phase the party is in (the countdown, a place, its reveal, the end) is worked out here from the server's
// times, so a phone that was away a moment draws the same screen as the others. Nobody's pin, and nothing of the place,
// comes before the place has closed.
(() => {
  "use strict";
  const K = window.FreeKit;
  const EVERY = 2000;
  let P = null, CODE = "", pollT = 0, tickT = 0, drawn = "", drawnV = -1, mine = null, sending = false, sentN = 0;
  // Which screen was up before this one: a reveal straight after its own place keeps the picture.
  let drawnBefore = "";

  /* ---------- Talking to the server ---------- */
  function stop() { clearTimeout(pollT); clearInterval(tickT); pollT = tickT = 0; CODE = ""; P = null; drawn = ""; drawnV = -1; sentN = 0; mine = null; sending = false; }
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

  /* ---------- Who is who ---------- */
  const member = (m) => P.members.find((x) => x.m === m) || { m, name: "?" };
  const nameOf = (m) => member(m).name;
  const meM = () => member(P.me);
  const present = () => P.members.filter((m) => !m.left);
  const isHost = () => P.host === P.me;

  /* ---------- The phase, from the clock ---------- */
  function phase() {
    if (!P) return { k: "wait" };
    if (P.status === "lobby") return { k: "lobby" };
    if (P.status === "done") return { k: "final" };
    if (P.status !== "playing") return { k: "over" };
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
  function render() {
    if (!P) return;
    const ph = phase(), id = ph.k + (ph.r ? ":" + ph.r.n : "");
    const fresh = id !== drawn;
    if (fresh || (["lobby", "final", "later"].includes(ph.k) && drawnV !== P.v)) {
      if (fresh) drawnBefore = drawn;
      drawn = id; drawnV = P.v;
      DRAW[ph.k](ph, fresh);
    }
    TICK[ph.k] && TICK[ph.k](ph);
  }

  /* ---------- The screens ---------- */
  // The lobby is a page of the top, its way up the menu. Back from a place or the end is the free game's settings,
  // where the party stays open under "Back to friends". The free game keeps one entry of the history, so the menu is
  // what comes before either.
  const back = () => Router.go("/play", { replace: true });
  const placeTitle = (n) => T("free.place_n", { n: fmt(n), total: fmt(P.settings.rounds) });
  const DRAW = {
    wait() { K.top(T("party.title"), ""); K.wait(); },
    // The players first on a phone; on a computer the settings on the left and the players beside them.
    lobby(ph, fresh) {
      K.top(T("party.title"), T("party.code", { code: P.code }));
      const people = present();
      let html = '<div class="fr-r"><p class="fr-l up">' + esc(T("free.players", { n: fmt(people.length) })) + '</p><ul class="fr-mem">' + people.map((m) => '<li' + (m.m === P.me ? ' class="me"' : '') + '><i class="av">' + esc(initial(m.name)) + '</i><b lang="' + nameLang() + '">' + esc(m.name) + '</b>' +
        (m.m === P.host ? '<span class="up">' + esc(T("party.host")) + '</span>' : m.m === P.me ? '<span class="up">' + esc(T("you")) + '</span>' : '') + '</li>').join("") + '</ul></div>';
      html += '<div class="fr-a">' + K.settings(P.settings, true, !isHost());
      html += '<p class="fr-best">' + esc(T("party.length", { n: fmt(Math.max(1, Math.round(P.length / 60))) })) + '</p>';
      html += '<div class="fr-go">';
      if (isHost()) {
        if (people.length < 2) html += '<p class="fr-hint">' + esc(T("party.need_two")) + '</p>';
        html += '<button class="fr-pri" data-act="pStart"' + (people.length < 2 ? " disabled" : "") + '>' + esc(T("party.start")) + '</button>';
      } else html += '<p class="fr-hint">' + esc(T("party.wait_host", { name: nameOf(P.host) })) + '</p>';
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
    round(ph) {
      const r = ph.r;
      mine = null; sending = false;
      K.top(placeTitle(r.n), T("party.title"), back);
      K.round(r);
      if (r.repeat) toast(T("free.repeat"));
      K.goTo("pSend");
      if (answered(r)) { sentN = r.n; K.sent(waiting(r)); }
      else K.pinned((p) => { mine = p; });
      ribbonNow(r);
      K.clock(r.ends_at, P.settings.seconds, () => {
        // At zero a pin set but not sent goes by itself; without one the place counts as missed when it closes.
        if (mine && !answered(r)) answer(r, true);
      });
    },
    // A place closed: the day's result, with the answers to it and the standing; the next place starts by itself.
    reveal(ph, fresh) {
      const r = ph.r;
      K.stopClock(); K.ribbon(null);
      K.top(placeTitle(r.n), T("party.title"), back);
      // Opened in the middle of a reveal, the screen has not shown the place's picture yet.
      if (drawnBefore !== "round:" + r.n) K.round(r);
      // Nobody's answer leaves the list out.
      const guesses = r.guesses || [];
      const me = guesses.find((g) => g.m === P.me) || null;
      const others = guesses.filter((g) => g.m !== P.me).map((g) => ({ key: "m" + g.m, lat: g.lat, lon: g.lon, km: g.km, name: nameOf(g.m) }));
      const rows = rowsOf(r);
      K.answer({
        place: r.place, me, note: me ? "" : T("free.missed"), others, call: fmt(me && me.lat != null ? me.points : 0),
        top: K.table(rows, false),
        side: K.table(rows, true) + standing(0) + '<p class="fr-next up"></p>',
        bot: '<p class="pl fr-next up"></p>',
      });
      if (fresh) haptic("medium");
    },
    final(ph, fresh) {
      K.top(T("party.final"), K.setLabel(P.settings), back);
      const me = P.totals.find((l) => l.m === P.me), others = P.totals.length - 1;
      let html = '<div class="fr-big">';
      if (me) html += '<b>' + fmt(me.points) + '</b><span class="up">' + esc(T("party.rank", { rank: fmt(me.rank), of: fmt(P.totals.length) })) + '</span>';
      html += '</div>';
      if (me && me.beaten > 0) html += '<p class="fr-rec">' + esc(T("party.above", { n: fmt(Math.round(100 * me.beaten / others)) })) + '</p>';
      html += standing(0);
      if (P.best) {
        const r = P.rounds.find((x) => x.n === P.best.n);
        html += '<p class="fr-best">' + esc(T("party.best", { name: nameOf(P.best.m), km: kmT(P.best.km), where: r && r.place ? K.whereOf(r.place) : "" })) + '</p>';
      }
      html += '<div class="fr-go"><button class="fr-pri" data-act="pRematch">' + esc(T(P.next ? "party.to_rematch" : "party.rematch")) + '</button>' +
        '<button class="fr-sec" data-act="pShare">' + esc(T("party.share")) + '</button>' +
        '<button class="fr-link" data-act="pHome">' + esc(T("free.solo")) + '</button></div>';
      K.page(html, "fr-final", !fresh);
      if (fresh) haptic("success");
    },
    over() {
      K.top(T("party.title"), "");
      K.page('<p class="fr-wait">' + esc(T("party.over")) + '</p><div class="fr-go"><button class="fr-pri" data-act="pHome">' + esc(T("free.solo")) + '</button></div>');
    },
  };
  // The parts that move between two drawings: the countdown, who has answered, the seconds to the next place.
  const TICK = {
    count(ph) { const c = $("frCount"); if (c) c.textContent = fmt(Math.max(1, Math.ceil(K.msTo(ph.r.starts_at) / 1000))); },
    round(ph) { ribbonNow(ph.r); if (drawnV !== P.v) { drawnV = P.v; if (answered(ph.r)) { sentN = ph.r.n; K.sent(waiting(ph.r)); } } },
    reveal(ph) {
      const ms = ph.next ? K.msTo(ph.next.starts_at) : K.msTo(ph.r.closed_at) + P.reveal * 1000;
      const t = T(ph.next ? "party.next_in" : "party.final_in", { n: fmt(Math.max(0, Math.ceil(ms / 1000))) });
      document.querySelectorAll(".fr-next").forEach((e) => { if (e.textContent !== t) e.textContent = t; });
    },
  };
  // The answers to a place, best first, as the day's table: place, name, distance, points.
  function rowsOf(r) {
    const rows = (r.guesses || []).slice().sort((a, b) => b.points - a.points || (a.km ?? 1e9) - (b.km ?? 1e9) || a.time_ms - b.time_ms);
    return rows.map((g, i) => ({ rank: i + 1, name: nameOf(g.m), me: g.m === P.me, km: g.lat != null ? g.km : null, points: g.points }));
  }
  // The standing after the closed places: the first n lines, or all of them.
  function standing(n) {
    const lines = n ? P.totals.slice(0, n) : P.totals;
    if (!lines.length) return "";
    return '<p class="fr-l up">' + esc(T("party.standing")) + '</p>' + K.table(lines.map((l) => {
      const m = member(l.m);
      return { rank: l.rank, name: m.name, me: l.m === P.me, points: l.points, mark: m.left ? T("party.left") : m.away ? T("party.away") : "" };
    }), true);
  }
  const answered = (r) => (r.answered || []).includes(P.me);
  // Who the party still waits for in this place, besides you.
  const waiting = (r) => present().filter((m) => m.m !== P.me && m.from <= r.n && !m.away && !(r.answered || []).includes(m.m)).length;
  function ribbonNow(r) {
    K.ribbon(present().filter((m) => m.from <= r.n).map((m) => ({ name: m.name, me: m.m === P.me, done: (r.answered || []).includes(m.m), away: m.away })));
  }
  async function answer(r, auto) {
    if (sending || !mine || sentN === r.n) return;
    sending = true; K.sending(true);
    try {
      await send("/guess", { n: r.n, lat: mine.lat, lon: mine.lon, auto });
      sentN = r.n; haptic("success");
    } catch (e) {
      if (e.message === "already_answered") sentN = r.n;
      else toast(K.ferr(e));
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
      K.share(d.link, T("party.invite_text", { set: K.setLabel(P.settings) }));
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
      const text = "maply · " + K.setLabel(P.settings) + "\n" + T("party.share_text", { rank: fmt(me.rank), of: fmt(P.totals.length), points: fmt(me.points) });
      K.share(K.bot() && tgAt("6.1") ? "https://t.me/" + K.bot() + "/play" : location.origin + "/play", text);
    },
    pHome() { Router.go("/play", { replace: true }); },
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
    // From the solo settings: a lobby on them, with this player as its host.
    async create(set) {
      K.wait();
      let v;
      try { v = await K.req("/api/party", set); } catch (e) { toast(K.ferr(e)); K.home(); return; }
      run(v);
      Router.go("/party/" + v.code, { replace: true });
    },
    open: join,
  };
})();
