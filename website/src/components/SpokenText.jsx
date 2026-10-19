import React from 'react';

// Keep whitespace intact while marking only the sample's filler words.
export default function SpokenText({ text, strikeFillers = false }) {
  if (!strikeFillers) return text;
  return text.split(/(\b(?:um|uh)\b)/g).map((part, index) =>
    /^(?:um|uh)$/.test(part)
      ? <s key={index} className="spoken-filler">{part}</s>
      : part,
  );
}
