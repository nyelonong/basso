import { animate, gaussian, mulberry32, pointLayer, trackPointer } from "./gl";

// The about page's mark: the starter-grid's 16 steps x 4 rows made of glowing dust. It gathers from a cloud, a
// playhead of light sweeps it at 150 BPM with each lit step flaring on its hit, then it lets go and gathers again.
// It plays no sound.

const ROWS: [velocity: number, steps: number[]][] = [
  [0.85, [0, 3, 6, 8, 11, 14]],
  [0.65, [4, 12]],
  [0.32, Array.from({ length: 16 }, (_, i) => i)],
  [0.55, [4, 12]],
];
const STEP_SECONDS = 60 / (150 * 4);
const PITCH = 6.2;
const CELL = 5;
const X0 = -(15 * PITCH) / 2;
const Y0 = -(3 * PITCH) / 2;
const MARK = 17000;
const EMBERS = 280;
// One cycle, in seconds: gather (0-3), hold and sweep (3-15), let go (15-17.5), rest (17.5-19).
const CYCLE = 19;

const f = (n: number) => n.toFixed(3);
const VERTEX = `
  attribute vec3 aHome, aFrom; attribute vec4 aInfo;
  uniform vec2 uRes, uCenter; uniform float uScale, uTime, uGather, uDpr, uHead; uniform vec3 uPointer;
  varying vec3 vCol; varying float vA;
  const vec3 PALE = vec3(0.941, 0.933, 0.914);
  const vec3 HOT = vec3(0.957, 0.318, 0.173);
  const vec3 FLARE = vec3(1.0, 0.72, 0.6);
  void main() {
    float k = aInfo.x, vel = aInfo.y, col = aInfo.z, row = aInfo.w;
    vec2 p; float a; vec3 c = PALE;
    if (row < -0.5) {
      float rise = fract(aHome.y - uTime * (0.01 + 0.016 * k));
      p = vec2(aHome.x + 0.02 * sin(uTime * 0.3 + k * 40.0), rise) * uRes;
      a = (0.07 + 0.18 * k) * (0.6 + 0.4 * sin(uTime * (1.0 + 2.0 * k) + k * 30.0));
      if (vel > 0.5) { c = HOT; a *= 1.5; }
      gl_PointSize = (1.0 + 2.2 * aHome.z) * uDpr;
    } else {
      float t = clamp((uGather - k * 0.45) / 0.55, 0.0, 1.0);
      t = t * t * (3.0 - 2.0 * t);
      vec3 q = mix(aFrom, aHome, t);
      float since = mod(uHead - col, 16.0);
      float lit = step(0.01, vel);
      float hit = lit * exp(-since * 1.3) * t;
      float pass = exp(-since * 2.6) * t;
      vec2 cell = vec2(${f(X0)} + col * ${f(PITCH)}, ${f(Y0)} + row * ${f(PITCH)});
      q.xy += (q.xy - cell) * hit * 0.3 * vel;
      q.xy += vec2(sin(uTime * 1.3 + k * 50.0), cos(uTime * 1.1 + k * 70.0)) * (0.1 + 1.4 * (1.0 - t));
      p = uCenter + q.xy * uScale;
      vec2 d = p - uPointer.xy; float r = length(d);
      p += d / max(r, 0.0001) * uPointer.z * 40.0 * uDpr * exp(-r * r / (8000.0 * uDpr * uDpr));
      float blur = abs(q.z) / 4.0 + (1.0 - t) * 0.8;
      a = mix(0.15, 0.36 + 0.3 * vel, lit) * (1.0 + 2.6 * hit * vel + 0.9 * pass) / (1.0 + blur * blur * 3.0);
      c = mix(mix(PALE, HOT, pass * 0.4), mix(HOT * (0.5 + 0.5 * vel), FLARE, hit * 0.5), lit);
      gl_PointSize = (1.2 + 0.9 * fract(k * 7.0) + blur * 2.6) * (uScale / 5.5) * (1.0 + 0.5 * hit);
    }
    vCol = c; vA = a;
    gl_Position = vec4((p / uRes) * 2.0 - 1.0, 0.0, 1.0) * vec4(1.0, -1.0, 1.0, 1.0);
  }`;

