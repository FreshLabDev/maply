# Changelog

All notable maply changes are documented here.

The `## <tag>` section of this file *is* the GitHub Release body: the release
workflow copies it verbatim and refuses a tag that has no section. Write it
for whoever has to decide whether to upgrade.

See [`docs/versioning.md`](docs/versioning.md) for what the numbers mean and
[`docs/releases.md`](docs/releases.md) for how a release is published.

## Unreleased

Nothing is released yet. The repository holds the design, the MVP plan, the
source measurements and the clickable prototype of the Mini App.

### Added

- `docs/design.md`: the daily cycle, scoring, anti-cheat rules, the intrigue
  window, groups, the site, where places come from and how they are picked.
- `docs/mvp-plan.md`: what is in the MVP, where it departs from the design,
  stages with a check for each, risks and open questions.
- `web/prototype/`: the Mini App as one HTML file. A panorama or photo on top, a
  globe that opens into a map, a pin, the waiting screen and the day's result
  with a leaderboard. Inside Telegram it uses the Mini App API: brand colours
  for the header, background and bottom bar, vertical swipes off, the system
  Back button from the map to the photo, haptics, a closing confirmation while
  a pin is placed but not sent, safe areas, and sharing through `t.me/share`.
- `scripts/sources/`: coverage measurements of Panoramax and Mapillary at 68
  points on every continent.
- `scripts/curation/`: candidate harvest from Mapillary vector tiles with
  weighted regions.
