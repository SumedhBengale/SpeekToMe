const fs=require('node:fs'),assert=require('node:assert/strict'),path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../src-tauri/installer.nsh'),'utf8');
assert.match(source,/StrCpy \$DeleteAppDataCheckboxState 0/);
for(const unsafe of [/RMDir\s+\/r/i,/Remove-Item/i,/cmdkey/i,/C:\\Users/i])assert.ok(!unsafe.test(source));
console.log('PASS: uninstall disables Tauri AppData deletion and preserves user profiles');
