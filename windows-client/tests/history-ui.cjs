// Run with jsdom on NODE_PATH. All account/history data is synthetic.
const {JSDOM}=require('jsdom');
const fs=require('node:fs');const path=require('node:path');const assert=require('node:assert/strict');
const root=path.join(__dirname,'../frontend');
async function main(){
 const dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'https://test.invalid',runScripts:'outside-only',pretendToBeVisual:true});
 const w=dom.window, d=w.document;const add=d.addEventListener.bind(d);
 d.addEventListener=(type,...args)=>{if(type!=='DOMContentLoaded')add(type,...args)};
 let fetchRows=async()=>[],copied='',confirmed=false;const deleted=[];
 w.__TAURI__={core:{invoke:async(name,args)=>{
  if(name==='transcription_history')return fetchRows(args);
  if(name==='delete_transcription_history'){deleted.push(args.id);return;}
  return null;
 }},event:{listen:async()=>()=>{}}};
 Object.defineProperty(w.navigator,'clipboard',{value:{writeText:async(text)=>{copied=text;}}});
 w.setInterval=()=>0;
 w.eval(fs.readFileSync(path.join(root,'main.js'),'utf8')+`
 window.historyTest={load:loadTranscriptionHistory,initCustomSelects,configureDesktopSettings,
 setConfirm(fn){showConfirmDialog=fn;}};
 `);
 w.historyTest.setConfirm(async()=>confirmed);
 const now=Math.floor(Date.now()/1000);
 const long='<img src=x onerror=alert(1)>\n'+('यह एक परीक्षण है। '.repeat(40));
 const rows=Array.from({length:50},(_,i)=>({id:'row-'+i,created_at:now-i,mode:i%2?'dictation':'command',text:i===0?long:'Example '+i}));
 fetchRows=async()=>rows;
 await w.historyTest.load();
 assert.equal(d.querySelectorAll('.history-result').length,50);
 assert.equal(d.querySelectorAll('.history-day').length,1);
 assert.equal(d.getElementById('transcription-history-more').hidden,false);
 assert.equal(d.querySelector('.history-result-text').textContent,long);
 assert.equal(d.querySelector('.history-result-text img'),null,'history must not execute stored HTML');
 const first=d.querySelector('.history-result');
 Object.defineProperty(first.querySelector('p'),'scrollHeight',{value:200});
 Object.defineProperty(first.querySelector('p'),'clientHeight',{value:60});
 await new Promise(r=>setTimeout(r,30));
 const expand=first.querySelector('.history-expand');assert.equal(expand.hidden,false);
 expand.click();assert.equal(expand.getAttribute('aria-expanded'),'true');assert(first.classList.contains('is-expanded'));
 expand.click();assert.equal(expand.getAttribute('aria-expanded'),'false');
 fetchRows=async(args)=>{assert.equal(args.beforeId,'row-49');return [{id:'older',created_at:now-60,mode:'dictation',text:'Older'}]};
 await w.historyTest.load(true);
 assert.equal(d.querySelectorAll('.history-result').length,51);assert.equal(d.querySelectorAll('.history-day').length,1);
 first.querySelector('.history-copy').click();await new Promise(r=>setTimeout(r,0));assert.equal(copied,long,'copy must still work on earlier pages and include full text');
 first.querySelector('.history-delete').click();await new Promise(r=>setTimeout(r,0));assert.equal(deleted.length,0,'cancel must preserve saved data');
 confirmed=true;first.querySelector('.history-delete').click();await new Promise(r=>setTimeout(r,0));assert.deepEqual(deleted,['row-0']);assert(!first.isConnected);
 let resolveOld;
 fetchRows=()=>new Promise(resolve=>{resolveOld=resolve});const old=w.historyTest.load();
 fetchRows=async()=>[{id:'fresh',created_at:now,mode:'command',text:'Fresh account result'}];await w.historyTest.load();
 resolveOld([{id:'stale',created_at:now,mode:'dictation',text:'Stale private result'}]);await old;
 assert.equal(d.querySelectorAll('.history-result').length,1);assert.equal(d.querySelector('.history-result').dataset.id,'fresh');
 assert.equal(d.getElementById('transcription-history-refresh').disabled,false);
 fetchRows=async()=>{throw Error('private upstream detail')};await w.historyTest.load();
 assert(!d.getElementById('transcription-history-status').textContent.includes('private upstream detail'));
 w.close();console.log('History pagination, expansion, full-text copy, safe rendering, confirmed deletion and stale-response checks passed.');
}
main().catch(e=>{console.error(e);process.exitCode=1});
