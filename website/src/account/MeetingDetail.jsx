// A recorded meeting, laid out like the desktop app: title and speakers, the
// audio player, then Timeline, Transcript, AI Summary and Action Items tabs.
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { API_BASE } from '../utils/api';
import { Markdown } from '../utils/markdown';
import './meeting-detail.css';

// The microphone speaker is always the account owner; others take a colour in
// order of appearance.
const ME_COLOR = '#e11d48';
const SPEAKER_COLORS = ['#d97706', '#7c3aed', '#059669', '#2563eb', '#db2777', '#0891b2', '#65a30d', '#9333ea'];

const CLOUD_STATUS = {
  ready: 'Encrypted cloud backup available.',
  pending: 'Encrypted cloud backup is pending.',
  storage_full: 'Cloud storage is full, so this recording was not backed up.',
  not_archived: 'Encrypted cloud backup needs an active Pro plan. The recording is kept on the device that recorded it.',
  local_file_unavailable: 'The server could not find audio to back up. The recording may still be on the device that recorded it.',
  deleted: 'The cloud audio copy was deleted.',
  unavailable: 'Cloud audio backup is unavailable.',
};

function clock(seconds) {
  const total = Math.max(0, Math.floor(seconds || 0));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

function longDuration(seconds) {
  const total = Math.round(seconds || 0);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

function parseSegments(meeting) {
  let raw = [];
  try {
    raw = JSON.parse(meeting.segments_json || '[]');
  } catch {
    raw = [];
  }
  if (!Array.isArray(raw)) return [];
  return raw
    .filter(seg => seg && Number.isFinite(seg.start))
    .map(seg => ({
      start: seg.start,
      end: Number.isFinite(seg.end) ? seg.end : seg.start,
      text: seg.text || '',
      key: seg.speaker_key || seg.speaker || 'unknown',
      name: seg.speaker_display_name || seg.speaker || 'Speaker',
    }));
}

async function api(token, method, path, body) {
  const options = { method, headers: { Authorization: `Bearer ${token}` } };
  if (body) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }
  const res = await fetch(`${API_BASE}${path}`, options);
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || `Request failed (${res.status})`);
  return text ? JSON.parse(text) : null;
}

