/* ---------- Accounts ---------- */
// Sign-in on the site, the account menu and the accounts page. Both sign-ins go through the server (/auth/*): the page
// only follows links and posts JSON, so no provider's script runs here. A browser signed in with Telegram plays as the
// bot's player: app.js switches SITE off and talks to /api/today and the rest with the session cookie. In the Mini App
// there is no sign-in, only the accounts page, where Google is linked through a ticket opened in the phone's browser.
// The words come from the app.acct.* keys; the brands are names, the same in every language.
const BRAND = { telegram: "Telegram", google: "Google" };
let ME = null, ACCT = { code: "", ticket: "" };

const acctPost = async (path, body, method) => {
  const opts = { method: method || "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) };
  if (inTg) opts.headers.Authorization = "tma " + tg.initData;
  return reply(await get(path, opts));
};
const acctText = (code) => TX["acct.auth." + code] ? T("acct.auth." + code) : TX["error." + code] ? T("error." + code) : T("acct.auth.failed");
const here = () => location.pathname + location.search;
const tgName = (t) => t ? t.name + (t.username ? " (@" + t.username + ")" : "") : "";

// Before /api/today: who is signed in decides which API the game uses. The result of a sign-in comes back in ?auth=,
// and a ticket from the Mini App in #link=; both leave the address at once, so a reload or a shared link carries neither.
async function accountBoot() {
  if (inTg) return;
  const q = new URLSearchParams(location.search), h = new URLSearchParams(location.hash.slice(1));
  ACCT = { code: q.get("auth") || "", ticket: h.get("link") || "" };
  if (ACCT.code || ACCT.ticket) {
    q.delete("auth");
    history.replaceState(null, "", location.pathname + (q.toString() ? "?" + q : ""));
  }
  try { ME = await reply(await get("/api/me")); } catch { ME = null; }
  if (ME && ME.account && ME.account.telegram) SITE = false;
}

// After the words have loaded: the header, then whatever the address brought or the account asks.
function accountReady() {
  if (inTg) return;
  acctHeader();
  if (ACCT.code) toast(acctText(ACCT.code));
  if (ACCT.ticket) acctTicket(ACCT.ticket);
  else if (ME && ME.account && ME.guest && ME.guest.answers > 0) acctAdopt(ME.guest.answers);
}

function acctHeader() {
  const slot = $("acct");
  if (!slot) return;
  if (!ME) { slot.innerHTML = ""; return; }
  const a = ME.account;
  if (!a) {
    slot.innerHTML = '<button class="acct-in" id="acctIn">' + esc(T("acct.sign_in")) + '</button>';
    $("acctIn").onclick = acctDialog;
    return;
  }
  const who = a.telegram ? a.telegram.name : a.name || T("acct.google_account");
  slot.innerHTML = '<button class="acct-me" id="acctMe" aria-haspopup="menu" aria-expanded="false" aria-label="' + esc(T("acct.menu")) + '">' + av(who, true) + '</button>';
  $("acctMe").onclick = acctMenu;
}

/* The sign-in dialog: two buttons, each a link to /auth/<provider>/start that comes back here. */
const G_LOGO = '<svg class="g" viewBox="0 0 48 48" aria-hidden="true"><path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/><path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/><path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z"/><path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/></svg>';
const TG_LOGO = '<svg class="t" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="12" fill="#2AABEE"/><path fill="#FFFFFF" d="M5.4 11.8l11.6-4.5c.5-.2 1 .1.8.9l-2 9.3c-.1.7-.5.8-1.1.5l-3-2.2-1.5 1.4c-.2.2-.3.3-.6.3l.2-3.1 5.6-5.1c.2-.2 0-.3-.4-.1l-6.9 4.4-3-.9c-.6-.2-.7-.6.3-.9z"/></svg>';

let dlgFrom = null;
function acctBox(html, label) {
  acctClose();
  dlgFrom = document.activeElement;
  const d = el('<div class="acct-dlg" id="acctDlg" role="dialog" aria-modal="true" aria-label="' + esc(label) + '"><div class="acct-box">' +
    '<button class="sh-b acct-x" id="acctX" aria-label="' + esc(T("aria_collapse")) + '">×</button>' + html + '</div></div>');
  document.body.appendChild(d);
  for (const e of document.body.children) if (e !== d && e.id !== "toast" && e.tagName !== "SCRIPT") e.inert = true;
  d.addEventListener("click", (e) => { if (e.target === d) acctClose(); });
  $("acctX").onclick = acctClose;
  (d.querySelector(".acct-go") || $("acctX")).focus({ preventScroll: true });
  return d;
}
function acctClose() {
  const d = $("acctDlg");
  if (!d) return;
  d.remove();
  const sheetOn = $("sheet").classList.contains("on");
  for (const e of document.body.children) if (e.tagName !== "SCRIPT") e.inert = sheetOn && e.id !== "sheet" && e.id !== "toast";
  if (dlgFrom && document.contains(dlgFrom)) dlgFrom.focus({ preventScroll: true });
  dlgFrom = null;
}
addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if ($("acctDlg")) { e.preventDefault(); e.stopImmediatePropagation(); acctClose(); }
  else if ($("acctPop")) { e.preventDefault(); e.stopImmediatePropagation(); acctMenuClose(true); }
}, true);

