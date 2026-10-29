import { useEffect, useRef } from 'react';
import { createSpeekMascot, SPEEK_ICON_VIEWBOX, SPEEK_VIEWBOX } from '@speek';

// `framing="icon"` crops to the resting mascot for logo-sized slots; hops and
// thought dots then draw outside the box, so give the slot room or keep moods calm.
export default function SpeekMascot({ mode = 'idle', faceColor, followPointer = false, framing = 'full', className, label }) {
  const svgRef = useRef(null);
  const mascotRef = useRef(null);

  useEffect(() => {
    const mascot = createSpeekMascot(svgRef.current, { mode, faceColor, followPointer });
    mascotRef.current = mascot;
    return () => {
      mascot.destroy();
      mascotRef.current = null;
    };
    // The engine owns the initial mode; later changes go through setMode.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [faceColor, followPointer]);

  useEffect(() => {
    mascotRef.current?.setMode(mode);
  }, [mode]);

  return (
    <svg
      ref={svgRef}
      className={className}
      viewBox={framing === 'icon' ? SPEEK_ICON_VIEWBOX : SPEEK_VIEWBOX}
      overflow="visible"
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    />
  );
}
