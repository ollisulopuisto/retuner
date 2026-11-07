# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Calendar Versioning](https://calver.org/).

## [26.11.07.396] - 2026-11-07

### Changed
- Renamed project from Retuner to Viritin.
- Renamed default configuration file from `retuner.ini` to `viritin.ini`, with automatic migration on start.
- Renamed Home Assistant add-on from `retuner` to `viritin`.
- Updated Go module to `viritin` and updated internal package imports.
- Updated endpoints to `/viritin/*` while maintaining backwards compatibility for `/retuner/*` and `/ytuner/*`.
- Updated bookmark host placeholder to `viritinhost` while maintaining backwards-compatible reads for `retunerhost` and `ytunerhost`.
- Updated service definitions, timers, docs, and build workflows.

## [26.08.28.4] - 2026-08-28

### Added
- Radio Browser caching and filtering options.
- Maintenance server shutdown endpoint.
- Home Assistant add-on packaging.

## [26.08.28.3] - 2026-08-28

### Added
- Support for podcasts and RSS feeds.
- Playlist resolution for `.m3u`, `.pls`, `.asx`, and `.xspf`.
- HTTPS relay service.
- Curated country presets.
