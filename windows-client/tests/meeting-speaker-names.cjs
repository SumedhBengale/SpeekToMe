// Install jsdom outside the repo, then run with NODE_PATH pointing to its node_modules.
const {JSDOM} = require('jsdom');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const frontend = path.join(__dirname, '../frontend');
const dom = new JSDOM(fs.readFileSync(path.join(frontend, 'index.html'), 'utf8'), {url:'https://test.invalid', runScripts:'outside-only'});
const w = dom.window;
w.setInterval = () => 0;
const add = w.document.addEventListener.bind(w.document);
w.document.addEventListener = (type, ...args) => { if (type !== 'DOMContentLoaded') add(type, ...args); };
Object.defineProperty(w.document, 'readyState', {value:'loading'});
w.__TAURI__ = {core:{invoke:async()=>null},event:{listen:async()=>()=>{}}};
w.__assert = assert;
w.eval(fs.readFileSync(path.join(frontend, 'main.js'), 'utf8') + `
window.runSpeakerNameChecks = async () => {
  currentConfig = {session_token:'test-token'};
  activeMeetingId = 42;
  const a = __assert;
  const raw = [
    {speaker:'Speaker 1',speaker_key:'system:spk_1',speaker_display_name:'Speaker 1',start:0,end:2,text:'Hello'},
    {speaker:'Speaker 10',speaker_key:'system:spk_10',speaker_display_name:'Speaker 10',start:2,end:4,text:'Hi'},
    {speaker:'Owner',speaker_key:'mic:speaker_local',speaker_display_name:'Me',start:4,end:6,text:'Thanks'}
  ];
  let meeting = {id:42,speaker_names:{},segments_json:JSON.stringify(raw),speakers:[
    {id:'system:spk_1',name:'Speaker 1',default_name:'Speaker 1',is_me:false},
    {id:'system:spk_10',name:'Speaker 10',default_name:'Speaker 10',is_me:false},
    {id:'mic:speaker_local',name:'Me',default_name:'Me',is_me:true}
  ]};
  const player = {currentTime:3,duration:6,play:()=>{}};
  function render(updated) {
    Object.assign(meeting,updated);
    const segments = JSON.parse(updated.segments_json);
    renderMeetingTranscript(segments,player,'Owner','');
    renderMeetingTimeline(segments,6,player,'Owner');
    document.getElementById('meeting-actions-text').innerHTML = parseMarkdownToHTML(updated.action_items);
  }
  render(meeting);
  const oldColors = Object.fromEntries(Object.entries(window.__currentSpeakerThemes).map(([id,theme])=>[id,theme.color]));
  renderMeetingSpeakerEditor(meeting,render);
  const fields = [...document.querySelectorAll('#meeting-speaker-fields input')];
  const form = document.getElementById('meeting-speaker-form');
  const save = document.getElementById('meeting-speaker-save');
  const status = document.getElementById('meeting-speaker-status');
  a.equal(fields.length,3);
  a.equal(save.disabled,true);
  fields[0].value='Alice'; fields[0].dispatchEvent(new Event('input'));
  fields[1].value='Béla'; fields[2].value='Sam';
  a.equal(save.disabled,false);
  let release;
  window.fetch=async(url,options)=>{
    a.ok(url.endsWith('/meetings/42/speakers'));
    a.equal(options.method,'PUT');
    a.deepEqual(JSON.parse(options.body),{speaker_names:{'system:spk_1':'Alice','system:spk_10':'Béla','mic:speaker_local':'Sam'},previous_names:{}});
    await new Promise(resolve=>{release=resolve});
    return {ok:true,json:async()=>({...meeting,speaker_names:{'system:spk_1':'Alice','system:spk_10':'Béla','mic:speaker_local':'Sam'},
      action_items:'- Owner: Alice\\n- Owner: Béla\\n- Owner: Sam',
      segments_json:JSON.stringify(raw.map((segment,i)=>({...segment,speaker_display_name:['Alice','Béla','Sam'][i]})))})};
  };
  const pending=form.onsubmit(new Event('submit',{cancelable:true}));
  a.equal(save.disabled,true); a.ok(fields.every(field=>field.disabled));
  release(); await pending;
  a.equal(status.textContent,'Names saved');
  a.equal(player.currentTime,3);
  a.equal(document.querySelectorAll('.timeline-lane-row').length,3);
  const labels=[...document.querySelectorAll('.meeting-speaker-name')].map(el=>el.textContent);
  a.deepEqual(labels,['Alice','Béla','Sam']);
  for (const row of document.querySelectorAll('.turn-table-row')) {
    a.equal(row.querySelector('.inspector-speaker-pill').textContent.trim(),window.__currentSpeakerThemes[row.dataset.speakerId].label);
    a.equal(window.__currentSpeakerThemes[row.dataset.speakerId].color,oldColors[row.dataset.speakerId]);
  }
  a.ok(document.getElementById('meeting-actions-text').textContent.includes('Owner: Alice'));
  // A reopened editor receives saved values, including the microphone alias.
  renderMeetingSpeakerEditor(meeting,render);
  const reloaded=[...document.querySelectorAll('#meeting-speaker-fields input')];
  a.deepEqual(reloaded.map(input=>input.value),['Alice','Béla','Sam']);
  reloaded[0].value='Renamed';reloaded[0].dispatchEvent(new Event('input'));
  window.fetch=async()=>({ok:false,text:async()=> 'Save failed. Try again.'});
  await form.onsubmit(new Event('submit',{cancelable:true}));
  a.equal(status.textContent,'Save failed. Try again.');
  a.equal(reloaded[0].value,'Renamed');a.equal(save.disabled,false);
  a.equal(meeting.speaker_names['system:spk_1'],'Alice');
  // Names remain text, including potentially hostile markup.
  render({...meeting,segments_json:JSON.stringify([{...raw[0],speaker_display_name:'<img src=x onerror=alert(1)>'}]),action_items:'- Owner: <script>alert(1)</script>'});
  a.equal(document.querySelectorAll('#meeting-transcript-text img, #timeline-inspector-tbody img, #meeting-actions-text script').length,0);
  console.log('PASS: save/reload/failure feedback, all speaker views, action attribution, stable colors, playback position, safe text');
};
`);
w.runSpeakerNameChecks().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>w.close());
