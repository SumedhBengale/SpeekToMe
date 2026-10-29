// Writes Speek's static icons from the same geometry the live rig animates.
// Run after changing speek.js: node shared/speek/build-icons.js
// Then regenerate the desktop app's PNG, ICO and ICNS icons from app-icon.svg:
//   cd windows-client && cargo tauri icon src-tauri/icons/app-icon.svg
import { writeFileSync } from 'node:fs';
import { speekSvg, SPEEK_COLORS } from './speek.js';

// App icons keep a little air around Speek so it doesn't touch the tile edge.
const APP_ICON_VIEWBOX = '117 160 1020 1020';

const icons = [
  { path: '../../website/public/assets/speek.svg', mode: 'idle', faceColor: SPEEK_COLORS.faceOnLight },
  { path: '../../windows-client/frontend/logo.svg', mode: 'idle', faceColor: SPEEK_COLORS.faceOnDark },
  { path: '../../windows-client/src-tauri/icons/app-icon.svg', mode: 'idle', faceColor: SPEEK_COLORS.faceOnDark, viewBox: APP_ICON_VIEWBOX, size: 1024 }
];

for (const icon of icons) {
  const target = new URL(icon.path, import.meta.url);
  writeFileSync(target, `${speekSvg(icon)}\n`);
  console.log(`wrote ${target.pathname}`);
}
