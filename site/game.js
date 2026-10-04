// Lost Commands: the logic of the home page game, kept apart from the page so
// tools/gametest.js can check every clue is findable. A made-up history, a
// search that works like loxx's, and today's puzzle.
const LOXX_GAME = (() => {
  // [command, folder, hours since it last ran, runs, exit code, what it does]
  const HISTORY = [
    ['git reset --soft HEAD~1', '~/code/shop', 70, 2, 0, 'undo last commit keep changes uncommit take back mistake early'],
    ['git revert HEAD', '~/code/api', 340, 1, 0, 'undo commit safely reverse cancel pushed'],
    ['git stash push -m "wip"', '~/code/shop', 26, 4, 0, 'save set aside changes later shelve hide stash temporary work progress unfinished half done'],
    ['git cherry-pick 4f2a91c', '~/code/api', 700, 1, 0, 'apply bring copy single one commit fix from another other branch pick'],
    ['git log --oneline --graph -20', '~/code/shop', 2, 9, 0, 'show history commits list tree recent'],
    ['sudo systemctl restart nginx', '~/srv', 50, 14, 0, 'restart reload web server service website site down back life kick fix'],
    ['sudo nginx -t', '~/srv', 50, 5, 0, 'test check web server config syntax validate'],
    ['docker compose up -d --build', '~/code/shop', 5, 22, 0, 'start run launch containers background rebuild app'],
    ['docker system prune -af', '~', 500, 1, 0, 'free clear clean disk space remove delete old images containers docker cleanup storage full'],
    ['du -sh * | sort -h', '~/Downloads', 160, 3, 0, 'find big biggest large huge files folders size disk usage space taking eating'],
    ['df -h', '~', 96, 6, 0, 'disk space free full drive left'],
    ['kubectl port-forward svc/web 8080:80', '~/code/shop', 140, 7, 0, 'expose open access reach cluster service locally local browser tunnel port'],
    ['kubectl logs -f deploy/api --tail=100', '~/code/api', 24, 11, 0, 'follow watch tail logs live api errors output crash crashing'],
    ['tar -czvf backup.tar.gz ~/Documents', '~', 330, 1, 0, 'compress zip archive pack squeeze shrink folder documents backup single one file'],
    ['rsync -avz ./dist/ deploy@web1:/var/www/', '~/code/site', 72, 8, 0, 'copy upload deploy publish push built files website site server sync'],
    ['ssh -L 5432:localhost:5432 db1', '~', 170, 3, 0, 'tunnel connect remote database forward port'],
    ['psql -h localhost -U app shop_dev', '~/code/shop', 165, 5, 2, 'connect open login database postgres sql'],
    ['lsof -i :3000', '~/code/api', 48, 4, 0, 'which what who process using sitting busy occupied port find'],
    ['kill -9 48213', '~/code/api', 48, 1, 0, 'stop kill end process force'],
    ['find . -name "*.log" -mtime +7 -delete', '~/srv/logs', 720, 2, 0, 'delete remove clean old log files week'],
    ['grep -rn "TODO" src/', '~/code/api', 4, 3, 0, 'search find text code todo notes files'],
    ['openssl x509 -in cert.pem -noout -dates', '~/srv', 1400, 1, 0, 'check when certificate cert expire expiry expires ssl tls https date'],
    ['ffmpeg -i talk.mov -vcodec libx264 talk.mp4', '~/Videos', 500, 1, 0, 'convert shrink compress video recording talk mp4 smaller'],
    ['python3 -m http.server 8000', '~/code/site', 1, 12, 0, 'serve share folder files local network quick preview web server'],
    ['npm run build', '~/code/site', 1, 31, 1, 'build compile frontend project bundle'],
    ['chmod +x deploy.sh', '~/code/site', 75, 1, 0, 'make script executable permission allow'],
    ['crontab -e', '~', 1500, 2, 0, 'schedule job task run every night nightly day automatically cron'],
    ['git branch -D feature/old-login', '~/code/shop', 200, 1, 0, 'delete remove old local branch cleanup'],
    ['git commit --amend --no-edit', '~/code/shop', 30, 6, 0, 'fix add forgot forgotten change slip last commit amend'],
    ['git checkout -- package-lock.json', '~/code/site', 90, 2, 0, 'discard throw away undo changes file restore'],
    ['docker exec -it shop-db-1 bash', '~/code/shop', 120, 4, 0, 'shell inside get into enter container database'],
    ['ssh-keygen -t ed25519 -C "laptop"', '~', 2000, 1, 0, 'create make new fresh ssh key generate pair laptop'],
    ['sudo apt update && sudo apt upgrade -y', '~', 220, 5, 0, 'update upgrade all packages system software'],
    ['curl -sI https://shop.example.com', '~', 30, 6, 0, 'check website headers status response alive http'],
    ['watch -n 2 kubectl get pods', '~/code/api', 25, 3, 0, 'watch monitor pods status refresh every seconds'],
    ["sed -i 's/staging/production/g' config.yml", '~/code/api', 260, 1, 0, 'replace swap change rename every all text word config file'],
    ['ls -la ~/.ssh', '~', 2000, 2, 0, 'list show see hidden files ssh folder permissions'],
    ["jq '.items[].name' resp.json", '~/code/api', 10, 3, 0, 'extract pull names json parse filter field'],
    ['xargs -P 8 -n 1 curl -O < urls.txt', '~/Downloads', 900, 1, 0, 'download fetch many list urls files parallel eight time batch'],
    ['npx prettier --write "src/**/*.ts"', '~/code/site', 3, 7, 0, 'format tidy clean code style files'],
  ];

  // A memory of what you did, and the command it was.
  const CLUES = [
    ['Before a trip, you squeezed your Documents folder into a single file.', 13],
    ['The website was down, and you kicked the web server back to life.', 5],
    ['The disk was nearly full, so you cleared out old Docker images.', 8],
    ['You committed too early and took it back, keeping your changes.', 0],
    ['Something was already sitting on port 3000, and you found out what.', 17],
    ["You opened the cluster's web service in your own browser.", 11],
    ['You copied the built website up to the server.', 14],
    ['You checked when the HTTPS certificate expires.', 21],
    ['You set your half-done work aside to fix a bug first.', 2],
    ['You brought one fix over from another branch.', 3],
    ['You shared a folder on your network for a quick preview.', 23],
    ['A job had to run every night, all by itself.', 26],
    ['You shrank a talk recording into an mp4.', 22],
    ['Downloads was huge, so you looked for what was taking the space.', 9],
    ['You forgot one change in your last commit and slipped it in.', 28],
    ['You got a shell inside the database container.', 30],
    ['You made a fresh SSH key for a new laptop.', 31],
    ["You swapped every 'staging' for 'production' in a config file.", 35],
    ["You watched the API's logs live while it was crashing.", 12],
    ['You downloaded a whole list of URLs, eight at a time.', 38],
  ];

  const SKIP = new Set('a an the my me i to of for in on and how do does is it was with what which that this from all some get up you your one into by at'.split(' '));
  const stem = (w) => w.slice(0, 5);
  const wordsOf = (q) => q.toLowerCase().split(/[^a-z0-9.:~/_-]+/).filter((w) => w.length > 1 && !SKIP.has(w));

  // Like loxx: commands containing all the words first, then the closest in
  // meaning (here: a hand-written word list per command; the real loxx uses a
  // language model). The most recent wins a tie. Up to `limit` results.
  function search(q, withMeaning = true, limit = 4) {
    const words = wordsOf(q);
    const byAge = HISTORY.map((h, i) => i).sort((a, b) => HISTORY[a][2] - HISTORY[b][2]);
    if (!words.length) return byAge.slice(0, limit);
    return byAge.map((i) => {
      const text = HISTORY[i][0].toLowerCase();
      const tags = HISTORY[i][5].split(' ');
      const keyword = words.every((w) => text.includes(w));
      const close = withMeaning ? words.filter((w) => tags.some((t) => stem(t) === stem(w))).length : 0;
      return { i, score: (keyword ? 100 : 0) + close };
    }).filter((s) => s.score > 0).sort((a, b) => b.score - a.score).slice(0, limit).map((s) => s.i);
  }


  function ago(hours) {
    if (hours < 1) return 'just now';
    for (const [size, name] of [[8760, 'year'], [720, 'month'], [168, 'week'], [24, 'day'], [1, 'hour']]) {
      const n = Math.floor(hours / size);
      if (n >= 1) return n === 1 ? `1 ${name} ago` : `${n} ${name}s ago`;
    }
    return 'just now';
  }

  // Today's puzzle: its number, and five clues picked by a seeded shuffle, so
  // everyone gets the same five on the same day.
  const FIRST_DAY = Date.UTC(2026, 9, 1);
  function today(date = new Date()) {
    const day = Date.UTC(date.getFullYear(), date.getMonth(), date.getDate());
    const number = Math.floor((day - FIRST_DAY) / 864e5) + 1;
    let seed = number * 2654435761 >>> 0;
    const rand = () => {
      seed = (seed + 0x6d2b79f5) >>> 0;
      let t = seed;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
    const order = CLUES.map((c, i) => i);
    for (let i = order.length - 1; i > 0; i--) {
      const j = Math.floor(rand() * (i + 1));
      [order[i], order[j]] = [order[j], order[i]];
    }
    return { number, clues: order.slice(0, 5).map((i) => CLUES[i]) };
  }

  return { HISTORY, CLUES, search, ago, today };
})();
if (typeof module !== 'undefined') module.exports = LOXX_GAME;
