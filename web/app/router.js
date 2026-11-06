/* ---------- Pages: one document, an address for each screen ---------- */
// web/web.go answers index.html on every address in its Pages list, and this file maps the path to a screen. Each
// screen registers itself from its own file; app.js starts the router once the words and /api/today are in, so every
// screen's file has run by then and open() can use T().
//
//   Router.screen("days", /^\/days(?:\/([\w-]+))?$/, {
//     open(m, from) {},  // m is the path's match, m[1] the day; from is the screen before, "" on the first page.
//                        // open runs again when the path changes within the screen: /days, then /days/14.
//     close(to) {},      // optional: put away what open drew; to is the next screen's name.
//     nav: { href: "/days", t: "days", at: 2 }, // optional: the screen is a page of its own, its words under
//                        // app.<t>; Router.nav() lists them. The computer's header has its own links (desk.js).
//   });
//
//   Router.go("/days/14")            a new page from code: a history entry on the site
//   Router.go("/", { replace: true }) the same page, another address
//   Router.back("/days")             the page's own Back: the previous entry if it is ours, else the parent named
//   Router.on((name, from) => {})    after every change of screen, for the header's navigation
//   Router.guard({ running, ask })   a screen that wants to be asked before it is left (a game under way): while
//                                    running() is true, Router.go, Router.back and the browser's Back leave the page
//                                    as it was and call ask(proceed); proceed() then does what was asked.
//   Router.leave(fn)                 runs fn with the guard off: what ask's "Leave" does
//   Router.current                   the screen on show: "menu", "today", "days", …; "" before the first
//   Router.nav()                     the screens with a nav, in its order: [{ name, href, t }]
//
// "/" is the menu (menu.js) and "/today" the day (app.js). A name registered again replaces the screen in its
// place: a file that takes over an address only registers it. Paths are tried in the order the screens came. An
// address no screen claims shows the menu and says so in the address bar.
//
// A link <a href="/days" data-route> goes through the router on a plain click; with a modifier or the middle
// button the browser opens its own new tab.
//
// In Telegram there is no browser history: its Back button is the only one, and the page's history would go back
// out of the app on some clients. There the router only replaces the address, keeping Telegram's hash and its
// tgWebApp… launch parameters, and keeps the pages it went through itself: Back goes to the page before, as on the
// site, and to the parent at the first. A page's own query (/play?mode=classic) stays with that page.
const Router = (() => {
  const screens = [], listeners = [], trail = [];
  let current = null, started = false;
  // The guard; the depth of the history entry we are on; restoring is an entry put back after a Back, whose popstate
  // is still to come; stepping is a step through the history that we asked for; passing is a leave the guard allowed.
  let guarded = null, at = 0, restoring = false, stepping = false, passing = false;
  const telegram = () => { const w = window.Telegram && Telegram.WebApp; return !!w && w.platform !== "unknown"; };
  const depth = () => (history.state && history.state.maply) || 0;
  // An error in one screen is reported like any other, without stopping the change of screen.
  const safely = (fn) => { try { fn(); } catch (e) { setTimeout(() => { throw e; }); } };

  function screen(name, pattern, s) {
    const it = { name, pattern, open: s.open, close: s.close, nav: s.nav };
    const at = screens.findIndex((x) => x.name === name);
    if (at >= 0) screens[at] = it; else screens.push(it);
    if (current && current.name === name) current = it;
  }
  function find(path) {
    for (const s of screens) { const m = path.match(s.pattern); if (m) return { s, m }; }
    return null;
  }
  function route() {
    let hit = find(location.pathname);
    if (!hit) {
      hit = find("/");
      history.replaceState(history.state, "", "/" + location.search + location.hash);
    }
    if (!hit) return;
    const from = current ? current.name : "", next = hit.s;
    if (current && current !== next && current.close) safely(() => current.close(next.name));
    current = next;
    safely(() => next.open(hit.m, from));
    if (from !== next.name) listeners.forEach((fn) => safely(() => fn(next.name, from)));
  }
  function url(path) {
    const u = new URL(path, location.href);
    if (telegram()) {
      const q = new URLSearchParams(u.search);
      for (const [k, v] of new URLSearchParams(location.search)) if (k.startsWith("tgWebApp") && !q.has(k)) q.append(k, v);
      u.search = q.toString(); u.hash = location.hash;
    }
    return u.pathname + u.search + u.hash;
  }
  const vetoes = () => !passing && !!guarded && guarded.running();
  function leave(fn) {
    passing = true;
    try { fn(); } finally { passing = false; }
  }
  function go(path, opts) {
    if (vetoes()) { guarded.ask(() => leave(() => go(path, opts))); return; }
    const to = url(path), same = to === location.pathname + location.search + location.hash;
    const replace = !started || same || (opts && opts.replace);
    if (telegram() && !replace && trail.push(location.pathname) > 50) trail.shift();
    if (replace || telegram()) history.replaceState({ maply: depth() }, "", to);
    else history.pushState({ maply: depth() + 1 }, "", to);
    at = depth();
    if (started) route();
  }
  function back(parent) {
    if (vetoes()) { guarded.ask(() => leave(() => back(parent))); return; }
    if (telegram() && trail.length) go(trail.pop(), { replace: true });
    else if (!telegram() && depth() > 0) { stepping = true; history.back(); }
    else go(parent || "/", { replace: true });
  }
  // The browser's Back or Forward. Out of a screen that asks first, the entry is put back and the screen asks; a
  // second Back while it asks puts the entry back again, and the screen takes it as "stay".
  function popped() {
    const d = depth();
    if (restoring) { restoring = false; at = d; return; }
    if (!stepping && vetoes() && d !== at) {
      const step = d - at;
      restoring = true;
      history.go(-step);
      guarded.ask(() => leave(() => { stepping = true; history.go(step); }));
      return;
    }
    stepping = false; at = d;
    route();
  }
  function start() {
    if (started) return;
    started = true;
    history.replaceState({ maply: depth() }, "", location.href);
    at = depth();
    addEventListener("popstate", popped);
    route();
  }
  document.addEventListener("click", (e) => {
    const a = e.target.closest && e.target.closest("a[data-route]");
    if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || a.target) return;
    e.preventDefault();
    go(a.getAttribute("href"));
  });
  return {
    screen, go, back, start, leave,
    guard(g) { guarded = g; },
    on(fn) { listeners.push(fn); },
    nav: () => screens.filter((x) => x.nav).sort((a, b) => (a.nav.at || 0) - (b.nav.at || 0)).map((x) => ({ name: x.name, href: x.nav.href, t: x.nav.t })),
    get current() { return current ? current.name : ""; },
  };
})();
