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
| `pkg/migrateplugin/migrate_github.go` | `github_organization:scm_bundle` handler |
| `pkg/migrateplugin/migrate_gitlab.go` | `gitlab_group:scm_bundle` handler |
| `pkg/migrateplugin/migrate_bitbucket.go` | `bitbucket_workspace:scm_bundle` handler |
| `pkg/migrateplugin/migrate_stash.go` | `stash_project:scm_bundle` handler |
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
harness migrate github_organization:scm_bundle --from myorg --github-token "$GITHUB_TOKEN"
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

All four export providers are wired up under the `migrate <from>:<to>` pair
verb: `migrate github_organization:scm_bundle` (`../cmd/github/git.go`),
`migrate gitlab_group:scm_bundle` (`../cmd/gitlab/git.go`),
`migrate bitbucket_workspace:scm_bundle` (`../cmd/bitbucket/git.go`), and
`migrate stash_project:scm_bundle` (`../cmd/stash/git.go`). Everything below is
known-open, not overlooked.

- **The import half does not exist yet.** `migrate scm_bundle:repository`
  (`../cmd/gitimport`) is what makes an exported bundle land in Harness Code, and
  it is the point where `ctx.Auth` starts to matter. Note that `code` owns the
  `repository` *noun*, but command identity is `verb + noun:noun_to`, so
  `migrate scm_bundle:repository` registers cleanly as long as this spec does not
  declare `repository` in its own `nouns:` block.
- **`:repository` names what the import creates, not how much of it.** One bundle
  becomes many repos, and PRs, webhooks, rules and labels are all children of a
  repo, so nothing created falls outside the `:to` noun. The dissatisfying part
  is that `--to` would carry a Harness *scope* rather than a repo id, which is
  why it is `presence: none` there — account/org/project come from the profile.
- **Credentials and endpoints keep the provider prefix** (`--github-token`, not
  `--token`), because the noun says which *system* but not whose *credential* —
  and in this CLI a bare `--token` reads as a Harness token, which is exactly
  what `git-import --token` takes. Narrowing and behavior flags stay bare
  (`--repo`, `--resume`, `--no-pr`): they mean the same thing on all four
  providers. GitLab's `--repo` would be `--project` in GitLab's own vocabulary,
  but `--project` is global.
- **Globals to avoid when adding flags:** `--org`, `--project`, `--profile`,
  `--debug`, `--timeout` are global, and `--out`, `--format`, `--json`, `--yaml`,
  `--raw` are added to every workflow command.
- **`--to` has no spec-level default,** so each handler falls back to `harness`
  itself. `migrate_from`/`migrate_to` carry `label` and `presence`
  (`required`/`optional`/`none`) but not a default value.
- **Progress still goes through migrate's own `tracer`,** not `pkg/console`, so a
  migration doesn't look quite like the rest of `harness`. It keeps the animated
  progress bar, which core can't yet reproduce: `pkg/console` allows one animated
  stage per process and can't end one without printing a success/fail marker,
  while the engine nests `Start`/`Stop` across `errgroup` workers and reports
  failures through `Stop`. `pkg/bridge` overrides only `Debug()`, routing it to
  `hlog` so nothing needs to read the host's `--debug` flag.
- **No env-var fallbacks.** The standalone CLI accepts `github_ORG` and friends
  via kingpin's `Envar()`, but never documented them, so nothing published is
  lost. Core's spec schema has no equivalent; it can be added if asked for.
- **No Harness auth is used** (`no_auth: true`); an export only talks to the
  source provider. The import side is where `ctx.Auth` starts to matter.

Requires core at or after `more spec control over to/from flags for migrate`
(squash-cli dde8ffe), which adds the `migrate` verb, `noun_to`, and the
`migrate_from`/`migrate_to` blocks. A host binary older than that rejects
`migrate` outright, since the verb set is closed and validated at load time.

Worth pushing into core, which would let code here be deleted:

- **Cancel on SIGINT/SIGTERM in the host.** Core installs no signal handling at
  all, so `pkg/bridge.WithInterrupt` exists only so an interrupted export writes
  its checkpoint. The host builds `ctx.Context`; graceful shutdown shouldn't be
  per-plugin opt-in.
- **Nestable, concurrent stages in `pkg/console`,** with an exported neutral end
  alongside `Success`/`Fail`. That's what would let this drop `internal/tracer`.
- **`hlog.DebugEnabled()`.** `--debug` is a `BoolFunc` that calls
  `hlog.SetDebug()` and stores nothing, so the state is unreadable.