function acctDialog() {
  haptic("light");
  const back = encodeURIComponent(here());
  const btn = (p, logo) => ME.available[p]
    ? '<a class="acct-p acct-' + p + (p === "telegram" ? " acct-go" : "") + '" href="/auth/' + p + '/start?return_to=' + back + '">' + logo + '<span>' + esc(T("acct.with_" + p)) + '</span></a>'
    : '<span class="acct-p acct-' + p + ' off" aria-disabled="true">' + logo + '<span>' + esc(T("acct.with_" + p)) + '</span></span>';
  const none = !ME.available.telegram && !ME.available.google;
  acctBox('<b class="acct-h">' + esc(T("acct.dialog_title")) + '</b><p>' + esc(T("acct.dialog_text")) + '</p>' +
    btn("telegram", TG_LOGO) + btn("google", G_LOGO) +
    (none ? '<p class="acct-off">' + esc(T("acct.auth.unavailable")) + '</p>' : '') +
    '<p class="acct-note">' + esc(T("acct.dialog_note")) + '</p><a class="acct-link" href="/privacy?lang=' + LANG + '">' + esc(T("acct.privacy")) + '</a>', T("acct.dialog_title"));
}

/* The avatar's menu: profile when the profile page is there, accounts, language, sign out. */
function acctMenu() {
  if ($("acctPop")) { acctMenuClose(true); return; }
  haptic("light");
  const a = ME.account, items = [];
  if (typeof openProfile === "function") items.push(["profile", () => openProfile()]);
  items.push(["accounts", openAccounts], ["language", openLanguage], ["sign_out", acctSignOut]);
  const who = a.telegram ? tgName(a.telegram) : a.name || T("acct.google_account");
  const pop = el('<div class="acct-pop" id="acctPop" role="menu"><p class="acct-who" lang="' + nameLang() + '">' + esc(who) + '</p>' +
    items.map(([k], i) => '<button role="menuitem" data-i="' + i + '">' + esc(T("acct." + k)) + '</button>').join("") + '</div>');
  document.body.appendChild(pop);
  $("acctMe").setAttribute("aria-expanded", "true");
  pop.querySelectorAll("button").forEach((b) => { b.onclick = () => { acctMenuClose(false); items[+b.dataset.i][1](); }; });
  pop.querySelector("button").focus({ preventScroll: true });
  setTimeout(() => addEventListener("click", acctOutside, true), 0);
}
function acctOutside(e) { if (!e.target.closest("#acctPop, #acctMe")) acctMenuClose(false); }
function acctMenuClose(focus) {
  removeEventListener("click", acctOutside, true);
  const p = $("acctPop");
  if (p) p.remove();
  const b = $("acctMe");
  if (b) { b.setAttribute("aria-expanded", "false"); if (focus) b.focus({ preventScroll: true }); }
}
async function acctSignOut() {
  try { await acctPost("/auth/logout"); } catch (e) { toast(acctText(e.message)); return; }
  try { localStorage.removeItem(NAME); } catch {}
  location.reload();
}

