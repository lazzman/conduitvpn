import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const cwd = process.cwd();
const markers = [
  join(cwd, 'go.mod'),
  join(cwd, 'Makefile'),
  join(cwd, 'Dockerfile'),
  join(cwd, 'cmd/conduitvpn'),
  join(cwd, 'internal/webui'),
];
if (markers.some((p) => !existsSync(p))) {
  console.error('[pre-run] not conduitvpn workingDir:', cwd);
  process.exit(1);
}

let goMod = '';
try {
  goMod = readFileSync(join(cwd, 'go.mod'), 'utf8');
} catch (err) {
  console.error('[pre-run] cannot read go.mod:', err);
  process.exit(1);
}
if (!/^module conduitvpn$/m.test(goMod)) {
  console.error('[pre-run] go module is not conduitvpn:', cwd);
  process.exit(1);
}

function git(args) {
  return spawnSync('git', args, { cwd, encoding: 'utf8' });
}

function gitOk(args, err) {
  const r = git(args);
  if (r.status !== 0) {
    console.error(err, r.stderr || r.stdout);
    process.exit(1);
  }
  return r;
}

function gitPathExists(name) {
  const r = git(['rev-parse', '--git-path', name]);
  if (r.status !== 0) return false;
  const p = String(r.stdout).trim();
  return p.length > 0 && existsSync(p);
}

if (
  gitPathExists('rebase-merge') ||
  gitPathExists('rebase-apply') ||
  gitPathExists('MERGE_HEAD') ||
  gitPathExists('CHERRY_PICK_HEAD')
) {
  console.error('[pre-run] unfinished rebase/merge/cherry-pick; refusing to start a new rebase');
  process.exit(1);
}

const remotes = gitOk(['remote'], '[pre-run] git remote failed');
const remoteSet = new Set(String(remotes.stdout).split(/\s+/).filter(Boolean));
if (!remoteSet.has('upstream')) {
  const add = git(['remote', 'add', 'upstream', 'https://github.com/sarices/conduitvpn.git']);
  if (add.status !== 0) {
    console.error('[pre-run] cannot add upstream:', add.stderr);
    process.exit(1);
  }
}
if (!remoteSet.has('origin')) {
  console.error('[pre-run] missing remote: origin');
  process.exit(1);
}

const fetchUp = git(['fetch', '--prune', 'upstream']);
if (fetchUp.status !== 0) {
  console.error('[pre-run] git fetch upstream failed:', fetchUp.stderr);
  process.exit(1);
}
const fetchOrigin = git(['fetch', '--prune', 'origin']);
if (fetchOrigin.status !== 0) {
  console.error('[pre-run] git fetch origin failed:', fetchOrigin.stderr);
  process.exit(1);
}

gitOk(['rev-parse', '--verify', 'upstream/main^{commit}'], '[pre-run] upstream/main missing after fetch');
const mainRef = git(['rev-parse', '--verify', 'refs/heads/main']);
if (mainRef.status !== 0) {
  console.error('[pre-run] local branch main missing');
  process.exit(1);
}

const missingUp = gitOk(
  ['rev-list', '--count', 'refs/heads/main..upstream/main'],
  '[pre-run] rev-list upstream failed',
);
const nUp = parseInt(String(missingUp.stdout).trim(), 10) || 0;

const originMain = git(['rev-parse', '--verify', 'origin/main^{commit}']);
let nUnpushed = 0;
if (originMain.status !== 0) {
  nUnpushed = 1;
} else {
  const unpushed = gitOk(
    ['rev-list', '--count', 'origin/main..refs/heads/main'],
    '[pre-run] rev-list origin failed',
  );
  nUnpushed = parseInt(String(unpushed.stdout).trim(), 10) || 0;
}

if (nUp === 0 && nUnpushed === 0) {
  console.error('[pre-run] skip: local main already contains upstream/main and origin/main is in sync');
  process.exit(2);
}

console.error(
  `[pre-run] run agent: ${nUp} commit(s) on upstream/main not in local main; ${nUnpushed} local commit(s) not on origin/main`,
);
process.exit(0);
