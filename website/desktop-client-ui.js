// Expose the desktop client's real HTML/CSS to the website without its Tauri bridge.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const frontend = new URL("../windows-client/frontend/", import.meta.url);
function enclosingDiv(html, marker) {
  const start = html.lastIndexOf(
    '<div class="settings-card',
    html.indexOf(marker),
  );
  if (start < 0) throw new Error(`Desktop UI card missing: ${marker}`);
  const tags = /<\/?div\b[^>]*>/g;
  tags.lastIndex = start;
  let depth = 0;
  for (let match; (match = tags.exec(html));) {
    depth += match[0].startsWith("</") ? -1 : 1;
    if (!depth) return html.slice(start, tags.lastIndex);
  }
  throw new Error(`Desktop UI card is unbalanced: ${marker}`);
}
export default function desktopClientUI() {
  return {
    name: "speektome-desktop-ui",
    resolveId(id) {
      if (id === "virtual:desktop-client-ui") return "\0desktop-client-ui";
    },
    load(id) {
      if (id !== "\0desktop-client-ui") return;
      const htmlPath = fileURLToPath(new URL("index.html", frontend));
      const cssPath = fileURLToPath(new URL("style.css", frontend));
      this.addWatchFile(htmlPath);
      this.addWatchFile(cssPath);
      const html = readFileSync(htmlPath, "utf8");
      const sourceCSS = readFileSync(cssPath, "utf8").replace(
        /\/\*[\s\S]*?\*\//g,
        "",
      );
      const theme = sourceCSS.match(/:root,[\s\S]*?\{([^}]+)\}/);
      if (!theme) throw new Error("Desktop default theme missing");
      let css = `:host {${theme[1]}}`;
      // Preserve these selectors and declarations verbatim from the desktop stylesheet.
      for (const rule of sourceCSS.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        const selector = rule[1].trim();
        if (
          /^\.(settings-card$|dictation-|toggle-|plan-pill-tag$|meeting-timeline-|timeline-|segment-box)/.test(selector)
        )
          css += `${selector}{${rule[2]}}\n`;
      }
      return `export const clientCSS = ${JSON.stringify(css)}; export const cards = ${JSON.stringify({ dictation: enclosingDiv(html, 'id="streaming-mode-check"'), command: enclosingDiv(html, 'id="command-mode-check"') })};`;
    },
  };
}
