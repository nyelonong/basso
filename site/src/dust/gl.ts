// Shared pieces of the dust renderers: raw WebGL points, additive and premultiplied, as on discoverd.io.

export function mulberry32(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export const gaussian = (r: () => number) => () =>
  Math.sqrt(-2 * Math.log(Math.max(r(), 1e-9))) * Math.cos(6.2832 * r());

const FRAGMENT = `
  precision mediump float; varying vec3 vCol; varying float vA;
  void main() {
    float d = length(gl_PointCoord - 0.5) * 2.0;
    if (d > 1.0) discard;
    float g = vA * exp(-d * d * 3.5);
    gl_FragColor = vec4(vCol * g, g);
  }`;

export type Layer = {
  gl: WebGLRenderingContext;
  u: Record<string, WebGLUniformLocation | null>;
  /** Clears, sizes the canvas to the window, and returns the device pixel ratio and size in device pixels. */
  begin(): { dpr: number; w: number; h: number };
  draw(): void;
};

export function pointLayer(
  canvas: HTMLCanvasElement,
  vertex: string,
  attributes: [name: string, data: Float32Array, size: number][],
  uniforms: string[],
  count: number,
): Layer | null {
  const gl = canvas.getContext("webgl", { antialias: false, premultipliedAlpha: true });
  if (!gl) return null;
  const shader = (type: number, src: string) => {
    const s = gl.createShader(type)!;
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? "dust: shader failed");
    return s;
  };
  const program = gl.createProgram()!;
  gl.attachShader(program, shader(gl.VERTEX_SHADER, vertex));
  gl.attachShader(program, shader(gl.FRAGMENT_SHADER, FRAGMENT));
  gl.linkProgram(program);
  gl.useProgram(program);
  for (const [name, data, size] of attributes) {
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(program, name);
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, size, gl.FLOAT, false, 0, 0);
  }
  const u = Object.fromEntries(uniforms.map((n) => [n, gl.getUniformLocation(program, n)]));
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.ONE, gl.ONE);
  return {
    gl,
    u,
    begin() {
      const dpr = Math.min(window.devicePixelRatio || 1, 1.75);
      const w = Math.round(canvas.clientWidth * dpr);
      const h = Math.round(canvas.clientHeight * dpr);
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
        gl.viewport(0, 0, w, h);
      }
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT);
      return { dpr, w, h };
    },
    draw() {
      gl.drawArrays(gl.POINTS, 0, count);
    },
  };
}

/**
 * Runs `draw(seconds, still)` every frame while the page is visible. Under reduced motion it draws one still frame,
 * and again on resize and scroll.
 */
export function animate(draw: (t: number, still: boolean) => void): void {
  const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const t0 = performance.now();
  let frame = 0;
  const tick = () => {
    frame = 0;
    draw(still ? 6 : (performance.now() - t0) / 1000, still);
    if (!still && !document.hidden) frame = requestAnimationFrame(tick);
  };
  const wake = () => {
    if (!frame) frame = requestAnimationFrame(tick);
  };
  document.addEventListener("visibilitychange", wake);
  if (still) {
    window.addEventListener("resize", wake);
    window.addEventListener("scroll", wake, { passive: true });
  }
  wake();
}

/** Pointer position in CSS pixels and a 0-1 presence that eases in for a mouse and out when it leaves. */
export function trackPointer() {
  const pointer = { x: -9999, y: -9999, on: 0, target: 0 };
  window.addEventListener("pointermove", (e) => {
    pointer.x = e.clientX;
    pointer.y = e.clientY;
    pointer.target = e.pointerType === "mouse" ? 1 : 0;
  });
  document.documentElement.addEventListener("pointerleave", () => (pointer.target = 0));
  return pointer;
}
