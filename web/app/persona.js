/* ---------- The own profile's editor ---------- */
// The name and the photo a player chooses show over Telegram's everywhere until they remove them; the username is the
// address of their profile, /u/<username>, and the old one leads to the new until someone takes it. A site account
// without Telegram is asked for all three once, after it signs in: "Later" asks no more. The editor is a sheet over the
// profile, and Back comes back to it; a photo is cut square here, on a canvas, and only the square goes to the server.

// A post of the editor's: the Mini App signs it with initData, the site with its cookie, as JSON from maply's own page.
async function profPost(path, body) {
  const headers = { "Content-Type": "application/json" };
  if (inTg && tg.initData) headers.Authorization = "tma " + tg.initData;
  return reply(await get(path, { method: "POST", headers, body: JSON.stringify(body) }, 30000));
}

// EDIT is the editor on show: the profile's edit part, whether it asks the first time, and the sheet it opened over.
let EDIT = null;
const U_FORM = /^[a-z][a-z0-9_]{2,31}$/;
const cleanU = (s) => String(s || "").trim().replace(/^@+/, "").toLowerCase();

function openEdit(ask) {
  if (!PROF || !PROF.edit) return;
  haptic("light");
  EDIT = { ask: !!ask, saved: keepSheet(), busy: false, check: 0, stat: "" };
  drawEdit();
}
// Back from the editor: to the profile it opened over, drawn again with what changed; from the first question, away.
function leaveEdit() {
  const e = EDIT;
  EDIT = null;
  if (e && e.saved && e.saved.kind === "profile") { showProfile(); return; }
  if (e && e.saved) { restoreSheet(e.saved); return; }
  closeDays();
}

function drawEdit() {
  const e = PROF.edit, tgName = e.telegram || "", site = !tgName;
  const face = faceOf({ ...personOf(PROF), name: nameOf(PROF), me: true }, "big");
  let h = '<div class="pedit">';
  if (EDIT.ask) h += '<p class="pedit-intro">' + esc(T("prof.ask_note")) + '</p>';
  h += '<div class="pedit-ph">' + face + '<div class="pedit-phb">' +
    '<button class="pbtn" id="phPick">' + esc(T(e.photo ? "prof.photo_change" : "prof.photo_add")) + '</button>' +
    (e.photo ? '<button class="pbtn ghost" id="phDel">' + esc(T("prof.photo_remove")) + '</button>' : "") +
    '<input type="file" id="phFile" accept="image/jpeg,image/png,image/webp" hidden></div></div>' +
    '<p class="pnote-s">' + esc(T(site ? "prof.photo_note_site" : "prof.photo_note_tg")) + '</p>';
  h += '<label class="pfield" for="edName"><span class="up">' + esc(T("prof.name")) + '</span></label>' +
    '<input class="pin-t" id="edName" maxlength="24" autocomplete="nickname" dir="auto" lang="' + nameLang() + '" value="' + esc(e.name || "") + '" placeholder="' + esc(tgName || T("guest")) + '">' +
    '<p class="pnote-s">' + esc(site ? T("prof.name_note_site") : T("prof.name_note_tg", { name: tgName })) + '</p>';
  h += '<label class="pfield" for="edU"><span class="up">' + esc(T("prof.username")) + '</span></label>' +
    '<div class="pat" dir="ltr"><i aria-hidden="true">@</i><input class="pin-t" id="edU" maxlength="33" autocapitalize="none" autocomplete="username" spellcheck="false" dir="ltr" value="' + esc(e.username) + '" aria-describedby="edStat edUNote"></div>' +
    '<p class="pstat" id="edStat" aria-live="polite"></p>' +
    '<p class="pnote-s" id="edUNote" dir="auto"></p>';
  h += '<div class="pacts"><button class="pbtn main" id="edSave">' + esc(T("prof.save")) + '</button>' +
    (EDIT.ask ? '<button class="pbtn" id="edLater">' + esc(T("prof.later")) + '</button>' : "") + '</div></div>';
  sheet(esc(T(EDIT.ask ? "prof.ask_title" : "prof.edit")), h, EDIT.ask ? () => later() : leaveEdit);
  $("sheet").dataset.kind = "edit";
  $("phPick").onclick = () => $("phFile").click();
  $("phFile").onchange = () => { const f = $("phFile").files[0]; $("phFile").value = ""; if (f) openCrop(f); };
  if ($("phDel")) $("phDel").onclick = removePhoto;
  $("edU").oninput = checkU;
  $("edName").oninput = mark;
  $("edSave").onclick = saveEdit;
  if ($("edLater")) $("edLater").onclick = later;
  // Enter in a field saves, as a form would.
  ["edName", "edU"].forEach((id) => { $(id).onkeydown = (k) => { if (k.key === "Enter") { k.preventDefault(); saveEdit(); } }; });
  noteU();
  mark();
}

