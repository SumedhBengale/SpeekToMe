// Source DOM with mocked native adapters: never records, uploads, signs in, or changes OS access.
const {JSDOM}=require('jsdom');
const fs=require('node:fs');const path=require('node:path');const assert=require('node:assert/strict');
const frontend=path.join(__dirname,'../frontend');
async function run(platform){
 const dom=new JSDOM(fs.readFileSync(path.join(frontend,'index.html'),'utf8'),{url:'https://fixture.invalid',runScripts:'outside-only'});const w=dom.window;
 const add=w.document.addEventListener.bind(w.document);w.document.addEventListener=(type,...args)=>{if(type!=='DOMContentLoaded')add(type,...args)};
 w.setInterval=()=>0;w.fetch=async()=>({ok:true,json:async()=>({})});
 let cfg={session_token:null,onboarding_version:0,onboarding_step:1,onboarding_pro_defaults_applied:false,hotkey:'Ctrl + Win',hotkey_mode:'hold',theme:'classic_dark',sound_feedback:false,typing_mode:false,command_mode:false,autostart:false,transcription_mode:'batch',languages:['en'],active_language:'en',pill_bg_color:123,pill_wave_color:456};
 let failSave=false;let user=null;let caps={shortcut:false,insertion:false,microphone:true,microphone_device:'Synthetic microphone'};
 w.__TAURI__={core:{invoke:async(command,args)=>{
  if(command==='get_config')return {...cfg};if(command==='save_config'){if(failSave)throw Error('Synthetic save failure');cfg=JSON.parse(JSON.stringify(args.config));return;}
  if(command==='get_desktop_platform')return platform;if(command==='get_setup_capabilities')return caps;
  if(command==='get_user_info')return user;if(command==='check_setup_microphone')return {ready:true,device:'Synthetic microphone',level:20};
  if(command==='check_hotkey_conflicts')return {conflicts:[],verified:true};return null;
 }},event:{listen:async()=>()=>{}}};
 w.eval(fs.readFileSync(path.join(frontend,'main.js'),'utf8')+`;window.wizardTest={initializeWizard,goToStep,finishOnboarding,applyWizardProDefaults,refreshWizardAccess,gatherConfigFromUI,step:()=>currentStep,load(c,u){currentConfig=c;currentUserInfo=u;applyConfigToUI(c)},micReady(v){wizardMicrophoneReady=v}};`);
 const t=w.wizardTest;t.load(cfg,user);await t.initializeWizard();await t.goToStep(2);assert.equal(t.step(),1,'anonymous navigation must remain at login');
 cfg.session_token='synthetic-session';user={id:1,name:'Synthetic',email:'fixture@example.invalid',plan_tier:'pro',plan_active:true};t.load(cfg,user);
 await t.goToStep(2);assert.equal(t.step(),2);assert.equal(cfg.transcription_mode,'stream','first Pro setup enables streaming');
 w.document.getElementById('onboard-streaming-mode-check').checked=false;await t.goToStep(3);assert.equal(cfg.transcription_mode,'batch','later choices override the Pro default');
 await t.goToStep(4);assert.equal(t.step(),3,'missing permissions block progression');
 caps={...caps,shortcut:true,insertion:true};await t.refreshWizardAccess();w.document.getElementById('onboard-mic-test').click();await new Promise(r=>setTimeout(r,0));await t.goToStep(4);assert.equal(t.step(),4);
 w.document.getElementById('onboard-hotkey-mode-select').value='toggle';w.document.getElementById('onboard-hotkey-mode-select').dispatchEvent(new w.Event('change'));assert.match(w.document.getElementById('onboard-gesture-help').textContent,/Tap your shortcut/);
 assert.equal(cfg.theme,'classic_dark');assert.equal(cfg.sound_feedback,false);assert.equal(cfg.pill_bg_color,123,'setup must not recolour the native overlay');
 for(let step=5;step<=10;step++)await t.goToStep(step);
 await t.goToStep(11);assert.equal(t.step(),10,'practice requires text or explicit skip');
 w.document.getElementById('onboard-practice-skip').click();await new Promise(r=>setTimeout(r,0));assert.equal(t.step(),11);failSave=true;await t.finishOnboarding();assert.equal(cfg.onboarding_version,0);assert.equal(w.document.querySelector('.setup-footer').hidden,false,'finish errors must remain visible');assert.match(w.document.getElementById('onboard-save-status').textContent,/could not be saved/);failSave=false;await t.finishOnboarding();assert.equal(cfg.onboarding_version,1);
 assert.ok(w.document.getElementById('onboarding').classList.contains('hidden'));
 cfg={...cfg,onboarding_version:0,onboarding_step:7};t.load(cfg,user);await t.goToStep(7,true);assert.equal(t.step(),7,'saved progress can be restored');
 user={...user,plan_tier:'free',plan_active:false};t.load(cfg,user);await t.applyWizardProDefaults();assert.equal(w.document.getElementById('onboard-streaming-mode-check').disabled,true);assert.equal(w.document.getElementById('onboard-command-mode-check').checked,false);
 const ids=[...w.document.querySelectorAll('[id]')].map(el=>el.id);assert.equal(new Set(ids).size,ids.length,'no duplicate control IDs');
 assert.equal(w.document.getElementById('onboard-theme-select'),null);assert.equal(w.document.querySelectorAll('.onboarding-step').length,11);
 w.close();
}
(async()=>{for(const platform of ['windows','linux','macos'])await run(platform);console.log('Three-platform onboarding: login gates, Pro defaults, access checks, resume, practice, completion and hidden settings preservation passed.');})().catch(e=>{console.error(e);process.exitCode=1});
