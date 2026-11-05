/* ---------- Pages: one document, an address for each screen ---------- */
// web/web.go answers index.html on every address in its Pages list, and this file maps the path to a screen. Each
// screen registers itself from its own file; app.js starts the router once the words and /api/today are in, so every
// screen's file has run by then and open() can use T().
//
//   Router.screen("days", /^\/days(?:\/([\w-]+))?$/, {
//     open(m, from) {},  // m is the path's match, m[1] the day; from is the screen before, "" on the first page.
//                        // open runs again when the path changes within the screen: /days, then /days/14.
//     close(to) {},      // optional: put away what open drew; to is the next screen's name.
//     nav: { href: "/days", t: "days" }, // optional: a link in the computer's header, its words under app.<t>
//   });
//
//   Router.go("/days/14")            a new page from code: a history entry on the site
//   Router.go("/", { replace: true }) the same page, another address
//   Router.back("/days")             the page's own Back: the previous entry if it is ours, else the parent named
//   Router.on((name, from) => {})    after every change of screen, for the header's navigation
//   Router.current                   the screen on show: "today", "days", …
//   Router.nav()                     the header's links, in the order the screens came: [{ name, href, t }]
//
// A name registered again replaces the screen in its place: a file that takes over an address only registers it.
// Paths are tried in the order the screens came. An address no screen claims shows today and says so in the
// address bar.
//
// A link <a href="/days" data-route> goes through the router on a plain click; with a modifier or the middle
// button the browser opens its own new tab.
//
// In Telegram there is no browser history: its Back button is the only one, and the page's history would go back
// out of the app on some clients. There the router only replaces the address, keeping Telegram's hash, and Back
// goes to the parent.
const Router = (() => {
  const screens = [], listeners = [];
  let current = null, started = false;
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
    if (telegram()) { if (!u.search) u.search = location.search; u.hash = location.hash; }
    return u.pathname + u.search + u.hash;
  }
  function go(path, opts) {
    const to = url(path), same = to === location.pathname + location.search + location.hash;
    if (!started || telegram() || same || (opts && opts.replace)) history.replaceState({ maply: depth() }, "", to);
    else history.pushState({ maply: depth() + 1 }, "", to);
    if (started) route();
  }
  function back(parent) {
    if (!telegram() && depth() > 0) history.back();
    else go(parent || "/", { replace: true });
  }
  function start() {
    if (started) return;
    started = true;
    history.replaceState({ maply: depth() }, "", location.href);
    addEventListener("popstate", route);
    route();
  }
  document.addEventListener("click", (e) => {
    const a = e.target.closest && e.target.closest("a[data-route]");
    if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || a.target) return;
    e.preventDefault();
    go(a.getAttribute("href"));
  });
  return {
    screen, go, back, start,
    on(fn) { listeners.push(fn); },
    nav: () => screens.filter((x) => x.nav).map((x) => ({ name: x.name, href: x.nav.href, t: x.nav.t })),
    get current() { return current ? current.name : ""; },
  };
})();
