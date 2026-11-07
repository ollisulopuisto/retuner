# Country presets

A preset is a short, curated station list for one country. Viritin fetches the
file for each country you configure and merges it into the "My stations" menu,
so a new install has something worth listening to before anyone edits a config
file.

```
presets/fi.ini      ->  https://raw.githubusercontent.com/ollisulopuisto/viritin/master/presets/fi.ini
```

The file name is the ISO 3166-1 alpha-2 country code, lowercased. The format is
exactly `stations.ini`, so anything you can put in your own station list works
here:

```ini
[National]
Some National Station=https://stream.example/live.mp3|https://example/logo.png

[Regional]
Some Regional Station=https://stream.example/regional.mp3
```

## What belongs in one

**A broadcaster's own published stream, and not much else.** Warner Music & Sony
Music v TuneIn [2021] EWCA Civ 441 turned on who is making the communication to
the public. A geo-scoped list pointing at streams a broadcaster already
publishes for its own audience keeps this a convenience for listeners. A list
that aggregates other people's catalogues, or that re-points streams through
somewhere they were not meant to go, does not.

Concretely:

- **Yes**: the national broadcaster's radio channels, commercial stations that
  publish a direct stream, community stations that want to be found.
- **No**: streams lifted from an aggregator, streams behind a login, streams a
  broadcaster restricts by geography (they will fail for most listeners
  anyway), and anything you cannot point at a public page for.
- Keep it short. Twenty good stations beat two hundred, because the person
  browsing has a jog dial and a two-line display.

## Adding or updating one

Generate a starting point, which connects to every candidate and keeps only the
ones that actually serve audio:

```sh
./script/make-preset.py --country fi --out presets/fi.ini
```

Stations that radio-browser has no favicon for — most of them — get their logo
from their own homepage: a `<link rel="icon">`, an `apple-touch-icon` where
there is one (a 16-pixel favicon scaled up to 200 is a smear), or
`/favicon.ico` as the last guess. What the page points at is fetched and
checked before it is believed, since a link can just as easily lead to a login
page or four megabytes of something calling itself a PNG. `--no-icons` turns
that off and leaves the logos to the directory.

Then **edit it**. The generator is good at proving a URL plays and bad at
knowing whether a station belongs in a national preset — it sorts by
click-count and groups by whatever tag radio-browser happens to carry. Rename
the categories to something a person would recognise, drop the noise, and put
the broadcaster's own channels first.

If you cannot reach radio-browser from where you are, the
`Build a country preset` workflow does the same thing on a GitHub runner:
Actions → Build a country preset → Run workflow → enter a country code. It
opens a pull request with the file, which is somewhere to read it and edit it
before it becomes real — the whole file is in the description, so a phone is
enough to tell whether it is worth keeping. The artifact is still attached to
the run for anyone who would rather download it, and `Open a pull request with
the result` can be turned off.

It never commits to master, for the reason the next paragraph gives.

One repository setting has to be on for the pull request to be opened for you:
Settings → Actions → General → Workflow permissions → *Allow GitHub Actions to
create and approve pull requests*. Without it the run still generates the file
and pushes the branch, and leaves a warning with a link to open the pull
request in one click — the work is done either way.

To re-check a file that has been sitting for a while and drop what has gone
dead:

```sh
./script/make-preset.py --prune presets/fi.ini
```

## The weekly re-check

`.github/workflows/preset-refresh.yml` runs every Monday, connects to every
stream in every file here, and opens a pull request if anything changed. It
never commits to master: what belongs in a national preset is the judgement
this whole file is about, and a stream can stop answering for reasons that have
nothing to do with the station being gone.

That last point is why the weekly run does not use plain `--prune`, which drops
a station the first time it fails to answer. One CDN hiccup at 04:00 on a
Sunday would otherwise delete a national broadcaster with nobody watching.
Instead:

```sh
./script/make-preset.py --prune presets/fi.ini --strikes presets/strikes.json
```

A failure is recorded rather than acted on, and a station is dropped only after
three consecutive weekly failures (`--strike-limit`). A run in which it plays
clears its record, so three failures spread over a year never add up to a
removal; the record is held against the URL, so fixing a broken link by hand
does not inherit the old one's strikes.

`presets/strikes.json` is that record, and is carried in the same pull request.
It is rebuilt from scratch on each run, so a station you delete by hand leaves
nothing behind. Nothing reads it at runtime — Viritin never sees it.

In the pull request, `warn` lines are a watchlist and `drop` lines are
removals. Check a dropped station before merging: a stream can refuse a
datacentre in particular, and a broadcaster that has moved its URL wants the
new one rather than deleting the entry.

## How Viritin uses them

Set the countries in the add-on's configuration, or in `viritin.ini`:

```ini
[Presets]
Enable=1
PresetsCountries=fi,se
```

At startup Viritin fetches each file, validates that it parses and contains at
least one http(s) URL, and only then replaces its cached copy in
`config/presets/`. A fetch that fails leaves the previous copy in place, so a
network problem costs you yesterday's list rather than the whole menu. Your own
`stations.ini` is always loaded first and always wins: a category that appears
in both is merged, not duplicated, and a station you already have is not added
twice.
