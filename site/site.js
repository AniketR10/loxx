// Shared by every page: the mobile menu, elements that rise in as they scroll
// into view, and the tagline that lights up word by word. Pages work without
// it; it only adds motion and the menu button.
(() => {
  window.LOXX_SITE = true;
  const root = document.documentElement;

  // Mobile menu: the button morphs into an X and the links fill the screen.
  const island = document.querySelector('.island');
  const burger = island && island.querySelector('.burger');
  if (burger) {
    const set = (open) => {
      island.classList.toggle('open', open);
      root.classList.toggle('menu-open', open);
      burger.setAttribute('aria-expanded', String(open));
      burger.setAttribute('aria-label', open ? 'Close the menu' : 'Open the menu');
    };
    burger.addEventListener('click', () => set(!island.classList.contains('open')));
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') set(false); });
    for (const a of island.querySelectorAll('.links a')) a.addEventListener('click', () => set(false));
  }

  if (!root.classList.contains('motion')) return;

  // Rise in once, as each element enters the viewport.
  const rise = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      e.target.classList.add('in');
      rise.unobserve(e.target);
    }
  }, { rootMargin: '0px 0px -8% 0px' });
  for (const el of document.querySelectorAll('.reveal')) rise.observe(el);

  // Tagline: each word lights up as it crosses a line 55% down the screen, and
  // dims again if you scroll back above it.
  for (const text of document.querySelectorAll('.tagline-text')) {
    const words = [];
    for (const line of text.querySelectorAll('.line')) {
      const parts = line.textContent.trim().split(/\s+/);
      line.replaceChildren(...parts.flatMap((w, i) => {
        const span = document.createElement('span');
        span.className = 'word';
        span.textContent = w;
        words.push(span);
        return i < parts.length - 1 ? [span, ' '] : [span];
      }));
    }
    text.classList.add('split');
    const light = new IntersectionObserver((entries) => {
      for (const e of entries) {
        const above = e.isIntersecting || e.boundingClientRect.top < e.rootBounds.top;
        e.target.classList.toggle('lit', above);
      }
    }, { rootMargin: '0px 0px -45% 0px' });
    for (const w of words) light.observe(w);
  }
})();
