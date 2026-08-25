# League Ledger

A history site for your Sleeper fantasy football league: all-time standings,
season-by-season results, head-to-head records, and a record book — built
from your league's real data.

It's two pieces:

- **`fetch_data.go`** — a Go program that pulls your league's full history from
  Sleeper and saves it to `league-data.json`. This is the only piece that
  talks to the internet.
- **`league-history.html`** — the website. It just reads `league-data.json`
  and renders everything. No internet connection needed once the JSON exists.

## 1. Install Go (one-time)

If you don't already have it: https://go.dev/dl/ — download the installer
for your OS and run it. Check it worked:

```
go version
```

## 2. Fetch your league's data, and open the site

From this folder:

```
go run fetch_data.go -serve
```

This walks your league's history automatically (it follows Sleeper's season
chain backwards from the league ID below), writes `league-data.json`, and
then starts a local web server for you — all in one command, no Python or
other tools needed. You'll see something like:

```
Serving this folder at http://localhost:8000
Open http://localhost:8000/league-history.html in your browser.
Press Ctrl+C to stop.
```

Open that URL in your browser. When you're done, go back to the terminal
and press **Ctrl+C** to stop the server.

By default it uses this league ID:

```
1384692281021300736
```

If you ever want to point it at a different league, a different port, or a
future season's ID once your league rolls over, pass flags:

```
go run fetch_data.go -serve -league 123456789012345678 -port 8080
```

If you just want to refresh `league-data.json` without opening a server
(e.g. you're serving it another way), drop `-serve`:

```
go run fetch_data.go
```

Re-run with `-serve` any time you want fresh data — it re-fetches everything,
overwrites the file, and reopens the server.

## What it shows

- **Overview** — trophy case by year, all-time standings by win %
- **Seasons** — final standings for every season, plus a weekly power
  ranking table (record blended with points scored relative to league
  average — can rank teams differently than the raw standings do)
- **Head-to-Head** — every team's all-time record against every other team,
  plus a rivalry lookup: pick any two managers to see their full game log
- **Records** — highest/lowest scores, biggest blowouts, closest finishes,
  best/worst seasons, longest win/loss streaks, and a postseason-only
  record book
- **Draft** — pick a season from the dropdown to see that year's draft as a
  proper grid (rounds down the side, draft slots across the top), graded by
  how many fantasy points that player scored the year they were picked. An
  all-time "Draft Tendencies" table above it shows each manager's favorite
  position and favorite NFL team to draft from.
- **Transactions** — pick "All-Time" or a specific season from the dropdown
  for the trades/waiver/free-agent summary table, plus (when a specific
  season is selected) a full transaction log — who traded whom for what,
  and every waiver/free-agent add and drop.
- **Awards** — pick "All-Time" or a specific season from the dropdown: Mr.
  Consistent / Boom or Bust, Bench Tax, Luckiest / Unluckiest, Trade Hawk,
  Waiver Wire Wizard, scoped to whichever you pick.
- **Hall of Shame** — heartbreak losses, backdoor wins, championship games
  the "wrong" team won, missing the playoffs by the slimmest margin, and
  the worst bench-over-starter blunders
- **Teams** — click a team to see their full career history, rivalries,
  luck rating, bench tax, and roster-move counts

## Notes

- Team names and avatars come from whatever your league members set in
  Sleeper. If someone never set a custom team name, their Sleeper display
  name is used instead.
- Regular season vs. playoff weeks are detected from each season's
  `playoff_week_start` setting, and the champion is read from Sleeper's
  playoff bracket data.
- "Expected wins" (used for the Luckiest/Unluckiest awards) compares each
  week's score against the rest of the league that week, not just your
  actual opponent — a top-scoring week "expected" a win regardless of
  matchup luck.
- Draft grading uses total fantasy points the player scored that season,
  wherever those points landed — it's a read on the pick itself, not on
  in-season roster management.
- "Missed the playoffs by a hair" is inferred from who appears in Sleeper's
  playoff bracket data each season; if a season's bracket data is
  incomplete, that season is skipped for this one stat rather than guessing.
- This site doesn't assume dynasty/keeper rules — the Draft tab just shows
  whatever Sleeper has marked as a keeper pick, if any.
- The player-name lookup (`playersById` in league-data.json) is trimmed to
  only the players who actually show up in your league's history, so the
  file stays reasonably small despite Sleeper's full player list being huge.
- If you host this on a static site host (GitHub Pages, Netlify, etc.),
  just upload `league-history.html` and `league-data.json` together — you'd
  re-run `go run fetch_data.go` (without `-serve`) locally and re-upload the
  JSON whenever you want to refresh it.