// The address under the username, as it will be.
function noteU() {
  const u = cleanU($("edU").value);
  $("edUNote").textContent = T("prof.username_note", { url: location.host + "/u/" + (U_FORM.test(u) ? u : PROF.edit.username) });
}
// What the username field says of what is typed, and whether there is anything to save.
function mark() {
  const stat = $("edStat"), u = cleanU($("edU").value), same = u === PROF.edit.username;
  stat.className = "pstat" + (EDIT.stat ? " " + EDIT.stat : "");
  stat.textContent = same ? "" : EDIT.stat === "ok" ? "✓ " + T("prof.username_free") : EDIT.stat === "taken" ? "✗ " + T("prof.username_taken")
    : EDIT.stat === "form" ? "✗ " + T("prof.username_form") : "";
  const name = $("edName").value.trim() !== (PROF.edit.name || "");
  $("edSave").disabled = EDIT.busy || (!same && EDIT.stat !== "ok") || (!EDIT.ask && same && !name);
}
// The username is asked of the server a moment after the typing stops; a reply to an older value is dropped.
let checkT = 0;
function checkU() {
  const u = cleanU($("edU").value), my = ++EDIT.check;
  clearTimeout(checkT);
  noteU();
  if (u === PROF.edit.username) { EDIT.stat = ""; mark(); return; }
  if (!U_FORM.test(u)) { EDIT.stat = "form"; mark(); return; }
  EDIT.stat = "wait"; mark();
  checkT = setTimeout(async () => {
    let r;
    try { r = await getJSON("/api/username?name=" + encodeURIComponent(u)); } catch { r = null; }
    if (!EDIT || my !== EDIT.check) return;
    EDIT.stat = !r ? "" : r.ok ? "ok" : r.error === "username_taken" ? "taken" : "form";
    mark();
  }, 350);
}

// What the server says the profile is now, everywhere it shows: the profile kept, the header's letter, the account's
// face on the site and every face of the player's already drawn.
function edited(res) {
  PROF.edit = res.edit;
  PROF.name = res.person.name || "";
  PROF.u = res.person.u || "";
  PROF.photo = res.person.photo || "";
  MY_FACE = PROF.photo;
  document.querySelectorAll("i.av.me:not(.anon)").forEach((el) => {
    el.classList.toggle("ph", !!MY_FACE);
    el.innerHTML = MY_FACE ? '<img src="' + MY_FACE + '" alt="">' : esc(initial(nameOf(PROF)));
  });
  meLetter(PROF.name);
  if (typeof ME !== "undefined" && ME && ME.account && !ME.account.telegram) { ME.account.name = PROF.name; if (typeof acctHeader === "function") acctHeader(); }
}

async function saveEdit() {
  if (!EDIT || EDIT.busy) return;
  const e = PROF.edit, body = {}, name = $("edName").value.trim(), u = cleanU($("edU").value);
  if (name !== (e.name || "")) body.name = name;
  if (u !== e.username) body.username = u;
  if (EDIT.ask) body.asked = true;
  if (!Object.keys(body).length) { leaveEdit(); return; }
  haptic("select");
  EDIT.busy = true; mark();
  let res;
  try { res = await profPost("/api/profile/edit", body); } catch (x) {
    if (!EDIT) return;
    EDIT.busy = false;
    if (x.message === "username_taken" || x.message === "username_form") { EDIT.stat = x.message === "username_taken" ? "taken" : "form"; mark(); $("edU").focus(); return; }
    mark(); toast(errorText(x));
    return;
  }
  if (!EDIT) return;
  edited(res);
  toast(T("prof.saved"));
  // A new username is a new address: the page on show moves to it.
  if (body.username && Router.current === "u" && !PROF.saved) { EDIT = null; Router.go("/u/" + PROF.edit.username, { replace: true }); return; }
  leaveEdit();
}
// "Later" on the first question: it is not asked again.
async function later() {
  const e = EDIT;
  if (!e) return;
  try { if (e.ask) edited(await profPost("/api/profile/edit", { asked: true })); } catch {}
  leaveEdit();
}

