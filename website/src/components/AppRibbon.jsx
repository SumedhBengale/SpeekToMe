import { useEffect, useRef } from 'react';
import './AppRibbon.css';

const APPS = [
  { name: 'ChatGPT', file: 'chatgpt.avif' },
  { name: 'Claude', file: 'claude.avif' },
  { name: 'Notion', file: 'notion.avif' },
  { name: 'Slack', file: 'slack.webp' },
  { name: 'VS Code', file: 'vscode.webp' },
  { name: 'Cursor IDE', file: 'cursor-ide.webp' },
  { name: 'Obsidian', file: 'obsidian.webp' },
  { name: 'Linear', file: 'linear.avif' },
  { name: 'Arc Browser', file: 'arc.avif' },
  { name: 'Gmail', file: 'gmail.webp' },
  { name: 'Perplexity', file: 'perplexity.avif' },
  { name: 'Raycast', file: 'raycast.webp' },
  { name: 'Superhuman', file: 'superhuman.avif' },
  { name: 'Evernote', file: 'evernote.avif' },
  { name: 'Canva', file: 'canva.avif' },
  { name: 'Google Keep', file: 'google-keep.avif' },
  { name: 'Grammarly', file: 'grammarly.avif' },
  { name: 'WhatsApp', file: 'whatsapp.avif' },
  { name: 'Telegram', file: 'telegram.avif' },
  { name: 'iMessage', file: 'imessage.avif' },
  { name: 'Zoom', file: 'zoom.avif' },
  { name: 'MS Teams', file: 'teams.webp' },
  { name: 'Things', file: 'things.webp' },
  { name: 'Todoist', file: 'todoist.avif' }
];

export default function AppRibbon() {
  const ribbonRef = useRef(null);
  const trackRef = useRef(null);

  useEffect(() => {
    const ribbon = ribbonRef.current;
    const track = trackRef.current;
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
    let frame = null;
    let visible = false;
    let lastTime = 0;
    let position = 0;
    let velocity = 30;
    let direction = 1;
    let itemWidth = 128;
    let baseGap = 28;
    let previousPeriod = (itemWidth + baseGap) * APPS.length;
    let pointer = null;

    const paint = () => {
      const period = (itemWidth + baseGap) * APPS.length;
      // Preserve the visible position when the responsive item size changes.
      position = (position + ribbon.clientWidth / 2) * period / previousPeriod - ribbon.clientWidth / 2;
      position = ((position % period) + period) % period;
      previousPeriod = period;
      track.style.transform = `translate3d(${-position}px, 0, 0)`;
    };
    const draw = time => {
      const dt = lastTime ? Math.min((time - lastTime) / 1000, 0.032) : 0.016;
      lastTime = time;
      const resting = direction * 30;
      if (!pointer) {
        velocity += (resting - velocity) * (1 - Math.exp(-0.9 * dt));
        position += velocity * dt;
      }
      paint();
      frame = requestAnimationFrame(draw);
    };
    const sync = () => {
      if (visible && !document.hidden && !motion.matches) {
        if (frame == null) frame = requestAnimationFrame(draw);
      } else {
        cancelAnimationFrame(frame);
        frame = null;
        lastTime = 0;
        if (motion.matches) {
          track.style.removeProperty('transform');
        }
      }
    };
    const resize = () => {
      const small = window.matchMedia('(max-width: 600px)').matches;
      itemWidth = small ? 112 : 128;
      baseGap = small ? 16 : 28;
      paint();
    };
    const down = event => {
      if (motion.matches || (event.pointerType === 'mouse' && event.button !== 0)) return;
      pointer = { id: event.pointerId, x: event.clientX, time: performance.now() };
      ribbon.setPointerCapture(event.pointerId);
      ribbon.dataset.dragging = 'true';
      velocity = 0;
    };
    const move = event => {
      if (!pointer || pointer.id !== event.pointerId) return;
      const now = performance.now();
      const delta = event.clientX - pointer.x;
      position -= delta;
      const sampledVelocity = -delta / Math.max((now - pointer.time) / 1000, 0.008);
      velocity = Math.max(-650, Math.min(650, velocity * 0.35 + sampledVelocity * 0.65));
      pointer = { ...pointer, x: event.clientX, time: now };
      paint();
    };
    const up = event => {
      if (!pointer || pointer.id !== event.pointerId) return;
      // A flick carries on; holding still before release cancels its momentum.
      velocity *= Math.exp(-Math.max(0, performance.now() - pointer.time - 50) / 140);
      if (event.type === 'pointercancel') velocity = 0;
      if (Math.abs(velocity) > 30) direction = Math.sign(velocity);
      pointer = null;
      delete ribbon.dataset.dragging;
      if (ribbon.hasPointerCapture(event.pointerId)) ribbon.releasePointerCapture(event.pointerId);
    };
    const key = event => {
      if (motion.matches) return;
      if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
        event.preventDefault();
        direction = event.key === 'ArrowRight' ? 1 : -1;
        velocity = direction * 420;
      }
    };
    const observer = new IntersectionObserver(([entry]) => { visible = entry.isIntersecting; sync(); });
    const sizeObserver = new ResizeObserver(resize);
    observer.observe(ribbon);
    sizeObserver.observe(ribbon);
    document.addEventListener('visibilitychange', sync);
    motion.addEventListener('change', sync);
    ribbon.addEventListener('pointerdown', down);
    ribbon.addEventListener('pointermove', move);
    ribbon.addEventListener('pointerup', up);
    ribbon.addEventListener('pointercancel', up);
    ribbon.addEventListener('lostpointercapture', up);
    ribbon.addEventListener('keydown', key);
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      sizeObserver.disconnect();
      document.removeEventListener('visibilitychange', sync);
      motion.removeEventListener('change', sync);
      ribbon.removeEventListener('pointerdown', down);
      ribbon.removeEventListener('pointermove', move);
      ribbon.removeEventListener('pointerup', up);
      ribbon.removeEventListener('pointercancel', up);
      ribbon.removeEventListener('lostpointercapture', up);
      ribbon.removeEventListener('keydown', key);
    };
  }, []);

  return (
    <section className="apps-section" aria-labelledby="apps-title">
      <div className="app-ribbon-heading">
        <h2 id="apps-title">Your favorite apps. <span>Now with your voice.</span></h2>
      </div>
      <div ref={ribbonRef} className="app-ribbon" tabIndex={0} role="group" aria-label="Supported apps. Drag or use the arrow keys to explore.">
        <div className="app-ribbon-track" ref={trackRef}>
          {[0, 1, 2].map(copy => (
            <ul className="app-ribbon-group" key={copy} aria-hidden={copy > 0 ? true : undefined}>
              {APPS.map(app => <li key={app.name}><img src={`/assets/brands/${app.file}`} alt="" draggable="false" loading="lazy" /><span>{app.name}</span></li>)}
            </ul>
          ))}
        </div>
      </div>
    </section>
  );
}
