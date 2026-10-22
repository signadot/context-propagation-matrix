#!/usr/bin/env node
// pin.mjs — read and move the tracer/agent pins listed in versions/pins.yaml.
// The pins live in the manifests next to each relay; this tool only knows
// where they are and writes them the way their ecosystem expects (go get +
// tidy, cargo update --precise, a JSON key, a requirements line, a Gemfile
// line, a Dockerfile ARG).
//
//   node versions/pin.mjs                      # every pin with its current version (JSON)
//   node versions/pin.mjs --check              # pins resolve + agree with `cpm coords`; exit 1 if not
//   node versions/pin.mjs --latest [--only id] [--apply]
//                                              # kind: arg entries only (Dependabot owns the rest):
//                                              # newer stable versions as JSON; --apply rewrites (bump-agents.yml)
//   node versions/pin.mjs --variant otel-node --version 0.79.0
//                                              # set the pin behind a variant (backtest.yml, scratch checkout)
//
// --variant and --check run `go run ./cmd/cpm coords` (the registry says which
// package is a variant's tracer); kind: gomod needs `go`, kind: cargo `cargo`.
// Run from the repo root.

import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { parseArgs } from 'node:util';
import { compareVersions, latest, strip } from './registries.mjs';

const { values: opts } = parseArgs({
  options: {
    check: { type: 'boolean', default: false },
    latest: { type: 'boolean', default: false },
    apply: { type: 'boolean', default: false },
    only: { type: 'string' },
    variant: { type: 'string' },
    version: { type: 'string' },
    manifest: { type: 'string', default: 'versions/pins.yaml' },
  },
});

