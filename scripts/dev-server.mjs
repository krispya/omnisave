#!/usr/bin/env node
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const environmentPath = path.join(root, '.env');
if (existsSync(environmentPath)) {
  process.loadEnvFile(environmentPath);
}

const child = spawn('go', ['run', './cmd/server'], {
  cwd: root,
  stdio: 'inherit',
  env: process.env,
});
child.on('exit', (code) => process.exit(code ?? 0));
