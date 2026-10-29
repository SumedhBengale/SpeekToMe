// Speek's setup guide on the source DOM with mocked native adapters: what Speek
// says on each step, the reply buttons and the intro under reduced motion.
const {JSDOM}=require('jsdom');
const fs=require('node:fs');const path=require('node:path');const assert=require('node:assert/strict');
const frontend=path.join(__dirname,'../frontend');
// jsdom has no module scripts: inline the vendored rig and the guide as one script.
const moduleSource=file=>fs.readFileSync(path.join(frontend,file),'utf8').replace(/^import .*$/m,'').replace(/^export /gm,'');
(async()=>{
 const dom=new JSDOM(fs.readFileSync(path.join(frontend,'index.html'),'utf8'),{url:'https://fixture.invalid',runScripts:'outside-only',pretendToBeVisual:true});const w=dom.window;
 const add=w.document.addEventListener.bind(w.document);w.document.addEventListener=(type,...args)=>{if(type!=='DOMContentLoaded')add(type,...args)};
 w.setInterval=()=>0;w.fetch=async()=>({ok:true,json:async()=>({})});
 w.matchMedia=query=>({matches:/reduced-motion/.test(query),media:query,addEventListener(){},removeEventListener(){}});
 w.IntersectionObserver=class{observe(){}disconnect(){}};
 let cfg={session_token:'synthetic-session',signed_in:true,onboarding_version:0,onboarding_step:1,onboarding_pro_defaults_applied:false,hotkey:'Ctrl + Win',hotkey_mode:'hold',theme:'forest',typing_mode:false,command_mode:false,autostart:false,transcription_mode:'batch',languages:['en'],active_language:'en'};
 const user={id:1,name:'Synthetic Person',email:'fixture@example.invalid',plan_tier:'free',plan_active:false};
 w.__TAURI__={core:{invoke:async(command,args)=>{
  if(command==='get_config')return {...cfg};if(command==='save_config'){cfg=JSON.parse(JSON.stringify(args.config));return;}
  if(command==='get_desktop_platform')return 'windows';if(command==='get_setup_capabilities')return {shortcut:true,insertion:true,microphone:true,microphone_device:'Synthetic microphone'};
  if(command==='get_user_info')return user;if(command==='check_setup_microphone')return {ready:true,device:'Synthetic microphone',level:20};
  if(command==='get_voice_level')return 0.5;if(command==='check_hotkey_conflicts')return {conflicts:[],verified:true};return null;
 }},event:{listen:async()=>()=>{}}};
 w.eval(require('./frontend-scripts.cjs')(frontend)+`;window.wizardTest={initializeWizard,goToStep,load(c,u){currentConfig=c;currentUserInfo=u;applyConfigToUI(c);updateOnboardAuthUI(u)}};`);
 w.eval(`(()=>{${moduleSource('js/vendor/speek.js')}\n${moduleSource('js/speek-guide.js')}})()`);
 const t=w.wizardTest;const tick=()=>new Promise(r=>setTimeout(r,0));
 const line=()=>w.document.querySelector('.speek-line').textContent;
 assert.ok(w.document.getElementById('onboarding').classList.contains('has-speek'),'the guide takes over the setup card');
 t.load(cfg,user);await t.initializeWizard();await tick();
 assert.match(line(),/Free plan/,'signed-in setup resumes on step 2, and reduced motion skips the intro');
 await t.goToStep(1);await tick();assert.match(line(),/Nice to meet you, Synthetic!/,'a signed-in user is greeted by first name');
 await t.goToStep(5);await tick();assert.match(line(),/paste it all at once/);
 const replies=[...w.document.querySelectorAll('#speek-replies-5 .speek-reply')];
 assert.deepEqual(replies.map(r=>r.getAttribute('aria-pressed')),['true','false'],'replies mirror the checkbox');
 replies[1].click();await tick();
 assert.equal(w.document.getElementById('onboard-typing-mode-check').checked,true,'a reply sets the real checkbox');
 assert.match(line(),/Typing it out/);
 await t.goToStep(6);await tick();
 assert.equal(w.document.querySelector('#speek-replies-6 [data-value="on"]').disabled,true,'Pro-only answers are disabled on Free');
 assert.match(line(),/Pro trick/);
 await t.goToStep(6);await t.goToStep(7);await t.goToStep(8);await t.goToStep(9);await t.goToStep(10);await tick();
 assert.match(line(),/Your turn! Click the box, hold Ctrl \+ Windows/);
 const practice=w.document.getElementById('onboard-practice-text');practice.value='Hello there';practice.dispatchEvent(new w.Event('input',{bubbles:true}));await tick();
 assert.match(line(),/“Hello there” Look at that!/);
 await t.goToStep(11);await tick();assert.match(line(),/system tray/);
 assert.equal(cfg.typing_mode,true,'the reply was saved with setup progress');
 console.log('Speek guide: greetings, step lines, replies, Pro gating and practice passed.');
})().catch(error=>{console.error(error);process.exit(1);});
