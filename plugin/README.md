# harness-migrate as a Harness unified CLI plugin

This is a second front-end for the same engine the standalone `harness-migrate`
binary drives. It is a [Harness unified CLI](https://github.com/harness/cli)
plugin: a separate binary the `harness` host execs, with its command grammar
declared in [migrate.spec.yaml](pkg/migrateplugin/migrate.spec.yaml).

Nothing under `../internal` or `../types` changes. The handlers here call the
same constructors the kingpin `run()` bodies in `../cmd` call — this adds a
caller, it does not replace one.

## Layout

| Path | Role |
| --- | --- |
| `cmd/harness-migrate/main-harness-migrate.go` | plugin entrypoint (`--identity`, `--spec`, dispatch) |
| `pkg/migrateplugin/migrate.spec.yaml` | the grammar: nouns, commands, flags |
| `pkg/migrateplugin/migrateplugin.go` | `ModuleInit` — binds workflow IDs to handlers |
| `pkg/migrateplugin/gitexport_github.go` | github `git_export` handler |
| `pkg/migrateplugin/gitexport_gitlab.go` | gitlab `git_export` handler |
| `pkg/migrateplugin/client.go` | shared bearer-token `scm.Client` transport |
| `pkg/bridge/` | shared glue: tracer, interrupt handling — reused by every handler |

Its own Go module, because the parent is on Go 1.23 and `github.com/harness/cli`
requires 1.26. `github.com/harness/cli` is consumed from a sibling `../../squash-cli`
checkout for now; it is publicly go-gettable, so that `replace` can become a
plain version requirement later.

## Build and install

Plugin builds use [Task](https://taskfile.dev), from [../Taskfile.yml](../Taskfile.yml).
The root `Makefile` and `.drone.yml` build the standalone binary and are
deliberately untouched — that front-end is being deprecated, so the two build
systems share nothing.

```sh
task build      # -> plugin/bin/harness-migrate
task install    # harness install plugin plugin/bin/harness-migrate
harness execute git_export:github --github-org myorg --github-token "$GITHUB_TOKEN"
```

The binary must be named `harness-<plugin-name>`, so it is also
`harness-migrate` — hence `plugin/bin/` rather than the repo root, which the
standalone build already occupies.

Useful while iterating (`task --list` for the rest):

```sh
task dev         # install into plugin/devhome, leaving ~/.harness alone
task check       # fmt, vet, spec validation, tests
task identity    # the JSON the host reads at install time
task spec        # the grammar YAML the host stores at install time
```

If the host CLI isn't on your PATH: `task install HARNESS=../squash-cli/bin/harness`
(relative paths resolve from the repo root, not from `plugin/`).

Version stamping follows core's convention — a `migrate/v*` tag series separate
from the standalone binary's `v*` tags, with dev builds reporting the next patch
(`0.1.0-dev` until the first tag exists). Override with `task build PLUGIN_VERSION=0.1.0`.

## POC status

Two commands are wired up: `execute git_export:github`, wrapping
`../cmd/github/git.go`, and `execute git_export:gitlab`, wrapping
`../cmd/gitlab/git.go`. Everything below is known-open, not overlooked.

- **`execute` is a placeholder verb.** The unified CLI has a closed verb set with
  no `export`/`import`/`convert`/`migrate` in it. The shape worth arguing for is
  `harness export repo:github` → `harness import repo`; that needs new verbs
  approved in core.
- **`git_export` is a placeholder noun,** picked because it does not collide.
  `code` already owns `repository`, and cross-plugin noun collision handling
  (`<plugin>@<noun>`) is not implemented in core yet — a colliding spec is
  dropped whole.
- **Flag names are prefixed** (`--github-org`, not `--org`) because `--org`,
  `--project`, `--profile`, `--debug`, `--timeout` are global and `--out`,
  `--format`, `--json`, `--yaml`, `--raw` are added to every workflow command.
- **Progress still goes through migrate's own `tracer`,** not `pkg/console`, so a
  migration doesn't look quite like the rest of `harness`. It keeps the animated
  progress bar, which core can't yet reproduce: `pkg/console` allows one animated
  stage per process and can't end one without printing a success/fail marker,
  while the engine nests `Start`/`Stop` across `errgroup` workers and reports
  failures through `Stop`. `pkg/bridge` overrides only `Debug()`, routing it to
  `hlog` so nothing needs to read the host's `--debug` flag.
- **No env-var fallbacks.** The standalone CLI accepts `github_ORG` and friends
  via kingpin's `Envar()`; core's spec schema has no equivalent, and this is a
  new interface rather than a compatible one, so the flags are just required.
- **No Harness auth is used** (`no_auth: true`); git-export only talks to GitHub.
  The import side is where `ctx.Auth` starts to matter.

Requires core at or after `Separate embedded plugin specs from builtin specs at
enumeration` (squash-cli 8dcacf7). Before that commit, a plugin binary loading
its own spec registered nothing and cobra rejected every verb the host
dispatched.

Worth pushing into core, which would let code here be deleted:

- **Cancel on SIGINT/SIGTERM in the host.** Core installs no signal handling at
  all, so `pkg/bridge.WithInterrupt` exists only so an interrupted export writes
  its checkpoint. The host builds `ctx.Context`; graceful shutdown shouldn't be
  per-plugin opt-in.
- **Nestable, concurrent stages in `pkg/console`,** with an exported neutral end
  alongside `Success`/`Fail`. That's what would let this drop `internal/tracer`.
- **`hlog.DebugEnabled()`.** `--debug` is a `BoolFunc` that calls
  `hlog.SetDebug()` and stores nothing, so the state is unreadable.
