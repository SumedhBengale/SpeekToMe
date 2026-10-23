// Markdown rendering and HTML escaping helpers.

function parseMarkdownToHTML(md) {
  if (!md) return '';
  // Escape untrusted server/model output before adding the small markdown subset
  // supported by this view. This prevents stored XSS in meeting summaries.
  let html = escapeHtml(String(md));
  // Convert headers
  html = html.replace(/^### (.*$)/gim, '<h3>$1</h3>');
  html = html.replace(/^## (.*$)/gim, '<h2>$1</h2>');
  html = html.replace(/^# (.*$)/gim, '<h1>$1</h1>');
  // Convert bold
  html = html.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
  
  const lines = html.split('\n');
  let inList = false;
  let parsedLines = [];
  for (let line of lines) {
    let clean = line.trim();
    if (clean.startsWith('- ') || clean.startsWith('* ')) {
      if (!inList) {
        parsedLines.push('<ul style="margin: 0 0 10px 0; padding-left: 20px;">');
        inList = true;
      }
      parsedLines.push(`<li>${clean.substring(2)}</li>`);
    } else {
      if (inList) {
        parsedLines.push('</ul>');
        inList = false;
      }
      if (clean) {
        parsedLines.push(`<p style="margin: 0 0 8px 0;">${line}</p>`);
      } else {
        parsedLines.push('<br>');
      }
    }
  }
  if (inList) {
    parsedLines.push('</ul>');
  }
  return parsedLines.join('\n');
}

function escapeHtml(value) {
  if (value === null || value === undefined) return '';
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}
