// The desktop UI is served as plain files without a bundler, so it keeps a copy
// of speek.js. Run after changing speek.js: node shared/speek/sync-desktop.js
// windows-client/tests/speek-sync.cjs fails while the copy is stale.
import { copyFileSync } from 'node:fs';

const source = new URL('./speek.js', import.meta.url);
const target = new URL('../../windows-client/frontend/js/vendor/speek.js', import.meta.url);
copyFileSync(source, target);
console.log(`copied to ${target.pathname}`);
