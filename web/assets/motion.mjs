export const motionDuration = speed => Math.min(600, 1000 / speed);
const keys = ['first', 'last', 'min', 'max'];
const equal = (a, b) => keys.every(key => a[key] === b[key]);

// Move the chart camera (axis ranges), never the recorded values or dates.
// One scheduled frame per chart; cancelled callbacks cannot repaint.
export class CameraMotion {
  constructor({ request = callback => requestAnimationFrame(callback), cancel = id => cancelAnimationFrame(id), now = () => performance.now() } = {}) {
    Object.assign(this, { request, cancel, now, camera: null, target: null, frame: null, revision: 0 });
  }
  stop() { this.revision++; if (this.frame !== null) this.cancel(this.frame); this.frame = null; }
  clear() { this.stop(); this.camera = null; this.target = null; }
  move(target, draw, duration = 0) {
    this.stop(); this.target = { ...target };
    const from = this.camera;
    if (!from || duration <= 0 || equal(from, target)) { this.camera = { ...target }; draw(this.camera); return; }
    const revision = this.revision, began = this.now();
    const tick = timestamp => {
      if (revision !== this.revision) return;
      const progress = Math.max(0, Math.min(1, (timestamp - began) / duration));
      // Fast replay should pan steadily between days rather than braking and
      // accelerating on every bar. Slower transitions retain a soft ease.
      const ease = duration <= 200 ? progress : progress * progress * (3 - 2 * progress);
      this.camera = Object.fromEntries(keys.map(key => [key, from[key] + (target[key] - from[key]) * ease]));
      if (progress === 1) this.camera = { ...target };
      draw(this.camera); this.frame = progress < 1 ? this.request(tick) : null;
    };
    draw(from); this.frame = this.request(tick);
  }
}
