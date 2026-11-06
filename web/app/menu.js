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

  /* The cards' pictures */
  // Every card but the day's has one picture: its sign as a halftone, 18 dots by 18 like the logo's globe, in the
  // field's light green and lit from the upper left as the loading screen's globe is. Classic's sign is that globe.
  // They stand still. Now and then one light runs over all of them at once, and the card under the pointer or the
  // focus lights its own up. Without motion only that remains.
  const GRID = 18, SUB = 6;
  // The signs, drawn white on a 100 × 100 square; what they cover becomes the dots.
  const PIN = new Path2D("M320 501L214 395A150 150 0 1 1 426 395ZM320 229A60 60 0 1 0 320 349A60 60 0 1 0 320 229Z");
  const pinAt = (c, x, y, s, gap) => {
    c.save(); c.translate(x, y); c.scale(s, s); c.translate(-320, -501);
    if (gap) { c.globalCompositeOperation = "destination-out"; c.lineWidth = gap / s; c.lineJoin = "round"; c.stroke(PIN); c.globalCompositeOperation = "source-over"; }
    c.fill(PIN, "evenodd"); c.restore();
  };
  const cut = (c, f) => { c.globalCompositeOperation = "destination-out"; f(); c.globalCompositeOperation = "source-over"; };
  const SIGN = {
    blitz(c) {
      c.beginPath(); c.moveTo(62, 2); c.lineTo(16, 58); c.lineTo(45, 58); c.lineTo(36, 98); c.lineTo(84, 38); c.lineTo(56, 38); c.closePath(); c.fill();
    },
    streak(c) {
      c.fill(new Path2D("M52 4C58 26 84 38 84 64A34 34 0 0 1 16 64C16 47 27 36 35 28C36 41 41 47 47 50C43 34 45 18 52 4Z" +
        "M50 58C57 66 63 73 63 81A13 13 0 0 1 37 81C37 73 43 66 50 58Z"), "evenodd");
    },
    timed(c) {
      c.fillRect(18, 4, 64, 9); c.fillRect(18, 87, 64, 9);
      c.lineWidth = 7; c.beginPath();
      c.moveTo(28, 13); c.bezierCurveTo(28, 36, 46, 42, 46, 50); c.bezierCurveTo(46, 58, 28, 64, 28, 87);
      c.moveTo(72, 13); c.bezierCurveTo(72, 36, 54, 42, 54, 50); c.bezierCurveTo(54, 58, 72, 64, 72, 87); c.stroke();
      c.beginPath(); c.moveTo(37, 30); c.lineTo(63, 30); c.bezierCurveTo(59, 38, 53, 43, 50, 47); c.bezierCurveTo(47, 43, 41, 38, 37, 30); c.fill();
      c.beginPath(); c.moveTo(33, 86); c.bezierCurveTo(37, 73, 44, 67, 50, 67); c.bezierCurveTo(56, 67, 63, 73, 67, 86); c.closePath(); c.fill();
    },
    archive(c) {
      c.fillRect(6, 12, 88, 20); c.fillRect(13, 38, 74, 52);
      cut(c, () => c.fillRect(36, 49, 28, 9));
    },
    friends(c) { pinAt(c, 73, 76, .17, 0); pinAt(c, 28, 92, .17, 10); },
    days(c) {
      c.fillRect(10, 20, 80, 74);
      cut(c, () => {
        for (let i = 0; i < 12; i++) if (i !== 6) c.fillRect(17 + (i % 4) * 18, 44 + Math.floor(i / 4) * 17, 12, 11);
        c.fillRect(24, 10, 14, 18); c.fillRect(62, 10, 14, 18);
      });
      c.fillRect(28, 6, 6, 20); c.fillRect(66, 6, 6, 20);
    },
    top(c) {
      c.fill(new Path2D("M24 8H76V30C76 48 64 58 50 58C36 58 24 48 24 30Z"));
      c.lineWidth = 7; c.beginPath();
      c.moveTo(24, 16); c.lineTo(12, 16); c.lineTo(12, 26); c.bezierCurveTo(12, 36, 18, 42, 26, 42);
      c.moveTo(76, 16); c.lineTo(88, 16); c.lineTo(88, 26); c.bezierCurveTo(88, 36, 82, 42, 74, 42); c.stroke();
      c.fillRect(44, 56, 12, 18); c.fillRect(30, 74, 40, 9); c.fillRect(22, 86, 56, 10);
      cut(c, () => {
        c.beginPath();
        for (let k = 0; k < 10; k++) {
          const a = (-90 + k * 36) * Math.PI / 180, r = k % 2 ? 5 : 12;
          c.lineTo(50 + r * Math.cos(a), 31 + r * Math.sin(a));
        }
        c.closePath(); c.fill();
      });
    },
    me(c) {
      c.fill(new Path2D("M12 98C12 74 28 62 50 62C72 62 88 74 88 98Z"));
      cut(c, () => { c.beginPath(); c.arc(50, 33, 25, 0, 2 * Math.PI); c.fill(); });
      c.beginPath(); c.arc(50, 33, 19, 0, 2 * Math.PI); c.fill();
    },
  };
  // The light falls from the upper left: a dot there is whole, one at the lower right about two thirds.
  const lit = (u, v) => 1 - .34 * (u + v) / 2;
  // How large each of the GRID × GRID dots is, from 0 to 1: a dot where the sign covers most of its square, sized by
  // the light. No dots for the edges' slivers, so every sign has a clean outline.
  function tones(k) {
    const t = new Float32Array(GRID * GRID);
    if (k === "classic") return globeTones(t);
    const n = GRID * SUB, cv = document.createElement("canvas");
    cv.width = cv.height = n;
    const c = cv.getContext("2d");
    c.scale(n / 100, n / 100); c.fillStyle = c.strokeStyle = "#fff";
    SIGN[k](c);
    const a = c.getImageData(0, 0, n, n).data;
    for (let j = 0; j < GRID; j++) for (let i = 0; i < GRID; i++) {
      let sum = 0;
      for (let y = 0; y < SUB; y++) for (let x = 0; x < SUB; x++) sum += a[((j * SUB + y) * n + i * SUB + x) * 4 + 3];
      if (sum / (SUB * SUB * 255) >= .5) t[j * GRID + i] = lit((i + .5) / GRID, (j + .5) / GRID);
    }
    return t;
  }
  // Classic: the globe of land.js turned to Europe and Africa, the north a little towards the viewer. Land is a dot
  // sized by the light the sphere gets there, sea a small one.
  function globeTones(t) {
    if (typeof LAND === "undefined") return t;
    const raw = atob(LAND.bits), start = [];
    LAND.rows.reduce((n, c) => (start.push(n), n + c), 0);
    const land = (lat, lon) => {
      const i = Math.max(0, Math.min(LAND.rows.length - 1, Math.floor((lat + 90) / LAND.step))), c = LAND.rows[i];
      const j = ((Math.floor((lon + 180) / 360 * c - (i % 2) * .5) % c) + c) % c, n = start[i] + j;
      return (raw.charCodeAt(n >> 3) >> (n & 7)) & 1;
    };
    const L = [-.55, -.6, .58], ln = Math.hypot(...L), tilt = 22 * Math.PI / 180, ct = Math.cos(tilt), st = Math.sin(tilt);
    const S = 3;
    for (let j = 0; j < GRID; j++) for (let i = 0; i < GRID; i++) {
      // The square's middle on the disc, x right and y up, z towards the viewer; the land it sees by nine points.
      const px = (i + .5) / GRID * 2 - 1, py = 1 - (j + .5) / GRID * 2, r2 = px * px + py * py;
      if (r2 > .9) continue;
      let ground = 0;
      for (let y = 0; y < S; y++) for (let x = 0; x < S; x++) {
        const qx = ((i + (x + .5) / S) / GRID) * 2 - 1, qy = 1 - ((j + (y + .5) / S) / GRID) * 2, q2 = qx * qx + qy * qy;
        if (q2 >= 1) continue;
        const qz = Math.sqrt(1 - q2), Y = qy * ct + qz * st, Z = -qy * st + qz * ct;
        ground += land(Math.asin(Y) * 180 / Math.PI, 15 + Math.atan2(qx, Z) * 180 / Math.PI);
      }
      const light = Math.max(0, (px * L[0] - py * L[1] + Math.sqrt(1 - r2) * L[2]) / ln);
      t[j * GRID + i] = ground >= 4 ? .45 + .55 * light : .1 + .08 * light;
    }
    return t;
  }
  const TONES = {};

  // The pictures of the page on show: drawn once, and again only while the light runs or a card lights up.
  const Art = (() => {
    let list = [], raf = 0, timer = 0, sweep = -1, span = 0, ro = null;
    const still = matchMedia("(prefers-reduced-motion: reduce)");
    const color = (name) => {
      const h = getComputedStyle(document.documentElement).getPropertyValue(name).trim().replace("#", "");
      return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
    };
    let base = [89, 130, 107], hot = [181, 199, 189];
    const mix = (t) => "rgb(" + base.map((b, i) => Math.round(b + (hot[i] - b) * t)).join(",") + ")";
    function layout(root) {
      const o = root.getBoundingClientRect();
      span = o.width + o.height;
      for (const g of list) {
        const r = g.cv.getBoundingClientRect(), dpr = Math.min(3, devicePixelRatio || 1);
        g.size = r.width; g.x = r.left - o.left; g.y = r.top - o.top;
        g.cv.width = g.cv.height = Math.round(r.width * dpr);
        g.dpr = dpr;
      }
    }
    function draw(now) {
      const w = sweep < 0 ? -1e9 : -260 + (now - sweep) / 2600 * (span + 520);
      for (const g of list) {
        if (!g.size) continue;
        const c = g.cv.getContext("2d"), p = g.size / GRID, R = p * .47;
        c.setTransform(g.dpr, 0, 0, g.dpr, 0, 0);
        c.clearRect(0, 0, g.size, g.size);
        const t = TONES[g.k] || (TONES[g.k] = tones(g.k));
        // The dots in a few shades, so each is one path.
        const paths = [new Path2D(), new Path2D(), new Path2D(), new Path2D(), new Path2D()];
        for (let j = 0; j < GRID; j++) for (let i = 0; i < GRID; i++) {
          const v = t[j * GRID + i];
          if (v < .03) continue;
          const x = (i + .5) * p, y = (j + .5) * p, d = g.x + x + g.y + y;
          const light = Math.exp(-(((d - w) / 80) ** 2));
          const r = R * Math.sqrt(v) * (1 + .22 * light), q = Math.min(4, Math.round(Math.max(g.hot, light * .6) * 4));
          paths[q].moveTo(x + r, y); paths[q].arc(x, y, r, 0, 2 * Math.PI);
        }
        paths.forEach((path, q) => { c.fillStyle = mix(q / 4); c.fill(path); });
      }
    }
    function tick(now) {
      raf = 0;
      if (!list.length || !list[0].cv.isConnected) { stop(); return; }
      let busy = false;
      for (const g of list) {
        const to = g.on ? 1 : 0;
        if (g.hot !== to) { g.hot = still.matches ? to : to ? Math.min(1, g.hot + .12) : Math.max(0, g.hot - .08); busy = true; }
      }
      if (sweep >= 0 && now - sweep > 2600) sweep = -1;
      draw(now);
      if (busy || sweep >= 0) raf = requestAnimationFrame(tick);
    }
    const wake = () => { if (!raf) raf = requestAnimationFrame(tick); };
    function light() {
      if (!list.length || !list[0].cv.isConnected) { stop(); return; }
      timer = setTimeout(light, 14000);
      if (still.matches || document.hidden) return;
      sweep = performance.now(); wake();
    }
    function stop() { cancelAnimationFrame(raf); raf = 0; clearTimeout(timer); timer = 0; if (ro) ro.disconnect(); ro = null; list = []; }
    function mount(root) {
      stop();
      base = color("--field-tint"); hot = color("--ink-muted");
      list = [...root.querySelectorAll("canvas.mn-art")].map((cv) => ({ cv, k: cv.dataset.k, hot: 0, on: false }));
      for (const g of list) {
        const card = g.cv.closest("a"), set = (on) => () => { g.on = on; wake(); };
        card.addEventListener("pointerenter", set(true)); card.addEventListener("pointerleave", set(false));
        card.addEventListener("focus", set(true)); card.addEventListener("blur", set(false));
      }
      layout(root); draw(-1e9);
      ro = new ResizeObserver(() => { layout(root); draw(-1e9); });
      ro.observe(root);
      timer = setTimeout(light, 1800);
    }
    return { mount };
  })();
  const art = (k) => '<canvas class="mn-art" data-k="' + k + '" aria-hidden="true"></canvas>';

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
      MODES.map(([m, href]) => '<a class="mn-mode" href="' + href + '" data-route><b>' + esc(T("free." + m)) + '</b>' + art(m) + '</a>').join("") +
      '</div></section>' : "";
    const go = (href, k, title, hint) => '<a class="mn-more" href="' + href + '" data-route><b>' + esc(title) + '</b><span>' + esc(hint) + '</span>' + art(k) + '</a>';
    return '<div class="dk-wrap mn"><div class="mn-dayslot" id="mnDay">' + dayHTML() + '</div>' + free +
      '<nav class="mn-pages" aria-label="' + esc(T("desk.nav")) + '">' + go("/days", "days", T("days"), T("desk.days_hint")) +
      (leaders() ? go("/top", "top", T("desk.top"), T("desk.top_hint")) : "") + go("/me", "me", T("prof.title"), T("desk.me_hint")) + '</nav></div>';
  }

  Router.screen("menu", /^\/(?:index\.html)?$/, {
    open(m, from) {
      show("menu", menuHTML(), from, "maply");
      Art.mount(page.querySelector(".mn"));
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
