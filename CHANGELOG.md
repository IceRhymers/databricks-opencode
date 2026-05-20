# Changelog

## [1.0.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.7.0...v1.0.0) (2026-05-20)


### ⚠ BREAKING CHANGES

* serve subcommand (consolidate --headless / --idle-timeout) ([#84](https://github.com/IceRhymers/databricks-opencode/issues/84))
* legacy root flags removed:
    - --install-hooks    → hooks install
    - --uninstall-hooks  → hooks uninstall
    - --headless-ensure  → hooks session-start
* `--print-env` root flag removed. Use `config show` instead.

### Features

* CLI command-tree restructure (integration branch) ([f5bdb7b](https://github.com/IceRhymers/databricks-opencode/commit/f5bdb7b27124884d5d7ac07a083d4d7f828c2f92))
* command-tree registry foundation + config show ([#82](https://github.com/IceRhymers/databricks-opencode/issues/82)) ([f0667ab](https://github.com/IceRhymers/databricks-opencode/commit/f0667ab77c1775b4e6814eefed2edf9bdf5102a6))
* hooks subcommand (install/uninstall/session-start) ([#83](https://github.com/IceRhymers/databricks-opencode/issues/83)) ([0d1b259](https://github.com/IceRhymers/databricks-opencode/commit/0d1b2596c897fc9bb13dd7f29a05ae3989565adb))
* serve subcommand (consolidate --headless / --idle-timeout) ([#84](https://github.com/IceRhymers/databricks-opencode/issues/84)) ([df1adf9](https://github.com/IceRhymers/databricks-opencode/commit/df1adf9989262c5513030c0799812cdad3662f6e))

## [0.7.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.6.0...v0.7.0) (2026-05-07)


### Features

* parseArgs returns (*Args, error) ([bdece27](https://github.com/IceRhymers/databricks-opencode/commit/bdece27238d989b023f8687ea25c365e3e50f75f)), closes [#79](https://github.com/IceRhymers/databricks-opencode/issues/79)
* phase 2 — strict --idle-timeout + broadened redaction ([6f0cf11](https://github.com/IceRhymers/databricks-opencode/commit/6f0cf11a8ef94e7fb26b848506d704b5807c6907))


### Bug Fixes

* strict --idle-timeout parse and broadened token redaction ([9c7b973](https://github.com/IceRhymers/databricks-opencode/commit/9c7b9737c0712665df4734c98a0e18a7f924fd67)), closes [#72](https://github.com/IceRhymers/databricks-opencode/issues/72) [#73](https://github.com/IceRhymers/databricks-opencode/issues/73)
* surface headless.Ensure error from headlessEnsure ([a980e33](https://github.com/IceRhymers/databricks-opencode/commit/a980e337117a32511fb6480455af773a05cd8742))

## [0.6.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.5.0...v0.6.0) (2026-05-04)


### Features

* add databricks-claude-opus-4-7 and make it the primary default ([6d4bc86](https://github.com/IceRhymers/databricks-opencode/commit/6d4bc864d88e6df93a6aa18e7b553b7bb086daff)), closes [#64](https://github.com/IceRhymers/databricks-opencode/issues/64)
* simplify `ConstructGatewayURL`: use host-relative AI Gateway path (`{host}/ai-gateway/anthropic`), removing SCIM workspace-ID lookup, token parameter, and fallback ([#69](https://github.com/IceRhymers/databricks-opencode/issues/69)) ([3d1d55a](https://github.com/IceRhymers/databricks-opencode/commit/3d1d55afbb6949d07ad52a33f196c4126cb740d7))
* require conventional commit prefix in agent instructions ([2870b28](https://github.com/IceRhymers/databricks-opencode/commit/2870b28d6c014cfe329866414c3e1fd498e945ea))

## [0.5.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.4.1...v0.5.0) (2026-04-10)


### Features

* add update subcommand and startup update check ([f6cdffb](https://github.com/IceRhymers/databricks-opencode/commit/f6cdffbf7ad0f03d4f29bf7fc97a5371ac045983))
* add update subcommand and startup update check ([b9131bc](https://github.com/IceRhymers/databricks-opencode/commit/b9131bceb848c73098985770c4fbe2ff297e3b46))
* add update subcommand and startup update check ([d0e4f0e](https://github.com/IceRhymers/databricks-opencode/commit/d0e4f0e59c6cff5101c9d86cc1cddf191ac1edc1)), closes [#52](https://github.com/IceRhymers/databricks-opencode/issues/52)
* bump databricks-claude to v0.12.0 for update subcommand and startup check ([12ea3f0](https://github.com/IceRhymers/databricks-opencode/commit/12ea3f0f7e9b063e81e3c1f4e693d0b679bba2ed))


### Bug Fixes

* make headlessEnsure TLS-aware to prevent health check failures with HTTPS ([68d07cd](https://github.com/IceRhymers/databricks-opencode/commit/68d07cdc6c6807bba8d14c5aee441177c530594b)), closes [#55](https://github.com/IceRhymers/databricks-opencode/issues/55)
* use OS-specific opencode config dir on Windows and macOS ([31c71d4](https://github.com/IceRhymers/databricks-opencode/commit/31c71d47f7d27c5bc1e46277b7958b8bf0596605))
* use OS-specific opencode config dir on Windows and macOS ([17a3d41](https://github.com/IceRhymers/databricks-opencode/commit/17a3d4194e8975e2bbc76844731d78128fd5b4d8))
* use xdg-basedir convention (XDG_CONFIG_HOME or ~/.config) on all platforms ([1575e33](https://github.com/IceRhymers/databricks-opencode/commit/1575e332837d943e42c238d14b4875edc24b813b))

## [0.4.1](https://github.com/IceRhymers/databricks-opencode/compare/v0.4.0...v0.4.1) (2026-04-09)


### Bug Fixes

* bump databricks-claude to v0.10.1 for completion --shell= support ([ec74927](https://github.com/IceRhymers/databricks-opencode/commit/ec74927d6b2b9b0e694cc4b58e1637065e3cb171))
* bump databricks-claude to v0.10.1 to fix completion --shell= flag ([bbfb232](https://github.com/IceRhymers/databricks-opencode/commit/bbfb2320e0a57decc0d921767dbcafd038276ef3))

## [0.4.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.3.0...v0.4.0) (2026-04-09)


### Features

* add POST /shutdown endpoint and idle timeout for headless mode ([#40](https://github.com/IceRhymers/databricks-opencode/issues/40)) ([12011bf](https://github.com/IceRhymers/databricks-opencode/commit/12011bf86b0cd7c829de97135458cbe443c0f29e))
* add shell tab completions (bash/zsh/fish) ([fd3e7ee](https://github.com/IceRhymers/databricks-opencode/commit/fd3e7ee01048bfe235ab4e2454b37fd3176c60f4)), closes [#48](https://github.com/IceRhymers/databricks-opencode/issues/48)
* POST /shutdown + idle timeout for headless mode ([8207c11](https://github.com/IceRhymers/databricks-opencode/commit/8207c1157534196e54c83fcc83b4eddc4416aa05))
* shell tab completions (bash/zsh/fish) ([dfd396f](https://github.com/IceRhymers/databricks-opencode/commit/dfd396f7645cfab09b4d51b9e8e339f240a91e8e))


### Bug Fixes

* change default port from 49155 to 49156 ([60f0f57](https://github.com/IceRhymers/databricks-opencode/commit/60f0f57329d925ce54b6b470c20fc7b73c485d1f)), closes [#42](https://github.com/IceRhymers/databricks-opencode/issues/42)
* OpenCode plugin hooks — ESM format, surgical config, absolute path ([e6de5d9](https://github.com/IceRhymers/databricks-opencode/commit/e6de5d9f4152e064fc88919549c9749843d5790d))
* retrigger homebrew dispatch for v0.2.x ([76dc8e2](https://github.com/IceRhymers/databricks-opencode/commit/76dc8e279ae1c1b6135f8c018e2c832f25b4c468))

## [0.3.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.2.0...v0.3.0) (2026-04-07)


### Features

* dispatch Homebrew formula update on release ([e416cfb](https://github.com/IceRhymers/databricks-opencode/commit/e416cfbc03f8b03943dfaa493f8f9a4ce3293024))
* dispatch Homebrew formula update on release ([476ec2f](https://github.com/IceRhymers/databricks-opencode/commit/476ec2f6b2936976ded55bd636e2aa22a8db0b78))


### Bug Fixes

* correct YAML syntax in release.yml ([ff6defe](https://github.com/IceRhymers/databricks-opencode/commit/ff6defe8a1c58a9c385f4f8d9334486daba6137d))
* correct YAML syntax in release.yml (missing newline before update-homebrew job) ([0c6b2a0](https://github.com/IceRhymers/databricks-opencode/commit/0c6b2a0737e90ac223bfe49c4db4a5acee663b3a))

## [0.2.0](https://github.com/IceRhymers/databricks-opencode/compare/v0.1.2...v0.2.0) (2026-04-07)


### Features

* add --headless flag for proxy-only startup ([57d956b](https://github.com/IceRhymers/databricks-opencode/commit/57d956b118fe8885573feed30f54f87ab8ae6067)), closes [#21](https://github.com/IceRhymers/databricks-opencode/issues/21)


### Bug Fixes

* write databricks-proxy api_key directly into config.json for headless mode ([7ea17c5](https://github.com/IceRhymers/databricks-opencode/commit/7ea17c580e5f6e0dd486cce89af1f013e2e7f491))
* write databricks-proxy api_key directly into config.json for headless mode ([82b349b](https://github.com/IceRhymers/databricks-opencode/commit/82b349ba854451bc68ac58c40741f45c759dcc33))
