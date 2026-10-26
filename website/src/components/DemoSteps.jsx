import './DemoSteps.css';

export default function DemoSteps({ steps, completed, label = 'Demo steps', compact = false }) {
  return (
    <ol className={`demo-checklist${compact ? ' demo-checklist-compact' : ''}`} aria-label={label}>
      {steps.map((step, index) => {
        const done = index < completed;
        const current = index === completed;
        return (
          <li key={step} data-state={done ? 'complete' : current ? 'current' : 'upcoming'} aria-current={current ? 'step' : undefined}>
            <span className="demo-checklist-box" aria-hidden="true">{done && <svg viewBox="0 0 12 12" width="11" height="11" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d="m2.5 6 2.2 2.2L9.5 3.5" /></svg>}</span>
            <span>{step}<span className="demo-checklist-sr">{done ? ', complete' : current ? ', in progress' : ', upcoming'}</span></span>
          </li>
        );
      })}
    </ol>
  );
}
