// Transcription history view.

let displayedHistoryOwner=null;
let historyCursor=null;
let historyGeneration=0;
let historyLoading=false;
let historyPreviewObserver=null;
const historyIcons = {
  copy: '<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
  delete: '<path d="M3 6h18M9 6V4h6v2M6 6l1 14h10l1-14M10 10v6m4-6v6"/>',
  command: '<path d="m4 6 6 6-6 6m9 0h7"/>',
  dictation: '<rect x="9" y="2" width="6" height="13" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3m-4 0h8"/>'
};
function historyIcon(name) {
  const icon=document.createElementNS('http://www.w3.org/2000/svg','svg');
  icon.setAttribute('viewBox','0 0 24 24'); icon.setAttribute('width','16'); icon.setAttribute('height','16');
  icon.setAttribute('fill','none'); icon.setAttribute('stroke','currentColor'); icon.setAttribute('stroke-width','1.7');
  icon.setAttribute('stroke-linecap','round'); icon.setAttribute('stroke-linejoin','round'); icon.setAttribute('aria-hidden','true');
  icon.innerHTML=historyIcons[name]; return icon;
}
function historyDay(date) {
  const now=new Date(), yesterday=new Date(now); yesterday.setDate(now.getDate()-1);
  const key=date.toLocaleDateString();
  const label=key===now.toLocaleDateString()?'Today':key===yesterday.toLocaleDateString()?'Yesterday':date.toLocaleDateString(undefined,{weekday:'long'});
  return {key,label,date:date.toLocaleDateString(undefined,{month:'short',day:'numeric',year:'numeric'})};
}
function appendHistoryResult(row,list,generation) {
  const date=new Date(row.created_at*1000), day=historyDay(date);
  let group=[...list.children].find(el=>el.dataset.day===day.key);
  if(!group) {
    group=document.createElement('section'); group.className='history-day'; group.dataset.day=day.key;
    const heading=document.createElement('h3'); heading.className='history-day-heading'; heading.textContent=day.label;
    const calendar=document.createElement('span'); calendar.textContent=day.date; heading.append(calendar); group.append(heading); list.append(group);
  }
  const card=document.createElement('article'); card.className='history-result'; card.dataset.id=row.id;
  const header=document.createElement('div'); header.className='history-result-header';
  const badge=document.createElement('span'); badge.className='history-mode-icon'; badge.append(historyIcon(row.mode==='command'?'command':'dictation'));
  const meta=document.createElement('div'); meta.className='history-result-meta';
  const mode=document.createElement('span'); mode.className='history-mode'; mode.textContent=row.mode==='command'?'Command Mode':'Dictation';
  const time=document.createElement('time'); time.dateTime=date.toISOString(); time.title=date.toLocaleString();
  time.textContent=date.toLocaleTimeString(undefined,{hour:'numeric',minute:'2-digit'}); meta.append(mode,time);
  const actions=document.createElement('div'); actions.className='history-result-actions';
  const copy=document.createElement('button'); copy.className='history-button history-copy'; copy.type='button'; copy.append(historyIcon('copy'),document.createTextNode('Copy'));
  copy.addEventListener('click',async()=>{
    try { await navigator.clipboard.writeText(row.text); if(generation!==historyGeneration)return; copy.lastChild.textContent='Copied'; showToast('Full text copied to clipboard.','Copied','success'); }
    catch {if(generation===historyGeneration)showToast('Select the text to copy it. Clipboard access is unavailable.','Copy unavailable','warning');}
  });
  const remove=document.createElement('button'); remove.className='history-delete'; remove.type='button'; remove.title='Delete entry'; remove.setAttribute('aria-label','Delete history entry'); remove.append(historyIcon('delete'));
  remove.addEventListener('click',async()=>{
    const confirmed=await showConfirmDialog({title:'Delete this history entry?',message:'This removes the saved result from your account. This cannot be undone.',confirmText:'Delete entry',isDanger:true});
    if(!confirmed || generation!==historyGeneration)return;
    remove.disabled=true;
    try {
      await invoke('delete_transcription_history',{id:row.id}); if(generation!==historyGeneration)return;
      historyPreviewObserver?.unobserve(text);card.remove(); if(!group.querySelector('.history-result'))group.remove();
      document.getElementById('transcription-history-status').textContent=list.querySelector('.history-result')?'History entry deleted.':'No transcription history yet. Your next final result will appear here.';
    } catch {if(generation===historyGeneration){showToast('Please try again. Your saved text has not been removed.','Could not delete entry','warning');remove.disabled=false;}}
  });
  actions.append(copy,remove); header.append(badge,meta,actions);
  const text=document.createElement('p'); text.className='history-result-text'; text.textContent=row.text; text.dir='auto';
  const expand=document.createElement('button'); expand.className='history-expand'; expand.type='button'; expand.textContent='Show more'; expand.hidden=true; expand.setAttribute('aria-expanded','false');
  expand.addEventListener('click',()=>{
    const open=card.classList.toggle('is-expanded'); expand.setAttribute('aria-expanded',String(open)); expand.textContent=open?'Show less':'Show more';
  });
  card.append(header,text,expand); group.append(card);
  // Test actual rendered overflow, including long Hindi/RTL text and explicit newlines.
  const measure=()=>{if(!card.classList.contains('is-expanded'))expand.hidden=text.scrollHeight<=text.clientHeight+1;};
  if(window.ResizeObserver){
    if(!historyPreviewObserver)historyPreviewObserver=new ResizeObserver(entries=>entries.forEach(({target})=>{
      if(!target.isConnected){historyPreviewObserver.unobserve(target);return;}
      const item=target.closest('.history-result');
      if(!item.classList.contains('is-expanded'))item.querySelector('.history-expand').hidden=target.scrollHeight<=target.clientHeight+1;
    }));
    historyPreviewObserver.observe(text);
  }
  requestAnimationFrame(measure);
}
async function loadTranscriptionHistory(more=false){
 if(more && historyLoading)return;
 const generation=more?historyGeneration:++historyGeneration;
 const list=document.getElementById('transcription-history-list');
 const status=document.getElementById('transcription-history-status');
 const next=document.getElementById('transcription-history-more');
 const refresh=document.getElementById('transcription-history-refresh');
 if(!list||!status)return;
 if(!more){historyPreviewObserver?.disconnect();list.replaceChildren();historyCursor=null;}
 historyLoading=true;list.setAttribute('aria-busy','true');status.textContent='Loading history…';next.disabled=true;refresh.disabled=true;
 try{
  const rows=await invoke('transcription_history',{before:more?historyCursor?.created_at:null,beforeId:more?historyCursor?.id:null});
  if(generation!==historyGeneration)return;
  for(const row of rows){if(![...list.querySelectorAll('.history-result')].some(card=>card.dataset.id===row.id))appendHistoryResult(row,list,generation);}
  historyCursor=rows.at(-1)||historyCursor;next.hidden=rows.length<50;
  status.textContent=list.querySelector('.history-result')?'':'No transcription history yet. Your next final result will appear here.';
 }catch(err){if(generation===historyGeneration){status.textContent='History could not be loaded. Check your connection and try Refresh.';next.hidden=!more;}}
 finally{if(generation===historyGeneration){historyLoading=false;list.setAttribute('aria-busy','false');next.disabled=false;refresh.disabled=false;}}
}
document.getElementById('transcription-history-refresh')?.addEventListener('click',()=>loadTranscriptionHistory());
document.getElementById('transcription-history-more')?.addEventListener('click',()=>loadTranscriptionHistory(true));
