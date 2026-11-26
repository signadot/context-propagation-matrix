# How versions are kept current

Every version in the harness is pinned to an exact value: each
dependency in a manifest, each base image, each tracer. A build next
month produces the same images as a build today.

Two bots move the pins. They never move the same package.

| | Dependabot | `bump-agents.yml` |
| --- | --- | --- |
| Moves | Application dependencies, base images, tracer-adjacent packages | The tracers and agents |
| Configured in | `.github/dependabot.yml` | `pins.yaml` and `.github/workflows/bump-agents.yml` |
| Looks for new versions | Weekly | Every 6 hours, at minute 41 |
| Pull requests | One grouped pull request for each ecosystem. One for each image and for each tracer-adjacent package | One for each pin, on the branch `bump/<id>` |
| Merged by | A person | Auto-merge, after the checks pass and one person approves |
| Measured after the merge | At once, by the push run of `matrix.yml` | By the next run of `matrix.yml`, at most 6 hours later |

## Why two bots

The tracer version is the thing the harness measures. A change to it has
to be one pull request for one tracer, so that a cell that changes
afterwards can be attributed to one version. An application dependency
is not measured, so many of them can move in one pull request.

Dependabot cannot do the tracer part:

- It does not see a version in a Dockerfile `ARG`. 15 of the 35 pins are
  written that way (the Java, .NET and PHP agents).
- A tracer is often several packages that share one version: a family of
  Go modules, the `opentelemetry-*` Python packages, a Rust crate and the
  API crate it requires. Dependabot proposes them one at a time.
- A lock file has to move with the pin (`Gemfile.lock`, `Cargo.lock`,
  `go.sum`).

The same code that moves a pin forward also moves it backward for a
back-test (`cpm pins set`), so a back-test builds the relay exactly as a
bump does.

## `pins.yaml`: where every tracer pin is

`pins.yaml` does not hold the versions. They stay in the manifest next
to each relay. `pins.yaml` says where each pin is and how to rewrite it.

| Kind | Pins | Where the version is | How it is moved |
| --- | --- | --- | --- |
| `arg` | 15 | `ARG NAME=<version>` in a Dockerfile | The line is rewritten |
| `gomod` | 6 | A family of modules in `go.mod` | `go get` for the family, then `go mod tidy` |
| `npm` | 4 | `package.json` | The key is rewritten |
| `pip` | 4 | `requirements-<tool>.txt` | Every `pkg==` line is rewritten |
| `bundler` | 4 | `Gemfile` | The `gem` line is rewritten, then `bundle lock --update` |
| `cargo` | 2 | `Cargo.toml` | The requirement is rewritten, then `cargo update` |

Each pin names its registry and package in `source`. A variant in
`variants.yaml` names the same registry and package in `tracer`. That
equality is how a variant finds its pin.

Three commands read the file:

| Command | Used by | What it does |
| --- | --- | --- |
| `cpm pins latest [-only id] [-apply]` | `bump-agents.yml` | Looks up the newest stable version of each pin. With `-apply`, rewrites the pin |
| `cpm pins set -variant V -version X` | `backtest.yml` | Sets the pin behind a variant to an older version, in the job's checkout only |
| `cpm pins check` | `verify.yml` | Fails when a pin does not resolve in its file, when an instrumented variant does not have exactly one pin, when a `Gemfile.lock` does not hold the pinned version, or when a `Cargo.lock` holds two copies of a pinned crate |

## What Dependabot moves

`.github/dependabot.yml` has one entry for each ecosystem: Go modules,
npm, pip, Bundler, Composer, Maven, NuGet, Cargo and Docker.

- **Application dependencies.** Everything that is not a tracer is in
  the group `app-deps`. Dependabot opens one pull request for the group
  in each ecosystem, across all the directories of that ecosystem.
- **Base images.** One pull request for each image in each directory.
- **Tracer-adjacent packages.** Some packages belong to a tracer and
  have their own version line: the OpenTelemetry Go contrib modules, the
  New Relic Go integrations, Orchestrion, the OpenTelemetry Ruby
  instrumentation gems. `pins.yaml` does not list them. They are kept
  out of the group, so each one is its own pull request.

Every package that `pins.yaml` lists is in the `ignore` list of its
ecosystem. Dependabot never opens a pull request for a tracer.

**Rule:** when you add a tracer package to `pins.yaml`, add it to the
`ignore` list in `.github/dependabot.yml` in the same pull request.

Dependabot is also told to ignore these, because it cannot move them
correctly:

| Ignored | Why |
| --- | --- |
| Major versions of `dotnet/sdk` and `dotnet/aspnet` | They must move together with `TargetFramework` in `Relay.csproj` |
| Major versions of `eclipse-temurin` | It must move together with `java.version` in `pom.xml` and the Maven image |
| The `maven` image | The JDK is in the image's suffix, which Dependabot does not read as a version |