export default function MeetingDetail({ meeting: initial, token, formatDate, onBack, onDelete, onChanged }) {
  const [meeting, setMeeting] = useState(initial);
  const [tab, setTab] = useState('timeline');
  const [menuOpen, setMenuOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [titleDraft, setTitleDraft] = useState('');
  const [titleStatus, setTitleStatus] = useState('');
  const [savingTitle, setSavingTitle] = useState(false);
  const [speakerDrafts, setSpeakerDrafts] = useState({});
  const [speakerStatus, setSpeakerStatus] = useState('');
  const [savingSpeakers, setSavingSpeakers] = useState(false);
  const [filter, setFilter] = useState('');
  const [tooltip, setTooltip] = useState(null);
  const audio = useRef(null);
  const audioUrl = useRef(null);
  const [audioState, setAudioState] = useState('idle'); // idle | loading | ready | error
  const [audioError, setAudioError] = useState('');
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [muted, setMuted] = useState(false);
  const [speed, setSpeed] = useState('1');
  const scrubber = useRef(null);
  const menu = useRef(null);

  // Always show the server's current view of the meeting.
  useEffect(() => {
    let cancelled = false;
    api(token, 'GET', `/meetings/${initial.id}`).then(fresh => {
      if (!cancelled && fresh) setMeeting(fresh);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [initial.id, token]);

  useEffect(() => {
    const drafts = {};
    for (const speaker of meeting.speakers || []) drafts[speaker.id] = meeting.speaker_names?.[speaker.id] || '';
    setSpeakerDrafts(drafts);
  }, [meeting.speakers, meeting.speaker_names]);

  useEffect(() => () => {
    if (audioUrl.current) URL.revokeObjectURL(audioUrl.current);
  }, []);

  useEffect(() => {
    if (!menuOpen) return;
    const close = event => { if (!menu.current?.contains(event.target)) setMenuOpen(false); };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [menuOpen]);

  const segments = useMemo(() => parseSegments(meeting), [meeting]);
  const duration = Math.max(meeting.duration || 0, ...segments.map(s => s.end), 1);

  // Speakers in the server's order, each with a colour and talk-time stats.
  const speakers = useMemo(() => {
    const list = (meeting.speakers || []).map(s => ({ id: s.id, name: s.name || s.default_name, defaultName: s.default_name, isMe: s.is_me }));
    for (const seg of segments) {
      if (!list.some(s => s.id === seg.key)) list.push({ id: seg.key, name: seg.name, defaultName: seg.name, isMe: false });
    }
    let other = 0;
    return list.map(s => {
      const own = segments.filter(seg => seg.key === s.id);
      const talk = own.reduce((sum, seg) => sum + Math.max(0, seg.end - seg.start), 0);
      return { ...s, color: s.isMe ? ME_COLOR : SPEAKER_COLORS[other++ % SPEAKER_COLORS.length], turns: own.length, share: talk / duration };
    });
  }, [meeting.speakers, segments, duration]);
  const speakerById = Object.fromEntries(speakers.map(s => [s.id, s]));
  const nameOf = seg => speakerById[seg.key]?.name || seg.name;
  const colorOf = seg => speakerById[seg.key]?.color || '#6b7280';

  const cloudReady = meeting.cloud_audio_status === 'ready';

  const loadAudio = async () => {
    if (audioState === 'ready') return true;
    if (audioState === 'loading') return false;
    setAudioState('loading');
    setAudioError('');
    try {
      const res = await fetch(`${API_BASE}/meetings/${meeting.id}/audio`, { headers: { Authorization: `Bearer ${token}` } });
      if (!res.ok) throw new Error((await res.text()).trim() || 'The recording could not be loaded.');
      const blob = await res.blob();
      audioUrl.current = URL.createObjectURL(blob);
      audio.current.src = audioUrl.current;
      audio.current.playbackRate = Number(speed);
      setAudioState('ready');
      return true;
    } catch (err) {
      setAudioState('error');
      setAudioError(err.message || 'The recording could not be loaded.');
      return false;
    }
  };

  const playFrom = async seconds => {
    if (!cloudReady || !(await loadAudio())) return;
    audio.current.currentTime = seconds;
    setTime(seconds);
    audio.current.play().catch(() => {});
  };

  const togglePlay = async () => {
    if (!cloudReady) return;
    if (playing) {
      audio.current.pause();
      return;
    }
    if (await loadAudio()) audio.current.play().catch(() => {});
  };

  const seekFromPointer = event => {
    const rect = scrubber.current.getBoundingClientRect();
    const fraction = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    playFrom(fraction * duration);
  };

  const download = async () => {
    if (!(await loadAudio())) return;
    const link = document.createElement('a');
    link.href = audioUrl.current;
    link.download = `${(meeting.title || 'meeting').replace(/[^\w .-]+/g, '').trim() || 'meeting'}.ogg`;
    link.click();
  };

  const saveTitle = async event => {
    event.preventDefault();
    const title = titleDraft.trim();
    if (!title) {
      setTitleStatus('Enter a name.');
      return;
    }
    if (title === meeting.title) {
      setRenaming(false);
      return;
    }
    setSavingTitle(true);
    setTitleStatus('');
    try {
      const saved = await api(token, 'PUT', `/meetings/${meeting.id}/title`, { title });
      const next = { ...meeting, title: saved.title };
      setMeeting(next);
      onChanged?.(next);
      setRenaming(false);
    } catch (err) {
      setTitleStatus(err.message);
    } finally {
      setSavingTitle(false);
    }
  };

  const speakerDirty = speakers.some(s => (speakerDrafts[s.id] || '').trim() !== (meeting.speaker_names?.[s.id] || ''));
  const saveSpeakers = async event => {
    event.preventDefault();
    setSavingSpeakers(true);
    setSpeakerStatus('Saving…');
    const names = Object.fromEntries(Object.entries(speakerDrafts).map(([id, v]) => [id, v.trim()]).filter(([, v]) => v));
    try {
      const updated = await api(token, 'PUT', `/meetings/${meeting.id}/speakers`, { speaker_names: names, previous_names: meeting.speaker_names || {} });
      setMeeting(updated);
      onChanged?.(updated);
      setSpeakerStatus('Names saved');
    } catch (err) {
      setSpeakerStatus(err.message);
    } finally {
      setSavingSpeakers(false);
    }
  };

  const activeIndex = playing || time > 0 ? segments.findIndex(seg => time >= seg.start && time < seg.end) : -1;
  const query = filter.trim().toLowerCase();
  const rows = segments.map((seg, i) => ({ seg, i })).filter(({ seg }) => !query || nameOf(seg).toLowerCase().includes(query) || seg.text.toLowerCase().includes(query));

  // Consecutive segments from one speaker read as one transcript turn.
  const turns = [];
  for (const seg of segments) {
    const last = turns[turns.length - 1];
    if (last && last.key === seg.key && seg.start - last.end < 2) {
      last.text += ` ${seg.text}`;
      last.end = seg.end;
    } else turns.push({ ...seg });
  }

  const ticks = [];
  const step = duration > 3600 ? 600 : duration > 900 ? 300 : duration > 240 ? 60 : 15;
  for (let t = 0; t <= duration; t += step) ticks.push(t);

  return (
    <div className="meeting-detail">
      <button type="button" className="btn btn-sm meeting-back" onClick={onBack}>← Back to Meetings</button>

      <section className="card meeting-player-card">
        <div className="meeting-title-row">
          {renaming ? (
            <form className="meeting-title-form" onSubmit={saveTitle}>
              <input
                className="auth-input"
                value={titleDraft}
                maxLength={512}
                autoFocus
                aria-label="Meeting name"
                onChange={e => setTitleDraft(e.target.value)}
                onKeyDown={e => { if (e.key === 'Escape') setRenaming(false); }}
              />
              <button type="submit" className="btn btn-primary btn-sm" disabled={savingTitle}>Save</button>
              <button type="button" className="btn btn-sm" onClick={() => setRenaming(false)}>Cancel</button>
              {titleStatus && <p className="meeting-inline-error" role="alert">{titleStatus}</p>}
            </form>
          ) : (
            <h2 className="meeting-title">{meeting.title || 'Untitled meeting'}</h2>
          )}
          <div className="meeting-menu" ref={menu}>
            <button type="button" className="meeting-menu-button" aria-label="More options" onClick={() => setMenuOpen(open => !open)}>•••</button>
            {menuOpen && (
              <div className="meeting-menu-list" role="menu">
                <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setTitleDraft(meeting.title || ''); setTitleStatus(''); setRenaming(true); }}>Rename</button>
                <button type="button" role="menuitem" className="danger" onClick={() => { setMenuOpen(false); onDelete(meeting.id); }}>Delete meeting</button>
              </div>
            )}
          </div>
        </div>

        <div className="meeting-meta">
          <span>{formatDate(meeting.timestamp)}</span>
          <span>{longDuration(duration)}</span>
          <span>{speakers.length} {speakers.length === 1 ? 'speaker' : 'speakers'}</span>
        </div>
        {meeting.description && <p className="meeting-description">{meeting.description}</p>}

        {speakers.length > 0 && (
          <details className="meeting-speakers">
            <summary>Rename speakers</summary>
            <p className="meeting-speakers-hint">Names are saved for this meeting and update the transcript, timeline, summary and action items.</p>
            <form onSubmit={saveSpeakers}>
              {speakers.map(s => (
                <label key={s.id} className="meeting-speaker-row">
                  <span className="meeting-speaker-label"><span className="speaker-dot" style={{ background: s.color }} />{s.defaultName}{s.isMe ? ' (microphone)' : ''}</span>
                  <input
                    className="auth-input"
                    maxLength={80}
                    placeholder={s.defaultName}
                    value={speakerDrafts[s.id] || ''}
                    onChange={e => { setSpeakerDrafts(d => ({ ...d, [s.id]: e.target.value })); setSpeakerStatus(''); }}
                  />
                </label>
              ))}
              <div className="meeting-speakers-actions">
                <button type="submit" className="btn btn-primary btn-sm" disabled={!speakerDirty || savingSpeakers}>Save names</button>
                <span role="status">{speakerStatus}</span>
              </div>
            </form>
          </details>
        )}

        <audio
          ref={audio}
          preload="none"
          onPlay={() => setPlaying(true)}
          onPause={() => setPlaying(false)}
          onEnded={() => setPlaying(false)}
          onTimeUpdate={e => setTime(e.currentTarget.currentTime)}
        />
        <div className="meeting-audio-bar">
          <button type="button" className="meeting-play" onClick={togglePlay} disabled={!cloudReady || audioState === 'loading'} aria-label={playing ? 'Pause' : 'Play'}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d={playing ? 'M6 4h4v16H6zm8 0h4v16h-4z' : 'M7 4v16l13-8Z'} /></svg>
          </button>
          <span className="meeting-clock">{clock(time)} / {clock(duration)}</span>
          <div
            className={`meeting-scrubber ${cloudReady ? '' : 'disabled'}`}
            ref={scrubber}
            onClick={cloudReady ? seekFromPointer : undefined}
            role="slider"
            aria-label="Playback position"
            aria-valuemin={0}
            aria-valuemax={Math.round(duration)}
            aria-valuenow={Math.round(time)}
          >
            <div className="meeting-scrubber-fill" style={{ width: `${Math.min(100, (time / duration) * 100)}%` }} />
          </div>
          <button type="button" className="meeting-icon-button" aria-label={muted ? 'Unmute' : 'Mute'} onClick={() => { audio.current.muted = !muted; setMuted(!muted); }}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />{muted ? <><line x1="22" y1="9" x2="16" y2="15" /><line x1="16" y1="9" x2="22" y2="15" /></> : <path d="M15.54 8.46a5 5 0 0 1 0 7.07M19.07 4.93a10 10 0 0 1 0 14.14" />}</svg>
          </button>
          <select className="meeting-speed" value={speed} aria-label="Playback speed" onChange={e => { setSpeed(e.target.value); if (audio.current) audio.current.playbackRate = Number(e.target.value); }}>
            {['0.75', '1', '1.25', '1.5', '2'].map(v => <option key={v} value={v}>{v}x</option>)}
          </select>
        </div>
        <div className="meeting-cloud-row">
          <p role="status">{audioState === 'loading' ? 'Loading the encrypted recording…' : audioError || CLOUD_STATUS[meeting.cloud_audio_status] || 'Cloud audio status is unavailable.'}</p>
          <button type="button" className="btn btn-sm" onClick={download} disabled={!cloudReady || audioState === 'loading'}>Download audio</button>
        </div>

        <div className="meeting-tabs" role="tablist">
          {[['timeline', 'Timeline'], ['transcript', 'Transcript'], ['summary', 'AI Summary'], ['actions', 'Action Items']].map(([id, label]) => (
            <button key={id} type="button" role="tab" aria-selected={tab === id} className={`meeting-tab ${tab === id ? 'active' : ''}`} onClick={() => setTab(id)}>{label}</button>
          ))}
        </div>
      </section>

      {tab === 'timeline' && (
        <>
          <section className="card meeting-timeline-card">
            <div className="meeting-card-head">
              <h3>Speaker Timeline</h3>
              <div className="speaker-legend">
                {speakers.map(s => <span key={s.id} className="speaker-chip" style={{ borderColor: s.color, color: s.color }}><span className="speaker-dot" style={{ background: s.color }} />{s.name}</span>)}
              </div>
            </div>
            {segments.length === 0 ? <p className="markdown-empty">No speech was detected in this meeting.</p> : (
              <div className="meeting-tl">
                <div className="meeting-tl-ruler">
                  <span />
                  <div className="meeting-tl-ruler-track">
                    {ticks.map(t => <span key={t} style={{ left: `${(t / duration) * 100}%`, transform: t / duration > 0.92 ? 'translateX(calc(-100% - 2px))' : undefined }}>{clock(t)}</span>)}
                  </div>
                </div>
                {speakers.map(s => (
                  <div key={s.id} className="meeting-tl-lane">
                    <div className="meeting-tl-lane-label">
                      <span className="speaker-dot" style={{ background: s.color }} />
                      <div>
                        <strong>{s.name}</strong>
                        <small>{s.turns} {s.turns === 1 ? 'turn' : 'turns'} · {Math.round(s.share * 100)}% talk time</small>
                      </div>
                    </div>
                    <div className="meeting-tl-lane-track">
                      {segments.map((seg, i) => seg.key === s.id && (
                        <button
                          key={i}
                          type="button"
                          className={`meeting-tl-box ${i === activeIndex ? 'active' : ''}`}
                          style={{ left: `${(seg.start / duration) * 100}%`, width: `max(4px, ${((seg.end - seg.start) / duration) * 100}%)`, borderColor: s.color, background: `${s.color}22` }}
                          title={`${s.name} · ${clock(seg.start)}`}
                          onClick={() => playFrom(seg.start)}
                        />
                      ))}
                      <div className="meeting-tl-playhead" style={{ left: `${(time / duration) * 100}%` }} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className="card meeting-segments-card">
            <div className="meeting-card-head">
              <div>
                <h3>Turn-by-Turn Segments ({segments.length})</h3>
                <p className="meeting-card-hint">Click a row to play from that turn. Hover for its start and duration.</p>
              </div>
              <input className="auth-input meeting-filter" placeholder="Filter speaker or text…" value={filter} onChange={e => setFilter(e.target.value)} aria-label="Filter segments" />
            </div>
            <div className="segments-scroll" onMouseLeave={() => setTooltip(null)}>
              <table className="segments-table">
                <thead><tr><th className="col-index">#</th><th className="col-speaker">Speaker</th><th>Transcript</th></tr></thead>
                <tbody>
                  {rows.map(({ seg, i }) => {
                    const details = `Start ${clock(seg.start)} · Duration ${Math.max(0, seg.end - seg.start).toFixed(1)}s`;
                    const show = e => setTooltip({ text: details, x: e.clientX, y: e.clientY });
                    return (
                      <tr key={i} className={i === activeIndex ? 'active' : ''} onClick={() => playFrom(seg.start)} onMouseEnter={show} onMouseMove={show} onMouseLeave={() => setTooltip(null)}>
                        <td className="col-index">#{String(i + 1).padStart(2, '0')}</td>
                        <td className="col-speaker"><span className="speaker-chip" style={{ borderColor: colorOf(seg), color: colorOf(seg) }}><span className="speaker-dot" style={{ background: colorOf(seg) }} />{nameOf(seg)}</span></td>
                        <td>{seg.text || <em className="markdown-empty">(No speech text)</em>}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {rows.length === 0 && <p className="markdown-empty">No segments match this filter.</p>}
            </div>
            {tooltip && <div className="segments-tooltip" role="tooltip" style={{ left: Math.min(tooltip.x + 14, window.innerWidth - 220), top: tooltip.y + 16 }}>{tooltip.text}</div>}
          </section>
        </>
      )}

      {tab === 'transcript' && (
        <section className="card meeting-transcript-card">
          {turns.length === 0 ? <p className="markdown-empty">No transcript was produced for this meeting.</p> : turns.map((turn, i) => (
            <div key={i} className={`transcript-turn ${(playing || time > 0) && time >= turn.start && time < turn.end ? 'active' : ''}`} onClick={() => playFrom(turn.start)}>
              <div className="transcript-turn-head">
                <span className="speaker-dot" style={{ background: colorOf(turn) }} />
                <strong style={{ color: colorOf(turn) }}>{nameOf(turn)}</strong>
                <span className="transcript-time">{clock(turn.start)}</span>
              </div>
              <p>{turn.text}</p>
            </div>
          ))}
        </section>
      )}

      {tab === 'summary' && (
        <section className="card meeting-text-card">
          <Markdown text={meeting.summary} empty="No summary was generated for this meeting." />
        </section>
      )}

      {tab === 'actions' && (
        <section className="card meeting-text-card">
          <Markdown text={meeting.action_items} empty="No action items were found in this meeting." />
        </section>
      )}
    </div>
  );
}