// ---------------------------------------------------------------------------
// Manifest. A minimal reader for this file's regular shape (a `pins:` list of
// flat maps; values are scalars, inline `{k: v}` maps, inline `[a, b]` lists,
// or block lists), so CI needs no YAML dependency.
function loadManifest(path) {
  const pins = [];
  let cur = null;
  let listKey = null;
  for (const raw of readFileSync(path, 'utf8').split('\n')) {
    const line = raw.replace(/\s+#.*$/, '').replace(/\s+$/, '');
    if (!line.trim() || line.trim().startsWith('#')) continue;
    let m;
    if ((m = line.match(/^\s*-\s+id:\s*(\S+)$/))) {
      cur = { id: m[1] };
      pins.push(cur);
      listKey = null;
    } else if (cur && listKey && (m = line.match(/^\s+-\s+(.+)$/))) {
      cur[listKey].push(unquote(m[1]));
    } else if (cur && (m = line.match(/^\s+(\w+):\s*(.*)$/))) {
      const [, k, v] = m;
      listKey = null;
      if (v === '') {
        cur[k] = [];
        listKey = k;
      } else if (v.startsWith('{')) {
        cur[k] = Object.fromEntries(
          [...v.replace(/^\{|\}$/g, '').matchAll(/(\w+):\s*("([^"]*)"|'([^']*)'|[^,}]+)/g)].map((x) => [x[1], (x[3] ?? x[4] ?? x[2]).trim()]),
        );
      } else if (v.startsWith('[')) {
        cur[k] = v.replace(/^\[|\]$/g, '').split(',').map((s) => unquote(s.trim())).filter(Boolean);
      } else {
        cur[k] = unquote(v);
      }
    }
  }
  for (const p of pins) {
    if (!kinds[p.kind]) die(`${p.id}: unknown kind ${p.kind}`);
    if (!p.source?.registry || !p.source?.package) die(`${p.id}: needs source {registry, package}`);
    p.variants ??= [];
  }
  return pins;
}
const unquote = (s) => s.replace(/^(['"])(.*)\1$/, '$2');
const die = (msg) => { console.error(`pin: ${msg}`); process.exit(1); };
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
const glob = (g) => new RegExp(`^${g.split('*').map(esc).join('.*')}$`);
function run(cmd, args, cwd) {
  console.error(`pin: (${cwd}) ${cmd} ${args.join(' ')}`);
  execFileSync(cmd, args, { cwd, stdio: ['ignore', 'inherit', 'inherit'] });
}

// ---------------------------------------------------------------------------
// Kinds. read(e) -> current version string; write(e, version) -> files touched.
const kinds = {
  arg: {
    pattern(e) {
      if (e.pattern) return new RegExp(e.pattern, 'm');
      if (e.arg) return new RegExp(`^ARG ${e.arg}=(?<version>\\S+)`, 'm');
      throw new Error('needs arg or pattern');
    },
    read(e) {
      const m = readFileSync(e.file, 'utf8').match(this.pattern(e));
      if (!m?.groups?.version) throw new Error(`pattern did not match in ${e.file}`);
      return m.groups.version;
    },
    write(e, version) {
      const src = readFileSync(e.file, 'utf8');
      const m = src.match(this.pattern(e));
      if (!m?.groups?.version) throw new Error(`pattern did not match in ${e.file}`);
      const start = m.index + m[0].indexOf(m.groups.version);
      writeFileSync(e.file, src.slice(0, start) + version + src.slice(start + m.groups.version.length));
      return [e.file];
    },
  },

  gomod: {
    read(e) {
      const src = readFileSync(join(e.dir, 'go.mod'), 'utf8');
      // Inside a require block or as a one-line `require mod vX`.
      const versions = e.modules.map((mod) => src.match(new RegExp(`^\\s*(?:require\\s+)?${esc(mod)} (v\\S+)`, 'm'))?.[1]);
      e.modules.forEach((mod, i) => { if (!versions[i]) throw new Error(`${mod} not required in ${e.dir}/go.mod`); });
      if (new Set(versions).size > 1) console.error(`pin: note: ${e.id} family not at one version: ${versions.join(', ')}`);
      return strip(versions[0]);
    },
    write(e, version) {
      run('go', ['get', ...e.modules.map((mod) => `${mod}@v${version}`)], e.dir);
      run('go', ['mod', 'tidy'], e.dir);
      return [join(e.dir, 'go.mod'), join(e.dir, 'go.sum')];
    },
  },

  npm: {
    read(e) {
      const deps = JSON.parse(readFileSync(e.file, 'utf8')).dependencies ?? {};
      for (const p of e.packages) if (!deps[p]) throw new Error(`${p} not in ${e.file} dependencies`);
      return deps[e.packages[0]];
    },
    write(e, version) {
      const json = JSON.parse(readFileSync(e.file, 'utf8'));
      for (const p of e.packages) {
        if (!json.dependencies?.[p]) throw new Error(`${p} not in ${e.file} dependencies`);
        json.dependencies[p] = version;
      }
      writeFileSync(e.file, JSON.stringify(json, null, 2) + '\n');
      return [e.file];
    },
  },

  pip: {
    lines(e) {
      const globs = e.packages.map(glob);
      return readFileSync(e.file, 'utf8').split('\n').map((line) => {
        const m = line.match(/^([A-Za-z0-9][A-Za-z0-9._-]*)==(\S+)/);
        return m && globs.some((g) => g.test(m[1])) ? { name: m[1], version: m[2], line } : { line };
      });
    },
    read(e) {
      const hit = this.lines(e).filter((l) => l.name);
      if (!hit.length) throw new Error(`no ${e.packages.join('|')}== line in ${e.file}`);
      return hit[0].version;
    },
    write(e, version) {
      const lines = this.lines(e);
      if (!lines.some((l) => l.name)) throw new Error(`no ${e.packages.join('|')}== line in ${e.file}`);
      writeFileSync(e.file, lines.map((l) => (l.name ? `${l.name}==${version}` : l.line)).join('\n'));
      return [e.file];
    },
  },

  bundler: {
    re: (gem) => new RegExp(`^(gem ['"]${esc(gem)}['"],\\s*['"])([^'"]+)(['"])`, 'm'),
    read(e) {
      const src = readFileSync(e.file, 'utf8');
      const versions = e.packages.map((gem) => src.match(this.re(gem))?.[2]);
      e.packages.forEach((gem, i) => { if (!versions[i]) throw new Error(`gem '${gem}' not pinned in ${e.file}`); });
      return versions[0];
    },
    write(e, version) {
      let src = readFileSync(e.file, 'utf8');
      for (const gem of e.packages) {
        if (!this.re(gem).test(src)) throw new Error(`gem '${gem}' not pinned in ${e.file}`);
        src = src.replace(this.re(gem), `$1${version}$3`);
      }
      writeFileSync(e.file, src);
      return [e.file];
    },
  },

  cargo: {
    // Cargo.toml holds the requirement ("0.32"); Cargo.lock the exact version,
    // which is what the relay reports and what we pin to.
    locked(e, crate) {
      const lock = readFileSync(join(e.dir, 'Cargo.lock'), 'utf8');
      const m = lock.match(new RegExp(`\\[\\[package\\]\\]\\nname = "${esc(crate)}"\\nversion = "([^"]+)"`));
      if (!m) throw new Error(`${crate} not in ${e.dir}/Cargo.lock`);
      return m[1];
    },
    reqRe: (crate) => new RegExp(`^(${esc(crate)}\\s*=\\s*(?:\\{[^}]*?version\\s*=\\s*)?")([^"]+)(")`, 'm'),
    read(e) {
      const toml = readFileSync(join(e.dir, 'Cargo.toml'), 'utf8');
      for (const crate of e.packages) if (!this.reqRe(crate).test(toml)) throw new Error(`${crate} not in ${e.dir}/Cargo.toml`);
      return this.locked(e, e.packages[0]);
    },
    write(e, version) {
      // Exact requirement in the scratch Cargo.toml, then move the whole
      // family in the lock in one resolver pass (crates of a family require
      // each other at the same version, so one at a time cannot resolve).
      // The Dockerfile's `cargo build --locked` then applies unchanged.
      let toml = readFileSync(join(e.dir, 'Cargo.toml'), 'utf8');
      for (const crate of e.packages) {
        if (!this.reqRe(crate).test(toml)) throw new Error(`${crate} not in ${e.dir}/Cargo.toml`);
        toml = toml.replace(this.reqRe(crate), `$1=${version}$3`);
      }
      writeFileSync(join(e.dir, 'Cargo.toml'), toml);
      run('cargo', ['update', ...e.packages.flatMap((crate) => ['-p', crate])], e.dir);
      return [join(e.dir, 'Cargo.toml'), join(e.dir, 'Cargo.lock')];
    },
  },
};

// ---------------------------------------------------------------------------
const pins = loadManifest(opts.manifest);
const sameSource = (a, b) => a.registry === b.registry && a.package === b.package;
const coords = () => JSON.parse(execFileSync('go', ['run', './cmd/cpm', 'coords'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] }));
const pinFor = (variant, cs) => {
  const c = cs.find((x) => x.variant === variant);
  if (!c?.tracer) return { error: `${variant}: not an instrumented variant in variants.yaml` };
  const hits = pins.filter((p) => p.variants.includes(variant) && sameSource(p.source, c.tracer));
  if (hits.length !== 1) return { error: `${variant}: ${hits.length} pins have source ${c.tracer.registry}/${c.tracer.package} and list the variant (need exactly 1)` };
  return { pin: hits[0], tracer: c.tracer };
};

if (opts.variant) {
  if (!opts.version) die('--variant needs --version');
  const version = strip(opts.version);
  const { pin, error } = pinFor(opts.variant, coords());
  if (error) die(error);
  const from = kinds[pin.kind].read(pin);
  const files = kinds[pin.kind].write(pin, version);
  console.error(`pin: ${opts.variant}: ${pin.id} ${from} -> ${version} (${files.join(', ')})`);
  console.log(JSON.stringify({ variant: opts.variant, id: pin.id, kind: pin.kind, from, to: version, files }, null, 2));
} else if (opts.latest) {
  // Forward bumps for what Dependabot cannot see. The other kinds are
  // Dependabot's; proposing them here would open duplicate PRs.
  const bumps = [];
  for (const p of pins.filter((p) => p.kind === 'arg' && (!opts.only || p.id === opts.only))) {
    let current;
    try { current = kinds.arg.read(p); } catch (err) { console.error(`${p.id}: ${err.message}`); process.exitCode = 1; continue; }
    let newest;
    try { newest = await latest(p.source); } catch (err) { console.error(`${p.id}: lookup failed: ${err.message}`); continue; }
    if (!newest || compareVersions(newest, current) <= 0) continue;
    bumps.push({ id: p.id, file: p.file, from: current, to: newest, source: p.source, variants: p.variants });
    if (opts.apply) {
      kinds.arg.write(p, newest);
      console.error(`${p.id}: ${current} -> ${newest} (${p.file})`);
    }
  }
  console.log(JSON.stringify(bumps, null, 2));
} else {
  // List (and, with --check, cross-check against the registry).
  const out = [];
  for (const p of pins) {
    try {
      out.push({ id: p.id, kind: p.kind, current: kinds[p.kind].read(p), source: p.source, variants: p.variants });
    } catch (err) {
      console.error(`${p.id}: ${err.message}`);
      process.exitCode = 1;
    }
  }
  if (opts.check) {
    const cs = coords();
    const known = new Set(cs.map((c) => c.variant));
    for (const c of cs) {
      const { pin, error } = pinFor(c.variant, cs);
      if (error) { console.error(error); process.exitCode = 1; } else console.error(`${c.variant}: ${pin.id} (${pin.kind}) pins ${out.find((o) => o.id === pin.id)?.current ?? '?'}`);
    }
    for (const p of pins) for (const v of p.variants) if (!known.has(v)) { console.error(`${p.id}: variant ${v} is not in variants.yaml`); process.exitCode = 1; }
    console.error(process.exitCode ? 'pin: check failed' : `pin: ${pins.length} pins, ${cs.length} instrumented variants, all resolved`);
  }
  console.log(JSON.stringify(out, null, 2));
}