/* The accounts page: each sign-in, linked or not, and the account's own buttons. */
async function acctLoad() {
  const opts = inTg ? { headers: { Authorization: "tma " + tg.initData } } : {};
  ME = await reply(await get("/api/me", opts));
  return ME;
}
async function openAccounts() {
  haptic("light");
  const ticket = sheet(esc(T("acct.accounts")), '<p class="sh-wait up">' + esc(lines("loading")[0]) + '</p>');
  try { await acctLoad(); } catch (e) { if (ticket === SHEET) sheet(esc(T("acct.accounts")), '<p class="sh-wait">' + esc(errorText(e)) + '</p>'); return; }
  if (ticket !== SHEET) return;
  drawAccounts();
}
function drawAccounts() {
  const a = ME.account || (inTg ? { telegram: tgSelf(), google: false } : null);
  if (!a) { closeDays(); acctDialog(); return; }
  const linked = { telegram: !!a.telegram, google: !!a.google }, both = linked.telegram && linked.google;
  const row = (p, logo, what) => '<div class="acct-row">' + logo + '<div><b>' + BRAND[p] + '</b><span lang="' + nameLang() + '">' + esc(what) + '</span></div>' +
    (linked[p] ? (both && !(inTg && p === "telegram") ? '<button data-unlink="' + p + '">' + esc(T("acct.unlink")) + '</button>' : '')
      : ME.available[p] ? '<button class="on" data-link="' + p + '">' + esc(T("acct.link")) + '</button>' : '<span class="off">' + esc(T("acct.auth.unavailable")) + '</span>') + '</div>';
  let html = row("telegram", TG_LOGO, linked.telegram ? tgName(a.telegram) : T("acct.not_linked")) +
    row("google", G_LOGO, linked.google ? T("acct.linked") : T("acct.not_linked"));
  if (!both && (linked.telegram || linked.google)) html += '<p class="sh-sub">' + esc(T("acct.last_one")) + '</p>';
  if (inTg && !linked.google && ME.available.google) html += '<p class="sh-sub">' + esc(T("acct.open_browser")) + '</p>';
  if (linked.google) html += '<p class="sh-sub">' + esc(T("acct.google_revoke")) + '</p>';
  if (!inTg && linked.telegram && TODAY && TODAY.bot) html += '<a class="acct-wide" href="https://t.me/' + encodeURIComponent(TODAY.bot) + '" target="_blank" rel="noopener">' + esc(T("acct.get_post")) + ' ' + fwd() + '</a>';
  if (!inTg) html += '<button class="acct-wide" id="acctAll">' + esc(T("acct.sign_out_all")) + '</button>';
  if (ME.account) html += '<button class="acct-wide acct-del" id="acctDel">' + esc(T("acct.delete")) + '</button>';
  html += '<a class="acct-link" href="/privacy?lang=' + LANG + '" target="_blank" rel="noopener">' + esc(T("acct.privacy")) + '</a>';
  sheet(esc(T("acct.accounts")), '<div class="acct-list">' + html + '</div>');
  const body = $("sheet");
  body.querySelectorAll("[data-link]").forEach((b) => { b.onclick = () => acctLink(b.dataset.link, b); });
  body.querySelectorAll("[data-unlink]").forEach((b) => { b.onclick = () => acctUnlink(b.dataset.unlink, b); });
  if ($("acctAll")) $("acctAll").onclick = async () => {
    try { await acctPost("/auth/logout/all"); } catch (e) { toast(acctText(e.message)); return; }
    location.reload();
  };
  if ($("acctDel")) $("acctDel").onclick = () => acctAsk(T("acct.delete_confirm"), async () => {
    try { await acctPost("/api/account", { confirm: "delete" }, "DELETE"); } catch (e) { toast(acctText(e.message)); return; }
    try { localStorage.removeItem(NAME); localStorage.removeItem(KEPT); } catch {}
    toast(T("acct.deleted"));
    if (inTg) { ME = null; openAccounts(); } else setTimeout(() => location.reload(), 900);
  });
}
// The Mini App's own person, for the page before an account exists: Telegram is theirs already.
const tgSelf = () => {
  const u = tg.initDataUnsafe && tg.initDataUnsafe.user;
  return u ? { name: [u.first_name, u.last_name].filter(Boolean).join(" "), username: u.username || "" } : { name: "" };
};
// A question with a yes, in Telegram's own dialog when there is one.
function acctAsk(text, yes) {
  if (inTg && tgAt("6.2")) { tg.showConfirm(text, (ok) => { if (ok) yes(); }); return; }
  if (confirm(text)) yes();
}
async function acctLink(p, btn) {
  btn.disabled = true;
  try {
    if (inTg) {
      // Google refuses to sign in inside Telegram: the phone's browser takes a ticket and asks before it goes.
      const r = await acctPost("/api/account/ticket");
      tg.openLink(r.url);
      toast(T("acct.open_browser"));
    } else {
      const r = await acctPost("/auth/" + p + "/link", { return_to: here() });
      location.assign(r.url);
      return;
    }
  } catch (e) { toast(acctText(e.message)); }
  btn.disabled = false;
}
function acctUnlink(p, btn) {
  acctAsk(T("acct.unlink_confirm", { provider: BRAND[p] }), async () => {
    btn.disabled = true;
    try { await acctPost("/api/account/identity/" + p, {}, "DELETE"); } catch (e) { toast(acctText(e.message)); btn.disabled = false; return; }
    // Without Telegram the site plays as a site player again: the game starts over in that mode.
    if (!inTg && p === "telegram") { location.reload(); return; }
    try { await acctLoad(); } catch {}
    drawAccounts();
  });
}