A person moves those. `toolchains.yml` fails a pull request in which a
manifest and its Dockerfile name different toolchain versions.

### Life of a Dependabot pull request

1. Dependabot opens it, weekly.
2. `verify.yml` runs. It builds and runs the relays the change can
   affect.
3. A person reviews and merges it.
4. The merge is a push to `main`, so `matrix.yml` starts and measures
   the variants the change can affect. A change to the root Go module
   or to a shared image affects every variant.

A major version of a language runtime (Python, Ruby, Go) arrives as an
ordinary pull request. The runtime is part of what a variant is measured
on, so merging it is a decision, not routine.

## What `bump-agents.yml` moves

### Life of a tracer bump

1. **Look.** Every 6 hours `cpm pins latest` asks each pin's registry
   for the newest stable version. Pre-releases are not proposed.
2. **Decide.** For each pin that is behind, the workflow looks at the
   open pull request on `bump/<id>`. It leaves the pull request alone
   when all three of these are true: it already proposes that version,
   it does not conflict with `main`, and `main` has not changed the
   relay since the branch was built (`scripts/bump-pr-stale.sh`).
3. **Rewrite.** Otherwise `cpm pins latest -only <id> -apply` rewrites
   the pin and its lock file. The workflow commits that on `bump/<id>`,
   which is always the current `main` plus one commit, and force-pushes
   the branch. The push is refused if someone else has pushed to the
   branch in the meantime.
4. **Open.** The workflow opens or updates the pull request. The title
   is `bump(<id>): <from> → <to>`. The labels are `dependencies`,
   `tracer-bump` and `auto-merge`. It requests squash auto-merge and
   closes any older pull request for the same pin.
5. **Check.** `verify.yml` builds and runs the variants of the pin.
6. **Merge.** GitHub merges the pull request when the three required
   checks pass and one person has approved it.
7. **Measure.** The next run of `matrix.yml` finds the pin's variants
   stale and measures them. That is a new `run-*` release, a site build
   and a changelog entry.

### What a person does for each bump

The workflow uses its own token (`GITHUB_TOKEN`). That has two effects.

- GitHub holds the `verify.yml` run of a pull request opened with that
  token. A maintainer has to press **Approve and run** on the pull
  request.
- GitHub starts no push run for a merge made with that token. So step 7
  waits for the next run of `matrix.yml`, which is at most 6 hours away.

So each bump needs two actions from a person: **Approve and run**, and
an approving review.

The optional repository secret `BUMP_PAT` removes both effects. With it
the workflow pushes and opens pull requests as the owner of that token.
The checks then start without approval, and the merge starts
`matrix.yml` at once. The secret was not set on 16 November 2026: bump
pull requests were opened and merged by `github-actions`.

## Where the two meet

A Dependabot pull request and a bump can change the same file, for
example `go.sum` or a `Gemfile.lock`.

- A bump whose branch conflicts with `main` is rebuilt at the next run
  of `bump-agents.yml`.
- A bump whose relay `main` has changed is rebuilt too, even without a
  conflict. Its checks ran against the older relay.
- A Dependabot pull request that conflicts is rebased by Dependabot.

Branch protection on `main` does not require a branch to be up to date.
A pull request can therefore merge on checks that ran against an older
`main`. Two guards exist for that:

- `verify.yml` fails a pull request run that starts after `main` has
  changed a relay the pull request changes
  (`scripts/run-base-moved.sh`). This matters for a held run, which is
  approved some time after it was created.
- `bump-agents.yml` rebuilds a bump as described above.

Neither guard covers a pull request whose checks passed in time and
which is merged later. A merge queue on `main` would. `verify.yml`
already runs on `merge_group` for that case, and the queue is not
switched on.

## By hand

| Task | How |
| --- | --- |
| Move a toolchain to a new major version | Change the image tags and the manifest in one pull request (`.NET`: `sdk`, `aspnet`, `Relay.csproj`. Java: the Maven image, `eclipse-temurin`, `pom.xml`) |
| Look for tracer releases now | Actions → bump-agents → Run workflow |
| Apply one bump in a local checkout | `go run ./cmd/cpm pins latest -only <id> -apply` |
| Measure a variant now | Actions → matrix → Run workflow, with the variant ids |
| Keep a version out of the back-tests | Add `backtest: { min: X }` or `skip: [...]` to the pin in `pins.yaml`, with the reason in a comment |
| Measure older versions again | Actions → backtest → Run workflow, with `refresh`. See the limit in [results-pipeline.md](results-pipeline.md#limit-of-refresh) |
