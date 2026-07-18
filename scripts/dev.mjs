#!/usr/bin/env node
// Self-configuring dev entry: `pnpm dev` works in any checkout, including a
// fresh git worktree running beside the main tree. Missing dependencies are
// installed, a missing server.yaml is derived from the main checkout's config
// on a free port (fresh empty database, inherited token and credentials), and
// the dash proxy follows the server port automatically.
import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import path from 'node:path';

const root = process.cwd();

function git(...args) {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
}

function mainCheckoutDir() {
  const commonDir = path.resolve(root, git('rev-parse', '--git-common-dir'));
  return path.dirname(commonDir);
}

function portIsFree(port) {
  return new Promise((resolve) => {
    const probe = createServer();
    probe.once('error', () => resolve(false));
    probe.once('listening', () => probe.close(() => resolve(true)));
    probe.listen(port);
  });
}

async function nextFreePort(from) {
  for (let port = from; port < from + 100; port += 1) {
    if (await portIsFree(port)) return port;
  }
  throw new Error(`no free port found between ${from} and ${from + 99}`);
}

function configField(yaml, field) {
  return yaml.match(new RegExp(`^${field}:\\s*(\\S+)`, 'm'))?.[1] ?? '';
}

function configPort(yaml) {
  const address = configField(yaml, 'listen_addr');
  const port = Number(address.split(':').pop());
  return Number.isInteger(port) ? port : 8080;
}

async function ensureServerConfig() {
  const configPath = path.join(root, 'server.yaml');
  if (existsSync(configPath)) return;

  const mainDir = mainCheckoutDir();
  const mainConfig = path.join(mainDir, 'server.yaml');
  const source = mainDir !== root && existsSync(mainConfig) ? mainConfig : 'server.yaml.example';
  let yaml = readFileSync(path.resolve(root, source), 'utf8');

  const port = await nextFreePort(configPort(yaml) + (source === 'server.yaml.example' ? 0 : 1));
  yaml = yaml.replace(/^listen_addr:.*$/m, `listen_addr: :${port}`);
  if (configField(yaml, 'token') === 'replace-with-a-secure-token') {
    yaml = yaml.replace(/^token:.*$/m, `token: ${randomBytes(32).toString('hex')}`);
  }
  writeFileSync(configPath, yaml);
  console.log(`created server.yaml from ${source} on port ${port}`);
}

if (!existsSync(path.join(root, 'node_modules', 'concurrently'))) {
  console.log('installing dependencies…');
  execFileSync('pnpm', ['install'], { cwd: root, stdio: 'inherit' });
}
await ensureServerConfig();

const config = readFileSync(path.join(root, 'server.yaml'), 'utf8');
const port = configPort(config);
const serverURL = `http://localhost:${port}`;

const dim = (text) => `\x1b[2m${text}\x1b[0m`;
console.log(`omnisave dev — ${path.basename(root)}`);
console.log(dim(`  server  ${serverURL}`));
console.log(dim(`  token   ${configField(config, 'token')}`));
console.log(dim('  dash    URL printed by vite below'));
console.log();

const child = spawn(
  'pnpm',
  [
    'exec',
    'concurrently',
    '--kill-others',
    '--names',
    'api,dash',
    '--prefix-colors',
    'blue,green',
    'pnpm run dev:server',
    'pnpm run dev:dash',
  ],
  {
    cwd: root,
    stdio: 'inherit',
    env: { ...process.env, OMNISAVE_DEV_API: serverURL },
  }
);
child.on('exit', (code) => process.exit(code ?? 0));
