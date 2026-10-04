// Checks the home page game (site/game.js): every clue's command must show up
// in the top 4 for the ways a player would plausibly describe it, and the
// daily puzzle must be fair. Run: node tools/gametest.js
const assert = require('node:assert/strict');
const { HISTORY, CLUES, search, today } = require('../site/game.js');

// Two descriptions a player might type for each clue, in clue order.
const TRIES = [
  ['compress documents folder', 'zip my documents'],
  ['restart web server', 'website down fix'],
  ['clear docker images', 'free disk space'],
  ['undo last commit', 'uncommit keep changes'],
  ['what is using port 3000', 'which process port'],
  ['open service in browser', 'access cluster service locally'],
  ['upload website to server', 'deploy built site'],
  ['certificate expiry', 'when does https cert expire'],
  ['set work aside', 'save unfinished changes for later'],
  ['bring fix from another branch', 'apply one commit'],
  ['share folder on network', 'quick local web server'],
  ['run job every night', 'schedule task'],
  ['convert video to mp4', 'shrink recording'],
  ['find big files', 'what is taking disk space'],
  ['add forgotten change to last commit', 'fix last commit'],
  ['shell inside container', 'get into database container'],
  ['new ssh key', 'generate key pair'],
  ['replace staging with production', 'swap text in config file'],
  ['watch api logs live', 'follow logs'],
  ['download list of urls', 'fetch many files parallel'],
];

let failures = 0;
assert.equal(TRIES.length, CLUES.length, 'one TRIES entry per clue');
CLUES.forEach(([clue, answer], k) => {
  assert.ok(HISTORY[answer], `clue ${k} points at a missing command`);
  for (const q of TRIES[k]) {
    const top = search(q);
    if (!top.includes(answer)) {
      failures++;
      console.log(`MISS  "${q}" for: ${clue}\n      want: ${HISTORY[answer][0]}\n      got:  ${top.map((i) => HISTORY[i][0]).join(' | ') || '(nothing)'}`);
    }
  }
});

// Each day: five different clues, the same for everyone, changing day to day.
const days = [0, 1, 2, 30].map((d) => today(new Date(2026, 9, 1 + d)));
assert.equal(days[0].number, 1);
for (const d of days) assert.equal(new Set(d.clues.map((c) => c[1])).size, 5);
assert.deepEqual(today(new Date(2026, 9, 2, 23, 59)), days[1], 'same puzzle all day');
assert.notDeepEqual(days[0].clues, days[1].clues);

console.log(failures ? `${failures} clue description(s) not found` : `all ${CLUES.length * 2} descriptions find their command; daily puzzle OK`);
process.exit(failures ? 1 : 0);