async function removePhoto() {
  if (!EDIT || EDIT.busy) return;
  haptic("select");
  EDIT.busy = true; mark();
  try { edited(await profPost("/api/profile/photo", { remove: true })); } catch (x) { toast(errorText(x)); }
  if (!EDIT) return;
  EDIT.busy = false;
  drawEdit();
}

/* ---------- Cutting the photo square ---------- */
// The photo fills a square: dragged with a finger or the mouse, zoomed with two fingers, the wheel or the slider, and
// moved with the arrows from the keyboard. The square goes up as a JPEG of CROP_SIDE pixels; the server makes its own
// smaller copy and keeps nothing else of the file.
const CROP_SIDE = 512, CROP_MAX = 25 << 20;
let CROP = null;

async function picture(file) {
  if (typeof createImageBitmap === "function") {
    try { return await createImageBitmap(file, { imageOrientation: "from-image" }); } catch {}
  }
  const url = URL.createObjectURL(file), img = new Image();
  img.src = url;
  try { await img.decode(); return img; } finally { setTimeout(() => URL.revokeObjectURL(url), 1000); }
}

async function openCrop(file) {
  if (file.size > CROP_MAX) { toast(T("prof.photo_error")); return; }
  let pic;
  try { pic = await picture(file); } catch { toast(T("prof.photo_error")); return; }
  const w = pic.width || pic.naturalWidth, h = pic.height || pic.naturalHeight;
  if (!w || !h) { toast(T("prof.photo_error")); return; }
  CROP = { pic, w, h, z: 1, x: 0, y: 0, side: 0, pts: new Map() };
  sheet(esc(T("prof.crop_title")), '<div class="pcrop"><div class="pcrop-box" id="cropBox" tabindex="0" role="img" aria-label="' + esc(T("prof.crop_hint")) + '"><canvas id="cropCv"></canvas></div>' +
    '<input type="range" id="cropZ" min="1" max="4" step="0.01" value="1" aria-label="' + esc(T("prof.crop_zoom")) + '">' +
    '<p class="pnote-s">' + esc(T("prof.crop_hint")) + '</p>' +
    '<div class="pacts"><button class="pbtn main" id="cropOk">' + esc(T("prof.crop_done")) + '</button><button class="pbtn" id="cropNo">' + esc(T("prof.cancel")) + '</button></div></div>',
  () => { CROP = null; drawEdit(); });
  $("sheet").dataset.kind = "crop";
  const box = $("cropBox");
  $("cropNo").onclick = () => { CROP = null; drawEdit(); };
  $("cropOk").onclick = sendCrop;
  $("cropZ").oninput = () => zoomTo(Number($("cropZ").value));
  box.addEventListener("pointerdown", down);
  box.addEventListener("pointermove", move);
  ["pointerup", "pointercancel", "pointerleave"].forEach((t) => box.addEventListener(t, up));
  box.addEventListener("wheel", (e) => { e.preventDefault(); zoomTo(CROP.z * Math.exp(-e.deltaY * 0.002)); }, { passive: false });
  box.addEventListener("keydown", (e) => {
    const step = 12, d = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] }[e.key];
    if (d) { e.preventDefault(); CROP.x += d[0]; CROP.y += d[1]; drawCrop(); }
    else if (e.key === "+" || e.key === "=") { e.preventDefault(); zoomTo(CROP.z * 1.1); }
    else if (e.key === "-") { e.preventDefault(); zoomTo(CROP.z / 1.1); }
  });
  if (typeof ResizeObserver === "function") new ResizeObserver(() => CROP && drawCrop()).observe(box);
  drawCrop();
  box.focus({ preventScroll: true });
}

