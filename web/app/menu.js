/* ---------- The menu and the leaderboards ---------- */
// "/" is the menu: the day first and largest, the free game's modes beside it, then the days, the leaderboards and
// the profile. The day itself is /today. A post's button opens the day: a DM's carries /today, a group's its signed
// payload (app.js moves such a start to /today). The bot's menu button and /start open the menu.
//
// The menu reads the day from app.js as it stands (TODAY, state, mine, the table) and fetches nothing of its own: it
// draws a loading card until the day is in, and draws the card again as the day moves on. /top is the free game's
// leaderboards, drawn by leaders.js (Leaders.show(el)); without that file there is no link to it and the address
// shows the menu.
//
// Both are a page in desktop.css's shell (.dk-page): one element under the header, over the game and under the sheet,
// so a mini profile opened from a table comes over the page and goes back to it. While it is on, body has is-page and
// the game behind is inert (app.js's behindSheet decides what is in reach). Leaving either closes a sheet that has
// no address of its own. Back from the leaderboards is Telegram's, Escape, the header and the browser's.
(() => {
  // The free game reads the mode from the address and opens its settings on it; Blitz is Classic on 30 seconds.
  const MODES = [["classic", "/play?mode=classic"], ["blitz", "/play?mode=blitz"], ["streak", "/play?mode=streak"],
    ["timed", "/play?mode=clock"], ["archive", "/play?mode=archive"], ["friends", "/play?with=friends"]];
  const leaders = () => !!(window.Leaders && typeof Leaders.show === "function");
  let page = null, kind = "", clock = 0, drawn = "", keyed = false;

  function show(k, html, from, label) {
    if (!page) {
      page = document.createElement("section");
      page.className = "dk-page pg"; page.id = "pg"; page.tabIndex = -1;
      document.body.insertBefore(page, $("sheet"));
      // A link taken with the keyboard (a click with no pointer behind it) hands the focus on to the next page.
      page.addEventListener("click", (e) => { keyed = e.detail === 0 && !!(e.target.closest && e.target.closest("a")); }, true);
    }
    kind = k; page.dataset.kind = k; page.innerHTML = html;
    page.setAttribute("aria-label", label);
    // The page goes over the game: the game's controls leave the tab order and the screen reader's reach. The
    // header stays, and so does the sheet, which comes over the page.
    page.classList.add("on"); document.body.classList.add("is-page");
    behindSheet(); syncBack();
    page.scrollTop = 0;
    // Coming from another screen, the focus comes along; the first page of a visit leaves it where the browser put it.
    if (from) page.focus({ preventScroll: true });
  }
  function hide() {
    if (!page || !page.classList.contains("on")) return;
    clearInterval(clock); clock = 0; drawn = ""; kind = "";
    page.classList.remove("on"); document.body.classList.remove("is-page");
    page.innerHTML = "";
    behindSheet(); syncBack();
  }
  // A mini profile, the world's table or an account's sheet opened over the page goes with it.
  function leave() { if ($("sheet").classList.contains("on")) closeDays(); hide(); }

  /* The day's card */
  // Where the day stands for this player: still to play, answered and waiting, answers closed without one, or the
  // result, the player's own or the day's when they did not answer.
  function stand() {
    if (!TODAY) return { k: "load" };
    if (state === "reveal") {
      const w = boardRows.find((r) => r.me);
      return { k: mine ? "mine" : "seen", rank: w && typeof w.rank === "number" ? w.rank : 0 };
    }
    if (TODAY.answer) return { k: "sent" };
    if (state === "wait" || !secondsTo(TODAY.deadline)) return { k: "closed" };
    return { k: "play" };
  }
  // The clock: to the end of answering, to the result, or in the live result to the names. None when nothing is due.
  function due(s) {
    if (s.k === "play") return [hm(secondsTo(TODAY.deadline)), T("until_truth")];
    if (s.k === "sent" || s.k === "closed") return [hm(secondsTo(TODAY.reveal)), T("until_answer")];
    if (s.k === "mine" && PHASE === "live") return [hm(secondsTo(TODAY.reveal)), T("until_names")];
    return null;
  }
  // "4 210 for 312 km" with the points large, in whatever order the language puts the two.
  function score() {
    const [a, b] = T("km_points", { points: "\u0001", km: kmT(mine.d) }).split("\u0001");
    return '<p class="mn-score">' + esc(a || "") + '<b class="nums">' + esc(fmt(mine.pts)) + '</b>' + esc(b || "") + '</p>';
  }
  function dayHTML() {
    const s = stand();
    let shot = "", body;
    if (s.k === "load") body = '<b class="mn-n">maply</b><p class="mn-st">' + esc(lines("loading")[0]) + '</p>';
    else {
      // The day's own picture, the one being played: a panorama by its middle, a photo as it is.
      if (TODAY.image) shot = '<span class="mn-shot' + (TODAY.kind === "panorama" ? " pano" : "") + '" style="background-image:url(&quot;' + esc(TODAY.image) + '&quot;)"></span>';
      // After the reveal the picture carries its author and licence, as on the day (app.js's credits). Text only:
      // the card is a link already.
      const cr = s.k === "mine" || s.k === "seen" ? $("credits").querySelector("span:not(:last-child)") : null;
      if (shot && cr) shot += '<span class="mn-cr">' + esc(cr.textContent.replace(/\s*·\s*$/, "")) + '</span>';
      body = '<span class="up mn-l">' + esc(T("desk.today")) + '</span><b class="mn-n">maply #' + esc(fmt(TODAY.day)) + '</b>';
      if (s.k === "mine") body += score() + (s.rank ? '<p class="mn-st">' + esc(T(WORLD_OPEN ? "world_final" : "world_now", { rank: fmt(s.rank), n: fmt(boardTotal) })) + '</p>' : "");
      else if (s.k === "sent" || s.k === "closed") body += '<p class="mn-st">' + esc(T(s.k === "sent" ? "sent" : "closed") + " · " + T("result_at", { time: atZone() })) + '</p>';
      else if (s.k === "seen") body += '<p class="mn-st">' + esc(T("day_result")) + '</p>';
    }
    const c = s.k === "load" ? null : due(s);
    return '<a class="mn-day mn-' + s.k + '" href="/today" data-route>' + shot + '<span class="mn-in">' + body +
      '<span class="mn-foot"><span class="mn-clock nums" id="mnClock">' + (c ? '<b>' + esc(c[0]) + '</b> <span class="up">' + esc(c[1]) + '</span>' : "") + '</span>' +
      '<span class="mn-go">' + esc(s.k === "play" ? T("days.play") : T("see")) + ' ' + fwd() + '</span></span></span></a>';
  }
  // The card is drawn again when the day moves on (the day's state and its reveal's time in, the answer, the result,
  // the names), the clock every second in between.
  const sign = () => { const s = stand(); return [LANG, atZone(), s.k, s.rank, TODAY && TODAY.day, mine && mine.pts, PHASE, WORLD_OPEN, boardTotal].join("|"); };
  function tickMenu() {
    if (kind !== "menu") return;
    const now = sign();
    if (now !== drawn) {
      drawn = now;
      if (TODAY) hideLoader();
      const at = document.activeElement, here = !!(at && at.closest && at.closest(".mn-day"));
      $("mnDay").innerHTML = dayHTML();
      if (here) $("mnDay").firstElementChild.focus({ preventScroll: true });
      return;
    }
    const s = stand(), c = s.k === "load" ? null : due(s), b = $("mnClock") && $("mnClock").querySelector("b");
    if (c && b && b.textContent !== c[0]) b.textContent = c[0];
  }

  function menuHTML() {
    const free = window.Free ? '<section class="mn-free" aria-labelledby="mnFree"><div class="mn-h"><b id="mnFree">' + esc(T("free.title")) + '</b>' +
      '<span class="up">' + esc(T("desk.unranked")) + '</span></div><div class="mn-modes">' +
      MODES.map(([m, href]) => '<a class="mn-mode" href="' + href + '" data-route><b>' + esc(T("free." + m)) + '</b><em>' + fwd() + '</em></a>').join("") +
      '</div></section>' : "";
    const go = (href, title, hint) => '<a class="mn-more" href="' + href + '" data-route><b>' + esc(title) + '</b><span>' + esc(hint) + '</span><em>' + fwd() + '</em></a>';
    return '<div class="dk-wrap mn"><div class="mn-dayslot" id="mnDay">' + dayHTML() + '</div>' + free +
      '<nav class="mn-pages" aria-label="' + esc(T("desk.nav")) + '">' + go("/days", T("days"), T("desk.days_hint")) +
      (leaders() ? go("/top", T("desk.top"), T("desk.top_hint")) : "") + go("/me", T("prof.title"), T("desk.me_hint")) + '</nav></div>';
  }

  Router.screen("menu", /^\/(?:index\.html)?$/, {
    open(m, from) {
      show("menu", menuHTML(), from, "maply");
      drawn = sign();
      clearInterval(clock); clock = setInterval(tickMenu, 1000);
      // The menu needs no picture: the loading screen goes once the day's state is in. The day brings it back when
      // it opens while its picture is still on the way.
      if (TODAY) hideLoader();
    },
    close: leave,
  });

  /* The leaderboards */
  Router.screen("top", /^\/top$/, {
    open(m, from) {
      // Not from inside the router's own change of screen: the menu comes next, after this one has been reported.
      if (!leaders()) { setTimeout(() => Router.go("/", { replace: true })); return; }
      // The tables bring their own title and their own width, the shell's.
      show("top", '<div id="pgBody"></div>', from, T("desk.top"));
      Leaders.show($("pgBody"));
    },
    // A mini profile opened from a table goes with the page.
    close: leave,
    // leaders.js goes to this page when the router has it, and draws the tables in a sheet otherwise.
    nav: { href: "/top", t: "desk.top", at: 3 },
  });
  // Escape is the page's Back, as it is the sheet's, unless a sheet is over the page.
  addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || kind !== "top" || e.defaultPrevented || document.body.classList.contains("is-sheet")) return;
    e.preventDefault(); Router.back("/");
  });

  // The day opened before its picture came: the loading screen says so until it does, as on a first start.
  // Opened from the keyboard, the day takes the focus the card had: the result beside the map on a computer, the
  // answer's buttons after the reveal, the globe that opens the map before it. A tap leaves it, or a phone would
  // show the ring.
  let pictured = false;
  pano.on("load", () => { pictured = true; });
  const shown = (e) => e && e.getClientRects().length > 0 && !e.closest("[inert]");
  Router.on((name) => {
    if (name === "today" && !pictured && TODAY && !TODAY.answer) $("loader").classList.remove("gone");
    if (name === "today" && keyed) {
      const to = [state === "reveal" && Desk.on ? $("side") : null, state === "reveal" ? $("go") : $("globeHit"), $("go")].find(shown);
      if (to) to.focus({ preventScroll: true });
    }
    keyed = false;
  });
})();
