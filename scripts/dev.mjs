#!/usr/bin/env node
// Self-configuring dev entry: `pnpm dev` works in any checkout, including a
// fresh git worktree running beside the main tree. Missing dependencies are
// installed, a missing .env is derived from the main checkout's environment
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

function environmentField(environment, name) {
  const value = environment.match(new RegExp(`^${name}=(.*)$`, 'm'))?.[1].trim() ?? '';
  return value.replace(/^(['"])(.*)\1$/, '$2');
}

function environmentPort(environment) {
  const address = environmentField(environment, 'OMNISAVE_LISTEN_ADDR');
  const port = Number(address.split(':').pop());
  return Number.isInteger(port) ? port : 8080;
}

function setEnvironmentField(environment, name, value) {
  const assignment = `${name}=${value}`;
  const pattern = new RegExp(`^${name}=.*$`, 'm');
  if (pattern.test(environment)) return environment.replace(pattern, assignment);
  return `${environment.trimEnd()}\n${assignment}\n`;
}

async function ensureServerEnvironment() {
  const environmentPath = path.join(root, '.env');
  if (existsSync(environmentPath)) return environmentPath;

  const mainDir = mainCheckoutDir();
  const mainEnvironment = path.join(mainDir, '.env');
  const exampleEnvironment = path.join(root, '.env.example');
  const source =
    mainDir !== root && existsSync(mainEnvironment) ? mainEnvironment : exampleEnvironment;
  let environment = readFileSync(source, 'utf8');

  const inherited = source === mainEnvironment;
  const port = await nextFreePort(environmentPort(environment) + (inherited ? 1 : 0));
  environment = setEnvironmentField(environment, 'OMNISAVE_LISTEN_ADDR', `:${port}`);
  if (environmentField(environment, 'OMNISAVE_TOKEN') === '') {
    environment = setEnvironmentField(environment, 'OMNISAVE_TOKEN', randomBytes(32).toString('hex'));
  }
  writeFileSync(environmentPath, environment);
  console.log(`created .env from ${path.basename(source)} on port ${port}`);
  return environmentPath;
}

if (!existsSync(path.join(root, 'node_modules', 'concurrently'))) {
  console.log('installing dependencies…');
  execFileSync('pnpm', ['install'], { cwd: root, stdio: 'inherit' });
}
const environmentPath = await ensureServerEnvironment();
process.loadEnvFile(environmentPath);

const port = Number(process.env.OMNISAVE_LISTEN_ADDR?.split(':').pop()) || 8080;
const serverURL = `http://localhost:${port}`;

const dim = (text) => `\x1b[2m${text}\x1b[0m`;
console.log(`omnisave dev — ${path.basename(root)}`);
console.log(dim(`  server  ${serverURL}`));
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
