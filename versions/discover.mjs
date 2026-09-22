#!/usr/bin/env node
// Enumerate back-test candidates: for every instrumented variant, the last N
// stable versions of its tracer package (from the registry coordinates in
// variants.yaml via `cpm coords`), minus the version already measured by the
// regular matrix, minus any (variant, version) pair that already has a
// `backtest-<variant>-<version>` GitHub Release.
//
//   node versions/discover.mjs                    # all public variants, depth 10
//   node versions/discover.mjs --depth 5 --variants datadog-java,otel-python
//   node versions/discover.mjs --include-secret   # New Relic too
//   node versions/discover.mjs --skip-published   # consult GitHub Releases
//
// Output (stdout): JSON array of {variant, version, package, registry,
// current} suitable for a GitHub Actions matrix. Progress goes to stderr.

import { execFileSync } from 'node:child_process';
import { parseArgs } from 'node:util';
import { compareVersions, list, strip } from './registries.mjs';

const { values: opts } = parseArgs({
  options: {
    depth: { type: 'string', default: '10' },
    variants: { type: 'string' },
    'include-secret': { type: 'boolean', default: false },
    'skip-published': { type: 'boolean', default: false },
    repo: { type: 'string', default: process.env.GITHUB_REPOSITORY ?? 'signadot/context-propagation-matrix' },
    max: { type: 'string', default: '250' }, // GitHub Actions caps a matrix at 256 jobs
  },
});

const depth = Number(opts.depth);
const only = opts.variants ? new Set(opts.variants.split(',').map((s) => s.trim())) : null;
const coords = JSON.parse(execFileSync('go', ['run', './cmd/cpm', 'coords'], { encoding: 'utf8' }));

let published = new Set();
if (opts['skip-published']) {
  try {
    const token = process.env.GITHUB_TOKEN;
    const headers = { 'user-agent': 'context-propagation-matrix', ...(token ? { authorization: `Bearer ${token}` } : {}) };
    for (let page = 1; page <= 10; page++) {
      const res = await fetch(`https://api.github.com/repos/${opts.repo}/releases?per_page=100&page=${page}`, { headers });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const rel = await res.json();
      if (!rel.length) break;
      for (const r of rel) if (r.tag_name.startsWith('backtest-')) published.add(r.tag_name);
    }
    console.error(`skip-published: ${published.size} backtest releases exist`);
  } catch (err) {
    console.error(`warning: could not list releases (${err.message}); not skipping`);
  }
}

// Registries are queried once per distinct package, not per variant.
const cache = new Map();
async function versionsFor(pkg) {
  const key = `${pkg.registry}:${pkg.package}`;
  if (!cache.has(key)) {
    cache.set(key, list({ registry: pkg.registry, package: pkg.package, tagPrefix: pkg.tagPrefix }, depth + 5).catch((err) => {
      console.error(`warning: ${key}: ${err.message}`);
      return [];
    }));
  }
  return cache.get(key);
}

const out = [];
for (const c of coords) {
  if (only && !only.has(c.variant)) continue;
  if (c.requires_secret && !opts['include-secret']) continue;
  const src = { ...c.tracer, tagPrefix: c.tracer.tag_prefix ?? '' };
  const versions = await versionsFor(src);
  // Relays sometimes report "ddtrace 1.25.0" or "v2.10.1"; keep the number.
  const current = strip((c.current_version ?? '').match(/\d+(\.\d+)+(b\d+)?/)?.[0] ?? '');
  const older = versions.filter((v) => !current || compareVersions(v, current) < 0).slice(0, depth);
  for (const version of older) {
    const tag = `backtest-${c.variant}-${version}`;
    if (published.has(tag)) continue;
    out.push({ variant: c.variant, version, package: src.package, registry: src.registry, current });
  }
  console.error(`${c.variant}: current ${current || '?'} → ${older.length} older candidate(s)`);
}

const max = Number(opts.max);
if (out.length > max) {
  console.error(`warning: ${out.length} candidates, truncating to ${max} (newest versions first per variant)`);
}
console.log(JSON.stringify(out.slice(0, max), null, 1));
