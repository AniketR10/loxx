// Home page terminal: types the search, then shows the results one by one,
// the way the real list fills in. Without it, the finished search is shown.
(() => {
  const term = document.getElementById('hero-term');
  if (!term || !document.documentElement.classList.contains('motion')) return;
  const typed = term.querySelector('.typed');
  const later = [...term.querySelectorAll('.rows li, .keys, .after')];
  const text = typed.textContent;
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));

  typed.textContent = '';
  for (const el of later) el.classList.add('wait');

  const play = async () => {
    await wait(900);
    for (const ch of text) {
      typed.textContent += ch;
      await wait(ch === ' ' ? 110 : 60);
    }
    for (const el of later) {
      await wait(el.classList.contains('after') ? 700 : 140);
      el.classList.remove('wait');
    }
  };
  // Start once the terminal is on screen.
  const io = new IntersectionObserver((entries) => {
    if (!entries.some((e) => e.isIntersecting)) return;
    io.disconnect();
    play();
  });
  io.observe(term);
})();
