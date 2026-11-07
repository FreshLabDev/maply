const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "app/pano.js"), "utf8");
const enablePanoWheelZoom = vm.runInNewContext(source + "\nenablePanoWheelZoom");

function setup({ hfov = 100, pitch = 20, yaw = 175, min = 20, max = 110 } = {}) {
  const listeners = new Map();
  const box = {
    clientHeight: 400,
    getBoundingClientRect: () => ({ left: 120, top: 40 }),
    addEventListener(name, fn) { listeners.set(name, fn); },
    removeEventListener(name, fn) { if (listeners.get(name) === fn) listeners.delete(name); },
  };
  let stopped = 0;
  const viewer = {
    getRenderer: () => ({ getCanvas: () => ({ clientWidth: 800, clientHeight: 400 }) }),
    stopMovement() { stopped++; },
    getHfov: () => hfov,
    getHfovBounds: () => [min, max],
    setHfov(value) { hfov = Math.max(min, Math.min(max, value)); },
    getPitch: () => pitch,
    setPitch(value) { pitch = value; },
    getYaw: () => yaw,
    setYaw(value) { yaw = ((value + 540) % 360) - 180; },
    mouseEventToCoords(e) {
      const x = (e.clientX - 120) / 800 * 2 - 1;
      const y = (1 - (e.clientY - 40) / 400 * 2) * 400 / 800;
      const focal = 1 / Math.tan(hfov * Math.PI / 360);
      const radians = pitch * Math.PI / 180;
      const forward = focal * Math.cos(radians) - y * Math.sin(radians);
      const distance = Math.hypot(x, forward);
      const atPitch = Math.atan((y * Math.cos(radians) + focal * Math.sin(radians)) / distance) * 180 / Math.PI;
      const atYaw = ((Math.atan2(x / distance, forward / distance) * 180 / Math.PI + yaw + 540) % 360) - 180;
      return [atPitch, atYaw];
    },
  };
  let notified = 0;
  const remove = enablePanoWheelZoom(box, () => viewer, () => notified++);
  const wheel = (clientX, clientY, deltaY, deltaMode = 0) => {
    const e = { clientX, clientY, deltaY, deltaMode, prevented: false, preventDefault() { this.prevented = true; } };
    listeners.get("wheel")(e);
    return e;
  };
  return { box, viewer, wheel, remove, listeners, get stopped() { return stopped; }, get notified() { return notified; } };
}

function assertAnchor(before, after) {
  assert.ok(Math.abs(before[0] - after[0]) < 0.001, `pitch moved: ${before[0]} to ${after[0]}`);
  assert.ok(Math.abs(((before[1] - after[1] + 540) % 360) - 180) < 0.001, `yaw moved: ${before[1]} to ${after[1]}`);
}

test("wheel zoom is immediate and keeps an off-centre point under the cursor", () => {
  const pano = setup();
  const pointer = { clientX: 730, clientY: 130 };
  const before = pano.viewer.mouseEventToCoords(pointer);
  const e = pano.wheel(pointer.clientX, pointer.clientY, -120);
  assert.equal(e.prevented, true);
  assert.equal(pano.stopped, 1);
  assert.equal(pano.notified, 1);
  assert.ok(pano.viewer.getHfov() < 100);
  assertAnchor(before, pano.viewer.mouseEventToCoords(pointer));
  pano.wheel(pointer.clientX, pointer.clientY, -120);
  assert.equal(pano.stopped, 2);
  assertAnchor(before, pano.viewer.mouseEventToCoords(pointer));
  pano.remove();
  assert.equal(pano.listeners.has("wheel"), false);
});

test("zoom out preserves the point across the yaw seam and respects field-of-view bounds", () => {
  const pano = setup({ yaw: -179, pitch: -30, hfov: 30 });
  const pointer = { clientX: 870, clientY: 320 };
  const before = pano.viewer.mouseEventToCoords(pointer);
  pano.wheel(pointer.clientX, pointer.clientY, 80, 1);
  assert.equal(pano.viewer.getHfov(), 110);
  assertAnchor(before, pano.viewer.mouseEventToCoords(pointer));
  pano.wheel(pointer.clientX, pointer.clientY, 80, 1);
  assert.equal(pano.viewer.getHfov(), 110);
  assertAnchor(before, pano.viewer.mouseEventToCoords(pointer));
});

test("zoom at the centre does not move the view, and a bound does not pan it", () => {
  const pano = setup({ hfov: 20, min: 20, max: 80 });
  const centre = { clientX: 520, clientY: 240 };
  const start = [pano.viewer.getPitch(), pano.viewer.getYaw()];
  pano.wheel(centre.clientX, centre.clientY, -120);
  assert.equal(pano.viewer.getHfov(), 20);
  assert.deepEqual([pano.viewer.getPitch(), pano.viewer.getYaw()], start);
  pano.wheel(centre.clientX, centre.clientY, 120);
  assert.equal(pano.viewer.getPitch(), start[0]);
  assert.equal(pano.viewer.getYaw(), start[1]);
});

test("ignores a viewer that has not initialized its renderer", () => {
  const pano = setup();
  pano.viewer.getRenderer = () => null;
  assert.equal(pano.wheel(500, 200, -120).prevented, false);
  assert.equal(pano.stopped, 0);
});
