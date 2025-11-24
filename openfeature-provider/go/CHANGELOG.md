# Changelog

## [0.2.0](https://github.com/spotify/confidence-resolver/compare/openfeature-provider/go/v0.1.0...openfeature-provider/go/v0.2.0) (2025-11-24)


### Features

* **openfeature-provider/go:** add slog-based logging ([#134](https://github.com/spotify/confidence-resolver/issues/134)) ([10362d9](https://github.com/spotify/confidence-resolver/commit/10362d9d6d80f25e63e12ba8c6257eb7e996a2c2))
* Request per second in TelemetryData ([#150](https://github.com/spotify/confidence-resolver/issues/150)) ([b91669d](https://github.com/spotify/confidence-resolver/commit/b91669d75caa0971ab71d0589634ab039dae6081))
* size limited flush api  ([#149](https://github.com/spotify/confidence-resolver/issues/149)) ([6ac60d6](https://github.com/spotify/confidence-resolver/commit/6ac60d6195421c9355941e4201993b521c831fcd))


### Bug Fixes

* **openfeature-provider/go:** move initialize work to provider.init ([#142](https://github.com/spotify/confidence-resolver/issues/142)) ([e1ef08a](https://github.com/spotify/confidence-resolver/commit/e1ef08a992fb980449ea267c7855eae396fe9e7e))


### Dependencies

* The following workspace dependencies were updated
  * dependencies
    * rust-guest bumped from 0.1.9 to 0.1.10

## [0.1.0](https://github.com/spotify/confidence-resolver/compare/openfeature-provider/go/v0.0.1...openfeature-provider/go/v0.1.0) (2025-11-11)


### ⚠ BREAKING CHANGES

* **go:** connection factory replaces server addr options ([#128](https://github.com/spotify/confidence-resolver/issues/128))
* **go:** correct module structure to match declared module path ([#106](https://github.com/spotify/confidence-resolver/issues/106))

### Features

* add go provider ([#100](https://github.com/spotify/confidence-resolver/issues/100)) ([5c0895b](https://github.com/spotify/confidence-resolver/commit/5c0895bd35edd7daf436be5a64b5a40ba3eb7dab))
* **go:** connection factory replaces server addr options ([#128](https://github.com/spotify/confidence-resolver/issues/128)) ([cd955a2](https://github.com/spotify/confidence-resolver/commit/cd955a22917c3572446cdc55491b1cd8b304763a))


### Bug Fixes

* **go:** Better error messaging for sticky rules ([#110](https://github.com/spotify/confidence-resolver/issues/110)) ([31a6893](https://github.com/spotify/confidence-resolver/commit/31a6893bb83c36abc2f8386912dcf316ee454e5a))
* **go:** correct module structure to match declared module path ([#106](https://github.com/spotify/confidence-resolver/issues/106)) ([c2eb597](https://github.com/spotify/confidence-resolver/commit/c2eb597d1c696bd1fac4459866b258ce852dbf9a))
* **go:** implement StateHandler for proper shutdown ([#109](https://github.com/spotify/confidence-resolver/issues/109)) ([6041e45](https://github.com/spotify/confidence-resolver/commit/6041e455c80ef24e9ac50c4881ce17ff40bee871))
* **openfeature/go:** fix openfeature reason mapping ([#121](https://github.com/spotify/confidence-resolver/issues/121)) ([c0334c5](https://github.com/spotify/confidence-resolver/commit/c0334c518af5eb294e8583b46e333864a1796507))

## Changelog
