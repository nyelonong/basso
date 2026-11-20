import type { BeatQueue } from "./beat";
import { animate, mulberry32, pointLayer } from "./gl";

// The player's dust: a quiet field behind the header and the stage edges that pulses on every scheduled hit. Each
// speck listens to one voice; a kick also sends a ring of light out from the Play button.

const COUNT = 14000;
const SHARE = [0.36, 0.2, 0.3, 0.14];

const VERTEX = `
  attribute vec3 aHome; attribute vec4 aInfo;
  uniform vec2 uRes, uOrigin; uniform float uTime, uDpr, uKickAge; uniform vec4 uEnv;
  varying vec3 vCol; varying float vA;
  const vec3 PALE = vec3(0.941, 0.933, 0.914);
  const vec3 HOT = vec3(0.957, 0.318, 0.173);
  void main() {
    float k = aInfo.x, v = aInfo.y;
    float e = min(1.2, 1.25 * (v < 0.5 ? uEnv.x : v < 1.5 ? uEnv.y : v < 2.5 ? uEnv.z : uEnv.w));
    vec2 p = (aHome.xy + 0.006 * vec2(sin(uTime * 0.4 + k * 40.0), cos(uTime * 0.33 + k * 60.0))) * uRes;
    vec2 d = p - uOrigin; float r = length(d); vec2 out_ = d / max(r, 0.0001);
    float ring = exp(-pow((r - uKickAge * 1500.0 * uDpr) / (70.0 * uDpr), 2.0)) * exp(-uKickAge * 2.2);
    if (v < 0.5) p += out_ * e * 34.0 * uDpr * exp(-r / (520.0 * uDpr));
    p += out_ * ring * 10.0 * uDpr;
    float a = (0.09 + 0.2 * k) * (0.7 + 0.3 * sin(uTime * (0.8 + 1.4 * k) + k * 30.0));
    vA = a * (1.0 + 6.0 * e + 3.5 * ring);
    vCol = mix(v < 0.5 ? mix(PALE, HOT, 0.3 + 0.7 * min(e, 1.0)) : PALE, HOT, ring * 0.6);
    gl_PointSize = (1.2 + 2.4 * aHome.z) * uDpr * (1.0 + 0.9 * e + 0.6 * ring);
    gl_Position = vec4((p / uRes) * 2.0 - 1.0, 0.0, 1.0) * vec4(1.0, -1.0, 1.0, 1.0);
  }`;

function particles() {
  const r = mulberry32(11);
  const home = new Float32Array(COUNT * 3);
  const info = new Float32Array(COUNT * 4);
  for (let i = 0; i < COUNT; ) {
    const x = r();
    const y = r();
    const top = Math.exp(-y * 6);
    const edge = Math.max(0, (0.24 - x) / 0.24, (x - 0.76) / 0.24);
    if (r() > Math.max(top, edge * 0.8, 0.07)) continue;
    home.set([x, y, r()], i * 3);
    let voice = 0;
    let u = r();
    while (voice < 3 && u > SHARE[voice]) u -= SHARE[voice++];
    info.set([r(), voice, 0, 0], i * 4);
    i++;
  }
  return { home, info };
}

/** Starts the field on `canvas`; hits come from `beat`, read against the audio clock `now` (null before audio). */
export function startBeatField(canvas: HTMLCanvasElement, origin: HTMLElement, beat: BeatQueue, now: () => number | null) {
  const { home, info } = particles();
  const layer = pointLayer(
    canvas,
    VERTEX,
    [
      ["aHome", home, 3],
      ["aInfo", info, 4],
    ],
    ["uRes", "uOrigin", "uTime", "uDpr", "uKickAge", "uEnv"],
    COUNT,
  );
  if (!layer) return;
  const { gl, u } = layer;
  animate((t, still) => {
    const { dpr, w, h } = layer.begin();
    const clock = now();
    const env = clock === null || still ? [0, 0, 0, 0] : beat.envelopes(clock);
    const age = clock === null || still ? 9 : Math.min(9, beat.kickAge(clock));
    const b = origin.getBoundingClientRect();
    gl.uniform2f(u.uRes, w, h);
    gl.uniform2f(u.uOrigin, (b.left + b.width / 2) * dpr, (b.top + b.height / 2) * dpr);
    gl.uniform1f(u.uTime, t);
    gl.uniform1f(u.uDpr, dpr);
    gl.uniform1f(u.uKickAge, age);
    gl.uniform4f(u.uEnv, env[0], env[1], env[2], env[3]);
    layer.draw();
  });
}
