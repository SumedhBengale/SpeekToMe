// speekto.me Minimalist Javascript Interactions
document.addEventListener("DOMContentLoaded", () => {
  // 1. Programmatically generate minimalist flat soundwave bars in Hero visualizer
  const barsContainer = document.querySelector(".wave-bars");
  if (barsContainer) {
    const totalBars = 24;
    for (let i = 0; i < totalBars; i++) {
      const bar = document.createElement("div");
      bar.className = "wave-bar";
      
      // Randomize animation delays and durations for organic movement
      const delay = (Math.random() * 1.5).toFixed(2);
      const duration = (0.6 + Math.random() * 1.0).toFixed(2);
      
      bar.style.animationDelay = `${delay}s`;
      bar.style.animationDuration = `${duration}s`;
      
      barsContainer.appendChild(bar);
    }
  }

  // 2. Interactive Click on central voice capture button
  const waveCenter = document.querySelector(".wave-center");
  if (waveCenter) {
    let recording = false;
    waveCenter.addEventListener("click", () => {
      recording = !recording;
      const bars = document.querySelectorAll(".wave-bar");
      
      if (recording) {
        waveCenter.innerHTML = "⏹️";
        // Boost soundwave frequency & amplitude
        bars.forEach(b => {
          b.style.animationDuration = `${(0.18 + Math.random() * 0.3).toFixed(2)}s`;
          b.style.backgroundColor = "#8b5cf6"; // Highlight active state
        });
      } else {
        waveCenter.innerHTML = "🎙️";
        // Restore calm soundwave states
        bars.forEach((b, idx) => {
          b.style.animationDuration = `${(0.6 + Math.random() * 1.0).toFixed(2)}s`;
          b.style.backgroundColor = "#1a1a1a";
        });
      }
    });
  }

  // 3. Highlight scrolled navbar border details
  const navbar = document.querySelector(".navbar");
  window.addEventListener("scroll", () => {
    if (window.scrollY > 40) {
      navbar.style.background = "rgba(255, 255, 238, 0.95)";
      navbar.style.padding = "16px 6%";
    } else {
      navbar.style.background = "rgba(255, 255, 238, 0.85)";
      navbar.style.padding = "20px 6%";
    }
  });
});
