#!/usr/bin/env node
// Finds newer versions of the Dockerfile-pinned agents in versions/agents.yaml.
//
//   node versions/bump-agents.mjs            # print proposed bumps as JSON
//   node versions/bump-agents.mjs --apply    # rewrite the files in place
//   node versions/bump-agents.mjs --only dd-java-agent --apply
//
// The GitHub workflow calls it once per agent (--only) on a fresh branch and
// opens a PR from the diff. Exits 0 with an empty list when nothing is newer.

import { readFileSync, writeFileSync } from 'node:fs';
import { parseArgs } from 'node:util';
import { compareVersions, latest } from './registries.mjs';

const { values: opts } = parseArgs({
  options: {
    apply: { type: 'boolean', default: false },
    check: { type: 'boolean', default: false }, // offline: only verify every pattern matches its file
    only: { type: 'string' },
    manifest: { type: 'string', default: 'versions/agents.yaml' },
  },
});

// Minimal YAML reader for this manifest's very regular shape (a list of flat
// maps with one nested `source` map). Avoids a dependency in CI.
function loadManifest(path) {
  const yaml = readFileSync(path, 'utf8');
  const agents = [];
  let cur = null;
  for (const raw of yaml.split('\n')) {
    const line = raw.replace(/\s+#.*$/, '');
    if (!line.trim() || line.trim().startsWith('#')) continue;
    let m;
    if ((m = line.match(/^\s*-\s+id:\s*(\S+)/))) {
      cur = { id: m[1] };
      agents.push(cur);
    } else if (cur && (m = line.match(/^\s+(\w+):\s*(.+)$/))) {
      const [, k, v] = m;
      if (k === 'source') {
        cur.source = Object.fromEntries(
          [...v.replace(/^\{|\}$/g, '').matchAll(/(\w+):\s*("([^"]*)"|[^,}]+)/g)].map((x) => [x[1], (x[3] ?? x[2]).trim()]),
        );
      } else if (k === 'variants') {
        cur.variants = v.replace(/^\[|\]$/g, '').split(',').map((s) => s.trim()).filter(Boolean);
      } else {
        cur[k] = v.replace(/^'|'$/g, '');
      }
    }
  }
  return agents;
}

function patternFor(a) {
  if (a.pattern) return new RegExp(a.pattern);
  if (a.arg) return new RegExp(`^ARG ${a.arg}=(?<version>\\S+)`, 'm');
  throw new Error(`${a.id}: needs arg or pattern`);
}

const agents = loadManifest(opts.manifest).filter((a) => !opts.only || a.id === opts.only);
const bumps = [];
for (const a of agents) {
  const src = readFileSync(a.file, 'utf8');
  const re = patternFor(a);
  const m = src.match(re);
  if (!m?.groups?.version) {
    console.error(`${a.id}: pattern did not match in ${a.file}`);
    process.exitCode = 1;
    continue;
  }
  const current = m.groups.version;
  if (opts.check) {
    console.error(`${a.id}: ${a.file} pins ${current} (${a.source.registry}/${a.source.package})`);
    continue;
  }
  let newest;
  try {
    newest = await latest(a.source);
  } catch (err) {
    console.error(`${a.id}: lookup failed: ${err.message}`);
    continue;
  }
  if (!newest || compareVersions(newest, current) <= 0) continue;
  const bump = { id: a.id, file: a.file, from: current, to: newest, source: a.source, variants: a.variants ?? [] };
  bumps.push(bump);
  if (opts.apply) {
    const start = m.index + m[0].indexOf(current);
    const updated = src.slice(0, start) + newest + src.slice(start + current.length);
    writeFileSync(a.file, updated);
    console.error(`${a.id}: ${current} -> ${newest} (${a.file})`);
  }
}
console.log(JSON.stringify(bumps, null, 2));
