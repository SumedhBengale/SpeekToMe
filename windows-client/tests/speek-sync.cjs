// The desktop UI's copy of Speek must match the shared rig the website uses.
const fs=require('node:fs');const path=require('node:path');const assert=require('node:assert/strict');
const shared=fs.readFileSync(path.join(__dirname,'../../shared/speek/speek.js'),'utf8');
const copy=fs.readFileSync(path.join(__dirname,'../frontend/js/vendor/speek.js'),'utf8');
assert.equal(copy,shared,'run node shared/speek/sync-desktop.js after changing shared/speek/speek.js');
console.log('Speek copy matches shared/speek/speek.js');
