/* ---------- Telegram Mini App ---------- */
// Outside Telegram the SDK also loads, but platform is "unknown": then we behave as a regular website.
const tg = window.Telegram && Telegram.WebApp;
const inTg = !!tg && tg.platform !== "unknown";
const tgAt = (v) => inTg && tg.isVersionAtLeast(v);
// A first guess at the language, from the client, so the first screen is not blank. /api/today then names the one
// core holds: what the person picked in any bot of the family. A browser keeps the one picked on the site.
const LANG0 = (((inTg && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) ||
  (!inTg && (() => { try { return localStorage.getItem("maply.lang"); } catch { return ""; } })()) || navigator.language || "en") + "")
  .toLowerCase().split(/[-_]/)[0];
if (inTg) {
  // ready.js has already told Telegram the app is up and set its colours.
  // Cutouts, fullscreen buttons or window height changed: recompute the camera, tape and panorama.
  ["viewportChanged", "safeAreaChanged", "contentSafeAreaChanged"].forEach((e) => tg.onEvent(e, () => dispatchEvent(new Event("resize"))));
  // Outside fullscreen Telegram draws its own title bar with the app's name right above the page: the header then
  // drops the wordmark and keeps the day and the timer. Clients older than 8.0 have no fullscreen at all.
  const framed = () => document.documentElement.classList.toggle("framed", !tg.isFullscreen);
  framed();
  tg.onEvent("fullscreenChanged", () => { framed(); dispatchEvent(new Event("resize")); });
  // Telegram's own Back leads out of a page, so the sheet draws no way out of its own (app.css).
  if (tg.isVersionAtLeast("6.1")) document.documentElement.classList.add("tgback");
}
// Feedback: in Telegram, its haptics (works on iPhone too, where navigator.vibrate is absent); in a browser, vibrate.
const VIBRATE = { light: 8, soft: 6, medium: 14, select: 10, success: [10, 40, 10] };
function haptic(kind) {
  if (tgAt("6.1")) {
    const h = tg.HapticFeedback;
    if (kind === "success") h.notificationOccurred("success"); else if (kind === "select") h.selectionChanged(); else h.impactOccurred(kind);
  // A page opened by its address can buzz on its own before any tap; the browser refuses that and says so in the console.
  } else if (navigator.vibrate && (!navigator.userActivation || navigator.userActivation.hasBeenActive)) navigator.vibrate(VIBRATE[kind]);
}
// Telegram's system Back button is on every page but the menu, the first page of all. It closes a sheet first; in the
// day it goes from the map to the photo, then to the menu (app.js). On a computer the guess's map is a panel beside
// the panorama, opened by hovering: not a screen to go back from. The free game shows it for its own Back.
function syncBack() {
  if (!tgAt("6.1")) return;
  const c = document.body.classList;
  c.contains("is-sheet") || c.contains("is-free") || (!!Router.current && Router.current !== "menu") ? tg.BackButton.show() : tg.BackButton.hide();
}
// External links open in Telegram's browser, otherwise the page leaves the Mini App.
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href^='http']");
  if (a && inTg) { e.preventDefault(); tg.openLink(a.href); }
});
