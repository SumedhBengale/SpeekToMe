// Writes Speek's static icons from the same geometry the live rig animates.
// Run after changing speek.js: node shared/speek/build-icons.js
import { writeFileSync } from 'node:fs';
import { speekSvg, SPEEK_COLORS } from './speek.js';

const icons = [
  { path: '../../website/public/assets/speek.svg', mode: 'idle', faceColor: SPEEK_COLORS.faceOnLight }
];

for (const icon of icons) {
  const target = new URL(icon.path, import.meta.url);
  writeFileSync(target, `${speekSvg(icon)}\n`);
  console.log(`wrote ${target.pathname}`);
}
