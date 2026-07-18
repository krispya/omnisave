#!/usr/bin/env node
// Dev reset: wipe this checkout's server data (database + artifacts) and the
// client's local state so the next `pnpm dev` and client run start from
// nothing. Refuses to run while the dev server is listening — deleting the
// database under a live server corrupts it.
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { createConnection } from 'node:net';
import os from 'node:os';
import path from 'node:path';

const root = process.cwd();

function configField(yaml, field) {
  return yaml.match(new RegExp(`^${field}:\\s*(\\S+)`, 'm'))?.[1] ?? '';
}

function configPort(yaml) {
  const address = configField(yaml, 'listen_addr');
  const port = Number(address.split(':').pop());
  return Number.isInteger(port) ? port : 8080;
}

function portInUse(port) {
  return new Promise((resolve) => {
    const probe = createConnection({ port, host: '127.0.0.1' });
    probe.once('connect', () => probe.end(() => resolve(true)));
    probe.once('error', () => resolve(false));
  });
}

// Mirrors Go's os.UserConfigDir, where the client keeps its tracking state.
function clientStatePath() {
  if (process.platform === 'darwin') {
    return path.join(os.homedir(), 'Library', 'Application Support', 'omnisave', 'client.json');
  }
  if (process.platform === 'win32') {
    const appData = process.env.APPDATA ?? path.join(os.homedir(), 'AppData', 'Roaming');
    return path.join(appData, 'omnisave', 'client.json');
  }
  const configDir = process.env.XDG_CONFIG_HOME ?? path.join(os.homedir(), '.config');
  return path.join(configDir, 'omnisave', 'client.json');
}

function remove(target, label) {
  if (!existsSync(target)) return false;
  rmSync(target, { recursive: true, force: true });
  console.log(`  removed ${label}  ${dim(target)}`);
  return true;
}

const dim = (text) => `\x1b[2m${text}\x1b[0m`;

const configPath = path.join(root, 'server.yaml');
const yaml = existsSync(configPath) ? readFileSync(configPath, 'utf8') : '';
const dbPath = path.resolve(root, configField(yaml, 'db_path') || './omnisave.db');
const artifactDir = path.resolve(root, configField(yaml, 'artifact_dir') || './artifacts');

if (yaml && (await portInUse(configPort(yaml)))) {
  console.error(`the dev server is running on port ${configPort(yaml)} — stop it first (pnpm dev)`);
  process.exit(1);
}

console.log(`omnisave dev reset — ${path.basename(root)}`);
let removedAnything = false;
for (const [target, label] of [
  [dbPath, 'server database'],
  [`${dbPath}-wal`, 'server database WAL'],
  [`${dbPath}-shm`, 'server database SHM'],
  [artifactDir, 'server artifacts'],
  [clientStatePath(), 'client state'],
]) {
  removedAnything = remove(target, label) || removedAnything;
}
if (!removedAnything) {
  console.log('  nothing to remove — already clean');
}
