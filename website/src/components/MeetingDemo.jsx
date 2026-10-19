import React, { useEffect, useRef, useState } from 'react';
import sample from './meetingDemoData.json';

const SPEAKERS = [
  { id: 'maya', name: 'Maya', role: 'Design', color: '#2563eb', tint: '#eff6ff' },
  { id: 'alex', name: 'Alex', role: 'Engineering', color: '#d97706', tint: '#fffbeb' },
  { id: 'sam', name: 'Sam', role: 'Product', color: '#7c3aed', tint: '#f5f3ff' },
];
const stamp = value => `0:${String(Math.floor(value)).padStart(2, '0')}`;

function PlaybackIcon({ paused }) {
  return <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    <path d={paused ? 'M8 4v16l13-8Z' : 'M6 4h4v16H6zm8 0h4v16h-4z'} />
  </svg>;
}

export default function MeetingDemo() {
  const audio = useRef(null);
  const media = useRef({ url: null, controller: null });
  const position = useRef(0);
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [ended, setEnded] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const turn = sample.turns.find(item => time >= item.start && time < item.end);
  const speaker = SPEAKERS.find(item => item.id === turn?.speaker);

  // The media clock is authoritative, including pauses, seeks, and slow loading.
  useEffect(() => {
    if (!playing) return;
    let frame;
    const update = () => {
      setTime(Math.min(audio.current.currentTime, sample.duration));
      frame = requestAnimationFrame(update);
    };
    frame = requestAnimationFrame(update);
    return () => cancelAnimationFrame(frame);
  }, [playing]);
  useEffect(() => {
    const player = audio.current;
    const resource = media.current;
    return () => {
      resource.controller?.abort();
      player.pause();
      if (resource.url) URL.revokeObjectURL(resource.url);
    };
  }, []);

  const seek = value => {
    position.current = value;
    if (audio.current.readyState > 0) audio.current.currentTime = value;
    setTime(value);
    setEnded(false);
  };
  const toggle = async () => {
    if (playing) { audio.current.pause(); return; }
    if (ended) seek(0);
    setError('');
    try {
      if (!media.current.url) {
        setLoading(true);
        const controller = new AbortController();
        media.current.controller = controller;
        // A small local blob also supports seeking on hosts without byte ranges.
        const response = await fetch('/assets/meeting-demo.mp3', { signal: controller.signal });
        if (!response.ok) throw new Error('Sample unavailable');
        const blob = await response.blob();
        if (controller.signal.aborted) return;
        media.current.url = URL.createObjectURL(blob);
        audio.current.src = media.current.url;
        audio.current.currentTime = position.current;
      }
      await audio.current.play();
    }
    catch (cause) { if (cause.name !== 'AbortError') setError('Audio could not play. Please try again.'); }
    finally { setLoading(false); }
  };

  return <div className="meeting-demo">
    <audio ref={audio} preload="none"
      onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)}
      onTimeUpdate={() => setTime(Math.min(audio.current.currentTime, sample.duration))}
      onEnded={() => { setPlaying(false); setEnded(true); setTime(sample.duration); }}
      onError={() => { setPlaying(false); setError('Audio could not load. Please try again.'); }} />
    <div className="meeting-demo-heading">
      <div><span className="meeting-demo-eyebrow">MEETING RECORDING</span><h3>Meet the team</h3></div>
      <span className="meeting-demo-count">3 speakers · 21 sec</span>
    </div>
    <p className="meeting-demo-intro">One conversation. Every speaker, in their own lane.</p>
    <div className="meeting-timeline-card">
      <div className="timeline-main-container" aria-label="Meeting speaker timeline">
        <div className="timeline-tracks-header-col">
          <div className="timeline-ruler-header-spacer"><span>Speakers</span></div>
          <div className="timeline-headers-list">
            {SPEAKERS.map(person => <div key={person.id} className="timeline-header-item" data-active={speaker?.id === person.id && !ended} style={{ '--speaker-color': person.color, '--speaker-tint': person.tint }}>
              <span className="timeline-header-swatch" style={{ background: person.color }} />
              <div className="timeline-header-info"><span className="timeline-header-name">{person.name}</span><span className="timeline-header-meta">{person.role}</span></div>
            </div>)}
          </div>
        </div>
        <div className="timeline-scroll-area">
          <div className="timeline-scroll-canvas">
            <div className="timeline-ruler-track" aria-hidden="true">
              {[0, 5, 10, 15, sample.duration].map(value => <div key={value} className="timeline-ruler-tick" style={{ left: `${value / sample.duration * 100}%` }}><span className="timeline-ruler-tick-label">{stamp(value === sample.duration ? Math.ceil(value) : value)}</span><span className="timeline-ruler-tick-line" /></div>)}
            </div>
            <div className="timeline-lanes-body">
              {SPEAKERS.map(person => <div key={person.id} className="timeline-lane-row" data-active={speaker?.id === person.id && !ended}>
                {sample.turns.filter(item => item.speaker === person.id).map(item => <button key={item.start} type="button"
                  className={`segment-box${turn === item && !ended ? ' active' : ''}`}
                  aria-label={`Jump to ${person.name} at ${stamp(item.start)}`}
                  title={`${person.name}: ${item.text}`} onClick={() => seek(item.start)}
                  style={{ left: `${item.start / sample.duration * 100}%`, width: `${(item.end - item.start) / sample.duration * 100}%`, backgroundColor: person.color }}>
                  <span className="meeting-segment-wave" aria-hidden="true">{[5, 9, 15, 8, 18, 11, 6, 16, 10, 19, 7, 13].map((height, index) => <i key={index} style={{ height }} />)}</span>
                </button>)}
              </div>)}
            </div>
            <div className="timeline-playhead" style={{ left: `${time / sample.duration * 100}%` }} aria-hidden="true"><span className="meeting-playhead-cap" /></div>
          </div>
        </div>
      </div>
    </div>
    <div className="meeting-now" style={{ '--speaker-color': speaker?.color || '#40101d', '--speaker-tint': speaker?.tint || '#faf6f7' }}>
      <div className="meeting-now-header" role="status">
        <span className="meeting-avatar" aria-hidden="true">{speaker?.name[0] || '✓'}</span>
        <div><span className="meeting-now-label">{ended ? 'Sample complete' : playing ? 'Speaking now' : time > 0 ? 'Paused on' : 'First up'}</span><strong>{ended ? 'Everyone, clearly captured' : speaker?.name || 'Ready to listen'}</strong></div>
        <span className="meeting-speaking-indicator" data-playing={playing} aria-hidden="true">{[0, 1, 2, 3].map(index => <i key={index} style={{ '--bar': index }} />)}</span>
      </div>
      <p>{ended ? 'Replay the audio or choose a segment to hear a speaker again.' : turn?.text}</p>
    </div>
    <div className="meeting-audio-controls">
      <button type="button" className="meeting-play-button" disabled={loading} onClick={toggle}><PlaybackIcon paused={!playing} />{loading ? 'Loading audio…' : playing ? 'Pause audio' : ended ? 'Replay audio' : 'Play audio'}</button>
      <span className="meeting-time" aria-hidden="true">{stamp(time)} / 0:21</span>
      <button type="button" className="meeting-restart" aria-label="Restart meeting sample" onClick={() => seek(0)}>↻</button>
    </div>
    <label className="meeting-seek-label"><span className="demo-sr-only">Seek meeting audio</span><input type="range" min="0" max={sample.duration} step="0.01" value={time} onChange={event => seek(Number(event.target.value))} aria-valuetext={`${stamp(time)} of 0:21`} /></label>
    {error && <p className="meeting-audio-error" role="alert">{error}</p>}
    <p className="demo-footnote">Sample conversation · Synthetic voices · Press play to listen</p>
  </div>;
}
