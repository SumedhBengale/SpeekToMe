import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';

export default function ScrollToTop() {
  const { pathname, search, hash } = useLocation();

  const scrollToTop = () => {
    window.scrollTo(0, 0);
    if (document.documentElement) document.documentElement.scrollTop = 0;
    if (document.body) document.body.scrollTop = 0;
    
    // Also reset any scrollable container wrappers
    const containers = document.querySelectorAll('.dashboard-container, .home-page, .about-page, .pricing-page, #root, body, html');
    containers.forEach((el) => {
      if (el && el.scrollTop !== undefined) {
        el.scrollTop = 0;
      }
    });
  };

  // 1. Scroll to top whenever route location changes
  useEffect(() => {
    scrollToTop();
  }, [pathname, search, hash]);

  // 2. Also listen for global link/button clicks to handle clicking links to the SAME page
  useEffect(() => {
    const handleGlobalClick = (e) => {
      const clickable = e.target.closest('a, button, .nav-item, .footer-links-group a');
      if (clickable) {
        scrollToTop();
        setTimeout(scrollToTop, 50);
        setTimeout(scrollToTop, 150);
      }
    };

    document.addEventListener('click', handleGlobalClick);
    return () => document.removeEventListener('click', handleGlobalClick);
  }, []);

  return null;
}
