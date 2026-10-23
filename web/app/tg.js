/* ---------- Telegram Mini App ---------- */
// Outside Telegram the SDK also loads, but platform is "unknown": then we behave as a regular website.
const tg = window.Telegram && Telegram.WebApp;
const inTg = !!tg && tg.platform !== "unknown";
const tgAt = (v) => inTg && tg.isVersionAtLeast(v);
// A first guess at the language, from the client, so the first screen is not blank. /api/today then names the one
// core holds: what the person picked in any bot of the family.
const LANG0 = (((inTg && tg.initDataUnsafe.user && tg.initDataUnsafe.user.language_code) || navigator.language || "en") + "")
  .toLowerCase().split(/[-_]/)[0];
if (inTg) {
  tg.ready(); tg.expand();
  // Telegram header and backdrop in the field colour, bottom bar in the credits-row colour.
  if (tgAt("6.9")) { tg.setHeaderColor("#245A3C"); tg.setBackgroundColor("#245A3C"); }
  if (tgAt("7.10")) tg.setBottomBarColor("#1A412B");
  // The panorama and globe are dragged in every direction: a swipe down must not collapse the app.
  if (tgAt("7.7")) tg.disableVerticalSwipes();
  // Cutouts, fullscreen buttons or window height changed: recompute the camera, tape and panorama.
  ["viewportChanged", "safeAreaChanged", "contentSafeAreaChanged"].forEach((e) => tg.onEvent(e, () => dispatchEvent(new Event("resize"))));
  // Outside fullscreen Telegram draws its own title bar with the app's name right above the page: the header then
  // drops the wordmark and keeps the day and the timer. Clients older than 8.0 have no fullscreen at all.
  const framed = () => document.documentElement.classList.toggle("framed", !tg.isFullscreen);
  framed();
  tg.onEvent("fullscreenChanged", () => { framed(); dispatchEvent(new Event("resize")); });
}
// Feedback: in Telegram, its haptics (works on iPhone too, where navigator.vibrate is absent); in a browser, vibrate.
const VIBRATE = { light: 8, soft: 6, medium: 14, select: 10, success: [10, 40, 10] };
function haptic(kind) {
  if (tgAt("6.1")) {
    const h = tg.HapticFeedback;
    if (kind === "success") h.notificationOccurred("success"); else if (kind === "select") h.selectionChanged(); else h.impactOccurred(kind);
  } else if (navigator.vibrate) navigator.vibrate(VIBRATE[kind]);
}
// Telegram's system Back button is shown while the globe is open and returns to the photo.
function syncBack() { if (tgAt("6.1")) view === "map" ? tg.BackButton.show() : tg.BackButton.hide(); }
// External links open in Telegram's browser, otherwise the page leaves the Mini App.
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href^='http']");
  if (a && inTg) { e.preventDefault(); tg.openLink(a.href); }
});