// The picture's scale in the box: at z 1 its shorter side fills the square.
const cropScale = (side) => side / Math.min(CROP.w, CROP.h) * CROP.z;
// The picture is kept over the whole square: never a gap at an edge.
function clampCrop(side) {
  const s = cropScale(side), mx = Math.max(0, (CROP.w * s - side) / 2), my = Math.max(0, (CROP.h * s - side) / 2);
  CROP.x = Math.min(mx, Math.max(-mx, CROP.x));
  CROP.y = Math.min(my, Math.max(-my, CROP.y));
}
function paint(ctx, side) {
  const s = cropScale(side), w = CROP.w * s, h = CROP.h * s;
  ctx.fillStyle = "#fff";
  ctx.fillRect(0, 0, side, side);
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(CROP.pic, side / 2 - w / 2 + CROP.x, side / 2 - h / 2 + CROP.y, w, h);
}
function drawCrop() {
  const box = $("cropBox"), cv = $("cropCv");
  if (!CROP || !box || !cv) return;
  const side = box.clientWidth, dpr = Math.min(3, window.devicePixelRatio || 1);
  if (!side) return;
  // Moving and zooming are kept in the box's pixels: a box that changes its size keeps the same cut.
  if (CROP.side && CROP.side !== side) { CROP.x *= side / CROP.side; CROP.y *= side / CROP.side; }
  CROP.side = side;
  clampCrop(side);
  cv.width = Math.round(side * dpr); cv.height = Math.round(side * dpr);
  const ctx = cv.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  paint(ctx, side);
}
function zoomTo(z) {
  CROP.z = Math.min(4, Math.max(1, z));
  $("cropZ").value = String(CROP.z);
  drawCrop();
}
function down(e) {
  e.currentTarget.setPointerCapture(e.pointerId);
  CROP.pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
}
function move(e) {
  const p = CROP && CROP.pts.get(e.pointerId);
  if (!p) return;
  if (CROP.pts.size === 1) { CROP.x += e.clientX - p.x; CROP.y += e.clientY - p.y; }
  else {
    // Two fingers: the change in their distance zooms.
    const [a, b] = [...CROP.pts.values()], before = Math.hypot(a.x - b.x, a.y - b.y);
    p.x = e.clientX; p.y = e.clientY;
    const [c, d] = [...CROP.pts.values()], after = Math.hypot(c.x - d.x, c.y - d.y);
    if (before > 0) { CROP.z = Math.min(4, Math.max(1, CROP.z * after / before)); $("cropZ").value = String(CROP.z); }
  }
  p.x = e.clientX; p.y = e.clientY;
  drawCrop();
}
function up(e) { if (CROP) CROP.pts.delete(e.pointerId); }

async function sendCrop() {
  if (!CROP || !EDIT || EDIT.busy) return;
  const cv = document.createElement("canvas"), side = CROP.side;
  cv.width = CROP_SIDE; cv.height = CROP_SIDE;
  const ctx = cv.getContext("2d");
  ctx.scale(CROP_SIDE / side, CROP_SIDE / side);
  paint(ctx, side);
  const b64 = cv.toDataURL("image/jpeg", 0.9).split(",")[1];
  haptic("select");
  EDIT.busy = true;
  $("cropOk").disabled = true;
  try { edited(await profPost("/api/profile/photo", { photo: b64 })); toast(T("prof.saved")); }
  catch (x) { toast(x.message === "photo" ? T("prof.photo_error") : errorText(x)); }
  if (!EDIT) return;
  EDIT.busy = false;
  CROP = null;
  drawEdit();
}

/* ---------- The first question ---------- */
// A site account without Telegram that was never asked: once it is signed in, the editor opens with the question.
async function askProfile() {
  if (inTg || typeof ME === "undefined" || !ME || !ME.account || ME.account.telegram) return;
  let d;
  try { d = await getJSON("/api/profile/edit"); } catch { return; }
  if (!d || !d.edit || !d.edit.ask || $("sheet").classList.contains("on")) return;
  if (!PROF || !PROF.edit) PROF = { me: true, site: true, at: 0, saved: null };
  PROF.edit = d.edit;
  PROF.name = d.person.name || ""; PROF.u = d.person.u || ""; PROF.photo = d.person.photo || "";
  openEdit(true);
}
