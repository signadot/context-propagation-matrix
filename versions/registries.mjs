// Package-registry clients shared by pin.mjs --latest (newest version of the
// Dockerfile-pinned agents) and discover.mjs (last N versions of every tracer
// in variants.yaml, for back-testing).
//
// Every client returns plain version strings, newest first, with pre-release
// and non-release tags filtered out. No dependencies beyond Node 20+ fetch.

const UA = 'context-propagation-matrix (+https://contextpropagation.io)';

async function getJSON(url, headers = {}) {
  const res = await fetch(url, { headers: { 'user-agent': UA, accept: 'application/json', ...headers } });
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.json();
}

async function getText(url, headers = {}) {
  const res = await fetch(url, { headers: { 'user-agent': UA, ...headers } });
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.text();
}

// Loose semver: 1.2.3, 1.2.3.4 (New Relic PHP), 0.65b0 (OTel Python betas
// are their stable channel), v-prefixed. Rejects rc/alpha/beta/dev/snapshot
// except the OTel-Python "bN" convention.
const STABLE = /^v?(\d+)(\.\d+)*(b\d+)?$/;
export function isStable(v) {
  if (!v) return false;
  if (/(rc|alpha|beta|dev|snapshot|preview|nightly|-)/i.test(v) && !/^\d+\.\d+b\d+$/.test(v)) return false;
  return STABLE.test(v);
}
export const strip = (v) => String(v).replace(/^v/, '');

export function compareVersions(a, b) {
  const pa = strip(a).split(/[.b]/).map(Number);
  const pb = strip(b).split(/[.b]/).map(Number);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d) return d;
  }
  return 0;
}
const sortDesc = (vs) => [...new Set(vs.map(strip))].filter(isStable).sort((a, b) => compareVersions(b, a));

// ---------------------------------------------------------------------------

export const registries = {
  async maven(pkg) {
    // pkg = "group:artifact"; metadata XML lists every version.
    const [g, a] = pkg.split(':');
    const xml = await getText(`https://repo1.maven.org/maven2/${g.replace(/\./g, '/')}/${a}/maven-metadata.xml`);
    return sortDesc([...xml.matchAll(/<version>([^<]+)<\/version>/g)].map((m) => m[1]));
  },

  async npm(pkg) {
    const meta = await getJSON(`https://registry.npmjs.org/${encodeURIComponent(pkg).replace('%40', '@')}`, {
      accept: 'application/vnd.npm.install-v1+json',
    });
    return sortDesc(Object.keys(meta.versions ?? {}));
  },

  async pypi(pkg) {
    const meta = await getJSON(`https://pypi.org/pypi/${pkg}/json`);
    // Skip yanked releases.
    return sortDesc(Object.entries(meta.releases ?? {}).filter(([, files]) => files.length && !files.every((f) => f.yanked)).map(([v]) => v));
  },

  async goproxy(pkg) {
    const txt = await getText(`https://proxy.golang.org/${encodeURI(pkg).toLowerCase()}/@v/list`);
    return sortDesc(txt.split('\n').filter(Boolean));
  },

  async nuget(pkg) {
    const meta = await getJSON(`https://api.nuget.org/v3-flatcontainer/${pkg.toLowerCase()}/index.json`);
    return sortDesc(meta.versions ?? []);
  },

  async rubygems(pkg) {
    const list = await getJSON(`https://rubygems.org/api/v1/versions/${pkg}.json`);
    return sortDesc(list.filter((v) => !v.prerelease).map((v) => v.number));
  },

  async pecl(pkg) {
    const xml = await getText(`https://pecl.php.net/rest/r/${pkg.toLowerCase()}/allreleases.xml`);
    return sortDesc([...xml.matchAll(/<v>([^<]+)<\/v>/g)].map((m) => m[1]));
  },

  async packagist(pkg) {
    const meta = await getJSON(`https://repo.packagist.org/p2/${pkg}.json`);
    return sortDesc((meta.packages?.[pkg] ?? []).map((r) => r.version));
  },

  async crates(pkg) {
    const meta = await getJSON(`https://crates.io/api/v1/crates/${pkg}/versions`);
    return sortDesc(meta.versions.filter((v) => !v.yanked).map((v) => v.num));
  },

  async 'github-releases'(repo, { tagPrefix = '' } = {}) {
    // Paginate a few pages; releases are newest-first already.
    const token = process.env.GITHUB_TOKEN;
    const headers = token ? { authorization: `Bearer ${token}` } : {};
    const out = [];
    for (let page = 1; page <= 3; page++) {
      const rel = await getJSON(`https://api.github.com/repos/${repo}/releases?per_page=100&page=${page}`, headers);
      if (!rel.length) break;
      for (const r of rel) {
        if (r.draft || r.prerelease) continue;
        let tag = r.tag_name;
        if (tagPrefix && !tag.startsWith(tagPrefix)) continue;
        tag = tag.slice(tagPrefix.length);
        out.push(tag);
      }
    }
    return sortDesc(out);
  },

  async 'newrelic-download'(pkg) {
    // https://download.newrelic.com/php_agent/archive/ is an autoindex of
    // version directories; /release/ holds only the current one.
    if (pkg !== 'php_agent') throw new Error(`newrelic-download: unknown package ${pkg}`);
    const html = await getText('https://download.newrelic.com/php_agent/archive/');
    return sortDesc([...html.matchAll(/href="(\d+\.\d+\.\d+\.\d+)\/?"/g)].map((m) => m[1]));
  },
};

/** Latest stable version for a {registry, package} coordinate. */
export async function latest(source) {
  const vs = await list(source, 1);
  return vs[0];
}

/** Last n stable versions (newest first) for a {registry, package} coordinate. */
export async function list(source, n = 10) {
  const fn = registries[source.registry];
  if (!fn) throw new Error(`unknown registry ${source.registry}`);
  const vs = await fn(source.package, source);
  return vs.slice(0, n);
}
