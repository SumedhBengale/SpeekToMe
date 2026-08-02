// speekto.me — Premium Interactions & Animation Engine
document.addEventListener("DOMContentLoaded", () => {

  // ═══════════════════════════════════════════
  // 1. SCROLL REVEAL (Intersection Observer)
  // ═══════════════════════════════════════════
  const revealElements = document.querySelectorAll(
    ".reveal, .reveal-left, .reveal-right, .reveal-scale"
  );

  const revealObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add("visible");
          // Once revealed, no need to observe anymore
          revealObserver.unobserve(entry.target);
        }
      });
    },
    {
      threshold: 0.1,
      rootMargin: "0px 0px -60px 0px",
    }
  );

  revealElements.forEach((el) => revealObserver.observe(el));

  // ═══════════════════════════════════════════
  // 2. NAVBAR SCROLL STATE
  // ═══════════════════════════════════════════
  const navbar = document.querySelector(".navbar");
  if (navbar) {
    let lastScroll = 0;
    window.addEventListener("scroll", () => {
      const scrollY = window.scrollY;
      if (scrollY > 50) {
        navbar.classList.add("scrolled");
      } else {
        navbar.classList.remove("scrolled");
      }
      lastScroll = scrollY;
    });
  }

  // Hamburger Mobile Menu Toggle
  const hamburger = document.querySelector(".nav-hamburger");
  const navLinks = document.querySelector(".nav-links");
  if (hamburger) {
    hamburger.addEventListener("click", () => {
      hamburger.classList.toggle("open");
      if (navLinks) navLinks.classList.toggle("active");
    });
  }

  // ═══════════════════════════════════════════
  // 3. NAV AUTH STATE
  // ═══════════════════════════════════════════
  const navBtn = document.querySelector(".nav-btn");
  if (navBtn) {
    const token = localStorage.getItem("speektome_token");
    const navActions = document.createElement("div");
    navActions.className = "nav-actions";

    if (token) {
      navActions.innerHTML = `
        <a href="/dashboard/" class="btn btn-secondary" style="padding: 10px 20px;">Dashboard</a>
        <button id="nav-logout-btn" class="btn btn-primary" style="cursor: pointer; padding: 10px 20px;">Logout</button>
      `;
      navBtn.parentNode.replaceChild(navActions, navBtn);

      const logoutBtn = document.getElementById("nav-logout-btn");
      if (logoutBtn) {
        logoutBtn.addEventListener("click", () => {
          localStorage.removeItem("speektome_token");
          window.location.reload();
        });
      }
    } else {
      navActions.innerHTML = `
        <a href="/dashboard/" class="btn btn-secondary" style="padding: 10px 20px;">Login</a>
        <a href="/dashboard/" class="btn btn-primary" style="padding: 10px 20px;">Get Started</a>
      `;
      navBtn.parentNode.replaceChild(navActions, navBtn);
    }
  }

  // ═══════════════════════════════════════════
  // 4. ANIMATED COUNTERS (count-up on scroll)
  // ═══════════════════════════════════════════
  const counterElements = document.querySelectorAll("[data-count]");

  const counterObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          const el = entry.target;
          const target = parseFloat(el.getAttribute("data-count"));
          const suffix = el.getAttribute("data-suffix") || "";
          const prefix = el.getAttribute("data-prefix") || "";
          const decimals = el.getAttribute("data-decimals")
            ? parseInt(el.getAttribute("data-decimals"))
            : 0;
          const duration = 2000;
          const startTime = performance.now();

          function easeOutExpo(t) {
            return t === 1 ? 1 : 1 - Math.pow(2, -10 * t);
          }

          function updateCounter(currentTime) {
            const elapsed = currentTime - startTime;
            const progress = Math.min(elapsed / duration, 1);
            const easedProgress = easeOutExpo(progress);
            const currentValue = target * easedProgress;

            el.textContent =
              prefix + currentValue.toFixed(decimals) + suffix;

            if (progress < 1) {
              requestAnimationFrame(updateCounter);
            }
          }

          requestAnimationFrame(updateCounter);
          counterObserver.unobserve(el);
        }
      });
    },
    { threshold: 0.5 }
  );

  counterElements.forEach((el) => counterObserver.observe(el));

  // ═══════════════════════════════════════════
  // 5. TYPEWRITER EFFECT
  // ═══════════════════════════════════════════
  const typewriterElements = document.querySelectorAll("[data-typewriter]");

  const typewriterObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          const el = entry.target;
          const text = el.getAttribute("data-typewriter");
          const speed = parseInt(el.getAttribute("data-typewriter-speed")) || 40;
          let index = 0;
          el.textContent = "";

          // Add cursor
          const cursor = document.createElement("span");
          cursor.className = "typewriter-cursor";
          el.parentNode.insertBefore(cursor, el.nextSibling);

          function type() {
            if (index < text.length) {
              el.textContent += text.charAt(index);
              index++;
              setTimeout(type, speed + Math.random() * 30);
            } else {
              // Keep cursor blinking for a bit, then remove
              setTimeout(() => {
                cursor.style.opacity = "0";
                cursor.style.transition = "opacity 0.5s";
              }, 3000);
            }
          }

          setTimeout(type, 300);
          typewriterObserver.unobserve(el);
        }
      });
    },
    { threshold: 0.5 }
  );

  typewriterElements.forEach((el) => typewriterObserver.observe(el));

  // ═══════════════════════════════════════════
  // 6. FAQ ACCORDION
  // ═══════════════════════════════════════════
  const faqItems = document.querySelectorAll(".faq-item");
  faqItems.forEach((item) => {
    const question = item.querySelector(".faq-question");
    if (question) {
      question.addEventListener("click", () => {
        const isActive = item.classList.contains("active");

        // Close all others
        faqItems.forEach((other) => other.classList.remove("active"));

        // Toggle current
        if (!isActive) {
          item.classList.add("active");
        }
      });
    }
  });

  // ═══════════════════════════════════════════
  // 7. MARQUEE DUPLICATION (seamless loop)
  // ═══════════════════════════════════════════
  const marqueeTracks = document.querySelectorAll(".marquee-track");
  marqueeTracks.forEach((track) => {
    const sets = track.querySelectorAll(".marquee-set");
    if (sets.length === 1) {
      // Duplicate for seamless looping
      const clone = sets[0].cloneNode(true);
      clone.setAttribute("aria-hidden", "true");
      track.appendChild(clone);
    }
  });

  // ═══════════════════════════════════════════
  // 8. TESTIMONIAL CAROUSEL
  // ═══════════════════════════════════════════
  const carouselTrack = document.querySelector(".testimonial-track");
  const carouselDots = document.querySelectorAll(".carousel-dot");

  if (carouselTrack && carouselDots.length > 0) {
    let currentSlide = 0;
    const totalSlides = carouselDots.length;

    function goToSlide(index) {
      currentSlide = index;
      carouselTrack.style.transform = `translateX(-${index * 100}%)`;
      carouselDots.forEach((dot, i) => {
        dot.classList.toggle("active", i === index);
      });
    }

    carouselDots.forEach((dot, i) => {
      dot.addEventListener("click", () => goToSlide(i));
    });

    // Auto-advance every 5 seconds
    setInterval(() => {
      goToSlide((currentSlide + 1) % totalSlides);
    }, 5000);
  }

  // ═══════════════════════════════════════════
  // 9. SMOOTH PARALLAX ON HERO ELEMENTS
  // ═══════════════════════════════════════════
  const heroVisualizer = document.querySelector(".hero-visualizer");
  if (heroVisualizer && window.innerWidth > 900) {
    window.addEventListener("scroll", () => {
      const scrollY = window.scrollY;
      if (scrollY < window.innerHeight) {
        heroVisualizer.style.transform = `translateY(${scrollY * 0.08}px)`;
      }
    });
  }

  // ═══════════════════════════════════════════
  // 10. PRICING CALCULATOR SLIDER
  // ═══════════════════════════════════════════
  const calcSlider = document.getElementById("usage-slider");
  const calcResult = document.getElementById("calc-result");
  if (calcSlider && calcResult) {
    calcSlider.addEventListener("input", () => {
      const hours = parseInt(calcSlider.value);
      const perHour = (2 / hours).toFixed(3);
      calcResult.innerHTML = `At <strong>${hours} hours/month</strong>, that's just <span class="highlight">$2.00/month</span> — or <span class="highlight">$${perHour}/hour</span> with Unlimited Pro!`;
    });
  }

  // ═══════════════════════════════════════════
  // 11. PROGRESS BAR ANIMATION
  // ═══════════════════════════════════════════
  const progressFills = document.querySelectorAll(".mockup-progress-fill");
  const progressObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          const el = entry.target;
          const targetWidth = el.getAttribute("data-width") || el.style.width;
          el.style.width = "0%";
          requestAnimationFrame(() => {
            el.style.width = targetWidth;
          });
          progressObserver.unobserve(el);
        }
      });
    },
    { threshold: 0.5 }
  );
  progressFills.forEach((el) => progressObserver.observe(el));

  // ═══════════════════════════════════════════
  // 12. KEYBOARD KEY PULSE ANIMATION
  // ═══════════════════════════════════════════
  const keyVisual = document.querySelector(".key-visual");
  if (keyVisual) {
    setInterval(() => {
      keyVisual.style.transform = "translateY(-4px) scale(1.02)";
      keyVisual.style.boxShadow =
        "0 20px 60px rgba(0,0,0,0.08), 0 0 40px rgba(240, 215, 255, 0.5)";
      setTimeout(() => {
        keyVisual.style.transform = "";
        keyVisual.style.boxShadow = "";
      }, 600);
    }, 3000);
  }

  // ═══════════════════════════════════════════
  // 13. NOISE OVERLAY
  // ═══════════════════════════════════════════
  const noise = document.querySelector(".noise-overlay");
  // Noise overlay is static CSS — no JS needed

  // ═══════════════════════════════════════════
  // 14. STAGGER CHILDREN REVEAL
  // ═══════════════════════════════════════════
  const staggerContainers = document.querySelectorAll("[data-stagger]");
  staggerContainers.forEach((container) => {
    const children = container.children;
    Array.from(children).forEach((child, index) => {
      child.style.transitionDelay = `${index * 0.1}s`;
    });
  });
});
