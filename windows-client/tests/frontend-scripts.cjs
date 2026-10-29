// The frontend's classic scripts, joined in the order index.html loads them,
// for tests that evaluate the page in jsdom. Module scripts are left out.
const fs=require('node:fs');const path=require('node:path');
module.exports=function frontendScripts(frontend){
 const html=fs.readFileSync(path.join(frontend,'index.html'),'utf8');
 return [...html.matchAll(/<script src="([^"]+)"><\/script>/g)].map(m=>m[1]).filter(src=>src!=='bridge.js')
  .map(src=>fs.readFileSync(path.join(frontend,src),'utf8')).join('\n;\n');
};
