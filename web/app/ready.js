/* Telegram is told the app is ready before MapLibre and Pannellum, about a megabyte, arrive: until then it keeps its own
   placeholder over maply's loading screen, which is here precisely to cover that wait. tg.js does the rest. */
(() => {
  const tg = window.Telegram && Telegram.WebApp;
  if (!tg || tg.platform === "unknown") return;
  const at = (v) => tg.isVersionAtLeast(v);
  tg.ready(); tg.expand();
  // Telegram header and backdrop in the field colour, bottom bar in the credits-row colour.
  if (at("6.9")) { tg.setHeaderColor("#245A3C"); tg.setBackgroundColor("#245A3C"); }
  if (at("7.10")) tg.setBottomBarColor("#1A412B");
  // The panorama and globe are dragged in every direction: a swipe down must not collapse the app.
  if (at("7.7")) tg.disableVerticalSwipes();
})();