/* The interface language. With Telegram it is core's, shared with every bot of the family; otherwise the browser's. */
const LANGS = ["en", "ru", "uk", "es", "fr", "de", "it", "pl", "cs", "tr", "sv", "be", "ca", "zh", "ja", "ar"];
const langName = (c) => { try { const n = new Intl.DisplayNames([c], { type: "language" }).of(c); return n.charAt(0).toLocaleUpperCase(c) + n.slice(1); } catch { return c; } };
function openLanguage() {
  haptic("light");
  const withTg = ME && ME.account && ME.account.telegram;
  let stored = ""; try { stored = localStorage.getItem("maply.lang") || ""; } catch {}
  const opts = (withTg ? [["", T("acct.follow_telegram")]] : []).concat(LANGS.map((c) => [c, langName(c)]));
  const cur = withTg ? LANG : stored || LANG;
  sheet(esc(T("acct.language")), '<div class="acct-langs">' + opts.map(([c, n]) => '<button data-lang="' + c + '"' + (c && c === cur ? ' class="on" aria-current="true"' : '') +
    (c ? ' lang="' + c + '"' : '') + '>' + (c && c === cur ? "◉ " : "◎ ") + esc(n) + '</button>').join("") + '</div>');
  $("sheet").querySelectorAll("[data-lang]").forEach((b) => { b.onclick = async () => {
    const c = b.dataset.lang;
    if (withTg) {
      try { await acctPost("/api/account/lang", { lang: c }); } catch (e) { toast(acctText(e.message)); return; }
    } else try { localStorage.setItem("maply.lang", c); } catch {}
    location.reload();
  }; });
}

/* A browser's answers from before the sign-in join an account that was there already only if the person says so. */
function acctAdopt(n) {
  const d = acctBox('<b class="acct-h">' + esc(T("acct.accounts")) + '</b><p>' + esc(T("acct.adopt", { n: fmt(n) })) + '</p>' +
    '<button class="acct-p acct-go" id="acctYes">' + esc(T("acct.adopt_yes")) + '</button><button class="acct-p acct-no" id="acctNo">' + esc(T("acct.adopt_no")) + '</button>', T("acct.accounts"));
  const answer = async (take) => {
    d.querySelectorAll("button").forEach((b) => { b.disabled = true; });
    let r;
    try { r = await acctPost("/api/account/adopt", { take }); } catch (e) { toast(acctText(e.message)); acctClose(); return; }
    acctClose();
    if (take) { toast(T("acct.adopted", { n: fmt(r.answers) })); setTimeout(() => location.reload(), 900); }
  };
  $("acctYes").onclick = () => answer(true);
  $("acctNo").onclick = () => answer(false);
}

/* A ticket from the Mini App: whose Telegram account Google will join, asked before going to Google. */
async function acctTicket(t) {
  let who;
  try { who = await acctPost("/auth/ticket", { ticket: t }); } catch (e) { toast(acctText(e.message)); return; }
  if (!who.google) { toast(T("acct.auth.unavailable")); return; }
  acctBox('<b class="acct-h">' + esc(T("acct.ticket_title")) + '</b><p lang="' + nameLang() + '">' + esc(T("acct.ticket_ask", { name: tgName(who) })) + '</p>' +
    '<p class="acct-note">' + esc(T("acct.ticket_warn")) + '</p>' +
    '<button class="acct-p acct-google acct-go" id="acctGo">' + G_LOGO + '<span>' + esc(T("acct.with_google")) + '</span></button>' +
    '<button class="acct-p acct-no" id="acctNo">' + esc(T("acct.cancel")) + '</button>', T("acct.ticket_title"));
  $("acctNo").onclick = acctClose;
  $("acctGo").onclick = async () => {
    $("acctGo").disabled = true;
    try { const r = await acctPost("/auth/ticket/confirm", { ticket: t, return_to: "/" }); location.assign(r.url); }
    catch (e) { toast(acctText(e.message)); acctClose(); }
  };
}

/* In the Mini App the accounts page opens from the days page, until the profile brings its own way there. */
function accountEntry() {
  return inTg && typeof openProfile !== "function" ? '<button class="acct-wide" id="acctOpen">' + esc(T("acct.accounts")) + ' ' + fwd() + '</button>' : "";
}
function accountBind() { const b = $("acctOpen"); if (b) b.onclick = openAccounts; }
