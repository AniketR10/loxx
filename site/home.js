// Lost Commands, the home page game: five memories a day, found by searching
// a made-up history the way loxx does. The logic and data are in game.js;
// this file runs the board.
(() => {
  const G = LOXX_GAME;
  const $ = (id) => document.getElementById(id);
  const puzzle = G.today();
  const SLOW = 20; // seconds: found within this is green, after it yellow
  const PENALTY = 5; // seconds added for picking the wrong command

  // Today's result lives only in this browser, as a convenience.
  const KEY = 'loxx-lost-commands';
  const load = () => { try { const s = JSON.parse(localStorage.getItem(KEY)); return s && s.day === puzzle.number ? s : { day: puzzle.number }; } catch { return { day: puzzle.number }; } };
  const save = (s) => { try { localStorage.setItem(KEY, JSON.stringify(s)); } catch {} };

  const views = { start: $('v-start'), round: $('v-round'), end: $('v-end') };
  const show = (name) => { for (const [k, v] of Object.entries(views)) v.hidden = k !== name; };
  const input = $('q');
  const rows = $('rows');
  const board = $('board');
  let round = 0;
  let results = [];
  let sel = 0;
  let started = 0;
  let penalty = 0;
  let tick;
  let meaningSoon;
  let outcome = []; // per round: { secs, found }

  const clock = (secs) => `${Math.floor(secs / 60)}:${String(Math.floor(secs % 60)).padStart(2, '0')}`;
  const square = (o) => (!o.found ? '⬛' : o.secs <= SLOW ? '🟩' : '🟨');

  function showToday() {
    const r = load().result;
    $('today').hidden = !r;
    if (r) $('today').textContent = `Today: ${r.squares} ${r.found}/5 in ${clock(r.secs)}`;
    $('start').textContent = r ? 'Play again' : 'Play';
  }

  function startGame() {
    round = 0;
    outcome = [];
    show('round');
    startRound();
  }

  function startRound() {
    $('round').textContent = `Round ${round + 1} / 5`;
    $('clue').textContent = puzzle.clues[round][0];
    $('msg').textContent = '';
    $('msg').className = 'msg';
    input.value = '';
    started = performance.now();
    penalty = 0;
    clearInterval(tick);
    tick = setInterval(() => { $('clock').textContent = `${elapsed().toFixed(1)} s`; }, 100);
    refresh();
    input.focus();
  }
  const elapsed = () => (performance.now() - started) / 1000 + penalty;

  function refresh() {
    sel = 0;
    clearTimeout(meaningSoon);
    // Like loxx: keyword matches at once, matches by meaning a moment later.
    results = G.search(input.value, false);
    draw(true);
    meaningSoon = setTimeout(() => { results = G.search(input.value, true); draw(false); }, 150);
  }

  function span(cls, text) {
    const s = document.createElement('span');
    s.className = cls;
    s.textContent = text;
    return s;
  }
  function draw(pending) {
    rows.replaceChildren(...results.map((i, k) => {
      const [text, dir, hours, runs, exit] = G.HISTORY[i];
      const li = document.createElement('li');
      li.className = k === sel ? 'sel' : '';
      li.setAttribute('role', 'option');
      li.setAttribute('aria-selected', String(k === sel));
      const details = [dir, G.ago(hours)].concat(runs > 1 ? [`${runs}×`] : []).join(' · ');
      li.append(span('t', text), '  ', span('d', details), ' ', exit === 0 ? span('ok', '✓') : span('bad', '✗'));
      li.addEventListener('click', () => { sel = k; pick(); });
      return li;
    }));
    if (!results.length) {
      const li = document.createElement('li');
      li.className = 'none';
      li.textContent = 'no matches';
      rows.append(li);
    }
    $('hint').textContent = `↑↓ choose · Enter use · Esc quit${pending ? ' · matching by meaning…' : ''}`;
  }

  function pick() {
    const i = results[sel];
    if (i === undefined) return;
    if (i === puzzle.clues[round][1]) {
      finishRound(true);
      return;
    }
    penalty += PENALTY;
    $('msg').textContent = `Not that one (+${PENALTY} s)`;
    $('msg').className = 'msg bad';
    board.classList.remove('shake');
    void board.offsetWidth; // restart the animation
    board.classList.add('shake');
  }

  // Leave mid-game: nothing is saved, back to the start.
  let next;
  function quit() {
    clearInterval(tick);
    clearTimeout(meaningSoon);
    clearTimeout(next);
    show('start');
    $('start').focus();
  }

  function finishRound(found) {
    clearInterval(tick);
    const secs = elapsed();
    outcome.push({ secs, found });
    $('msg').textContent = found ? `Found it in ${secs.toFixed(1)} s` : 'Skipped';
    $('msg').className = found ? 'msg good' : 'msg bad';
    next = setTimeout(() => {
      round++;
      if (round < puzzle.clues.length) startRound();
      else endGame();
    }, found ? 900 : 400);
  }

  function endGame() {
    const secs = outcome.reduce((t, o) => t + o.secs, 0);
    const found = outcome.filter((o) => o.found).length;
    const squares = outcome.map(square).join('');
    const s = load();
    s.result = { squares, secs, found };
    save(s);
    $('squares').textContent = squares;
    $('summary').textContent = `${found} of 5 found in ${clock(secs)}.`;
    $('answers').replaceChildren(...puzzle.clues.map(([clue, i], k) => {
      const li = document.createElement('li');
      li.append(clue, document.createElement('br'), Object.assign(document.createElement('code'), { textContent: G.HISTORY[i][0] }));
      if (!outcome[k].found) li.append(' ', span('gave', 'skipped'));
      return li;
    }));
    show('end');
    showToday();
  }

  input.addEventListener('input', refresh);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { pick(); e.preventDefault(); return; }
    if (e.key === 'Escape') { quit(); return; }
    if ((e.key !== 'ArrowDown' && e.key !== 'ArrowUp') || !results.length) return;
    e.preventDefault();
    sel = (sel + (e.key === 'ArrowDown' ? 1 : -1) + results.length) % results.length;
    draw(false);
  });
  $('start').addEventListener('click', startGame);
  $('skip').addEventListener('click', () => finishRound(false));
  $('quit').addEventListener('click', quit);
  $('share').addEventListener('click', async (e) => {
    const r = load().result;
    const text = `loxx · Lost Commands\n${r.squares} ${r.found}/5 in ${clock(r.secs)}\nhttps://loxx.run/#play`;
    try {
      await navigator.clipboard.writeText(text);
      e.target.textContent = 'Copied';
    } catch {
      $('share-text').textContent = text; // copying was blocked: show it to copy by hand
      $('share-text').hidden = false;
      e.target.textContent = 'Select the text below';
    }
    setTimeout(() => { e.target.textContent = 'Copy my result'; }, 1600);
  });

  showToday();
  show('start');
})();