function particles() {
  const r = mulberry32(31);
  const gauss = gaussian(r);
  const cells: { row: number; col: number; vel: number; weight: number }[] = [];
  ROWS.forEach(([velocity, steps], row) => {
    // Velocities are lifted so the quietest row (the hi-hats) still reads as lit.
    const vel = 0.4 + 0.6 * velocity;
    for (let col = 0; col < 16; col++) {
      const on = steps.includes(col);
      cells.push({ row, col, vel: on ? vel : 0, weight: on ? 0.6 + 0.4 * vel : 0.22 });
    }
  });
  const total = cells.reduce((sum, c) => sum + c.weight, 0);
  const home: number[] = [];
  const from: number[] = [];
  const info: number[] = [];
  for (const c of cells) {
    const n = Math.round((MARK * c.weight) / total);
    const cx = X0 + c.col * PITCH;
    const cy = Y0 + c.row * PITCH;
    for (let i = 0; i < n; i++) {
      let x = cx + (r() - 0.5) * CELL;
      let y = cy + (r() - 0.5) * CELL;
      // An empty step is mostly its outline.
      if (!c.vel && r() < 0.75) {
        const side = Math.floor(r() * 4);
        if (side < 2) x = cx + (side ? 1 : -1) * (CELL / 2);
        else y = cy + (side === 3 ? 1 : -1) * (CELL / 2);
      }
      home.push(x + gauss() * 0.13, y + gauss() * 0.13, gauss() * 0.6);
      const a = r() * 6.2832;
      const d = 35 + r() * 60;
      from.push(Math.cos(a) * d, Math.sin(a) * d * 0.5, gauss() * 14);
      info.push((c.col / 16) * 0.6 + r() * 0.4, c.vel, c.col, c.row);
    }
  }
  for (let j = 0; j < EMBERS; j++) {
    home.push(r(), r(), r());
    from.push(0, 0, 0);
    info.push(r(), r() < 0.25 ? 1 : 0, 0, -1);
  }
  return { home: new Float32Array(home), from: new Float32Array(from), info: new Float32Array(info) };
}

const gatherAt = (t: number) => {
  const c = t % CYCLE;
  if (c < 3) return c / 3;
  if (c < 15) return 1;
  if (c < 17.5) return 1 - (c - 15) / 2.5;
  return 0;
};

/** Draws the mark on the fixed `canvas`, centred on `slot`. Without WebGL nothing draws and the page is unchanged. */
export function startStepMark(canvas: HTMLCanvasElement, slot: HTMLElement) {
  const { home, from, info } = particles();
  const layer = pointLayer(
    canvas,
    VERTEX,
    [
      ["aHome", home, 3],
      ["aFrom", from, 3],
      ["aInfo", info, 4],
    ],
    ["uRes", "uCenter", "uScale", "uTime", "uGather", "uDpr", "uHead", "uPointer"],
    info.length / 4,
  );
  if (!layer) return;
  const { gl, u } = layer;
  const pointer = trackPointer();
  animate((t, still) => {
    const { dpr, w, h } = layer.begin();
    const box = slot.getBoundingClientRect();
    const size = Math.min(box.width * 0.96, (box.height / 26) * 100, 860);
    pointer.on += (pointer.target - pointer.on) * 0.08;
    gl.uniform2f(u.uRes, w, h);
    gl.uniform2f(u.uCenter, (box.left + box.width / 2) * dpr, (box.top + box.height / 2) * dpr);
    gl.uniform1f(u.uScale, (size / 100) * dpr);
    gl.uniform1f(u.uTime, t);
    gl.uniform1f(u.uGather, still ? 1 : gatherAt(t));
    gl.uniform1f(u.uDpr, dpr);
    gl.uniform1f(u.uHead, still ? 4.2 : (((t - 3) / STEP_SECONDS) % 16) + 16);
    gl.uniform3f(u.uPointer, pointer.x * dpr, pointer.y * dpr, still ? 0 : pointer.on);
    layer.draw();
  });
}
