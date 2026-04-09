# Changelog

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
