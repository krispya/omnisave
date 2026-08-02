#!/usr/bin/env node
// Dev reset: wipe this checkout's server data (database + artifacts) and the
// client's local state so the next `pnpm dev` and client run start from
// nothing. Refuses to run while the dev server is listening — deleting the
// database under a live server corrupts it.
import { existsSync, readdirSync, rmSync } from 'node:fs';
import { createConnection } from 'node:net';
import os from 'node:os';
import path from 'node:path';

const root = process.cwd();

function environmentPort() {
  const address = process.env.OMNISAVE_LISTEN_ADDR ?? '';
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

const environmentPath = path.join(root, '.env');
if (existsSync(environmentPath)) {
  process.loadEnvFile(environmentPath);
}
const dbPath = path.resolve(root, process.env.OMNISAVE_DB_PATH || './omnisave.db');
const storeDir = path.resolve(root, process.env.OMNISAVE_STORE_DIR || './store');

if (await portInUse(environmentPort())) {
  console.error(`the dev server is running on port ${environmentPort()} — stop it first (pnpm dev)`);
  process.exit(1);
}

// The store is the durable record of every save on this server, and deleting
// it is not recoverable from anywhere else. A reset during development is
// usually aimed at a store holding nothing but test data, so it only stops to
// ask when there is real content to lose.
function storedSaveCount(directory) {
  const objects = path.join(directory, 'objects');
  if (!existsSync(objects)) return 0;
  return readdirSync(objects, { recursive: true }).filter((entry) => String(entry).endsWith('.gz')).length;
}

const saveCount = storedSaveCount(storeDir);
if (saveCount > 0 && !process.argv.includes('--force')) {
  console.error(`${storeDir} holds ${saveCount} stored save file(s).`);
  console.error('Deleting it destroys them: nothing else on this machine has the content.');
  console.error('Copy the directory somewhere first, or re-run with --force.');
  process.exit(1);
}

console.log(`omnisave dev reset — ${path.basename(root)}`);
let removedAnything = false;
for (const [target, label] of [
  [dbPath, 'server database'],
  [`${dbPath}-wal`, 'server database WAL'],
  [`${dbPath}-shm`, 'server database SHM'],
  [storeDir, 'save store'],
  [clientStatePath(), 'client state'],
]) {
  removedAnything = remove(target, label) || removedAnything;
}
if (!removedAnything) {
  console.log('  nothing to remove — already clean');
}
