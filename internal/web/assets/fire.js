"use strict";

// The pairing page's mark: an ASCII onibi (ghost fire). A swaying teardrop
// gives the flame its shape; cooling noise and sparks make it flicker.
(() => {
  const el = document.getElementById("pair-fire");
  if (!el) return;
  const W = 40;
  const H = 20;
  const RAMP = " .:-=+*#%@";
  const MAX = RAMP.length - 1;
  const noise = new Float32Array(W * H);
  const still = matchMedia("(prefers-reduced-motion: reduce)");
  let sparks = [];
  let tick = 0;
  let last = 0;

  function cool() {
    for (let x = 0; x < W; x++) noise[(H - 1) * W + x] = Math.random();
    for (let y = 0; y < H - 1; y++) {
      for (let x = 0; x < W; x++) {
        const src = Math.min(
          W - 1,
          Math.max(0, x + Math.round(Math.random() * 2 - 1)),
        );
        noise[y * W + x] =
          noise[(y + 1) * W + src] * 0.94 + Math.random() * 0.06;
      }
    }
  }

  function step() {
    tick += 1;
    cool();
    const sway = Math.sin(tick / 11) + Math.sin(tick / 27) * 0.5;
    const height = 0.86 + Math.sin(tick / 5) * 0.05 + Math.random() * 0.05;
    const radius = W * 0.3;
    // Cells are about 1.5x taller than wide, so the round bulb spans fewer rows.
    const bulb = Math.min(0.5, radius / 1.5 / ((H - 1) * height));
    const grid = new Array(W * H).fill(0);
    for (let y = 0; y < H; y++) {
      // v runs from 0 at the base to 1 at the tip of the flame.
      const v = (H - 0.6 - y) / ((H - 1) * height);
      if (v > 1) continue;
      const half =
        v < bulb
          ? radius * Math.sqrt(1 - Math.pow(1 - v / bulb, 2))
          : radius * Math.pow((1 - v) / (1 - bulb), 1.15);
      const centre =
        (W - 1) / 2 + sway * v * v * 2.5 + Math.sin(v * 7 - tick / 3) * v * 1.2;
      for (let x = 0; x < W; x++) {
        const d = Math.abs(x - centre) / (half + 0.35);
        if (d >= 1) continue;
        const body = Math.pow(1 - d * d, 0.45) * (1 - v * 0.45);
        // Steady near the base so its curve holds; wilder toward the tip.
        const wild = Math.min(1, v / bulb) * 0.45 + 0.15;
        const flicker = 1 - wild + noise[y * W + x] * wild * 1.3;
        grid[y * W + x] = Math.min(1, body * flicker);
      }
    }
    if (Math.random() < 0.35) {
      sparks.push({
        x: (W - 1) / 2 + sway * 1.5 + (Math.random() * 8 - 4),
        y: H * (1 - height) + 1,
        life: 4 + Math.random() * 4,
      });
    }
    sparks = sparks.filter((s) => {
      s.y -= 1;
      s.x += Math.random() * 1.4 - 0.7 + sway * 0.2;
      s.life -= 1;
      const x = Math.round(s.x);
      const y = Math.round(s.y);
      if (s.life <= 0 || y < 0 || x < 0 || x >= W) return false;
      grid[y * W + x] = Math.max(grid[y * W + x], 0.12 + s.life * 0.02);
      return true;
    });
    let out = "";
    for (let y = 0; y < H; y++) {
      for (let x = 0; x < W; x++)
        out += RAMP[Math.round(grid[y * W + x] * MAX)];
      out += "\n";
    }
    el.textContent = out;
  }

  function frame(now) {
    requestAnimationFrame(frame);
    if (still.matches || document.hidden || !el.offsetParent) return;
    if (now - last < 80) return;
    last = now;
    step();
  }

  for (let i = 0; i < H; i++) cool();
  step();
  requestAnimationFrame(frame);
})();
