#!/usr/bin/env node

const assert = require('assert/strict');
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

const root = path.join(__dirname, '..');
const vendorDir = path.join(root, 'vendor');

const runPostinstallWithHttpFailure = () => {
  const script = `
    const https = require('https');
    const { Readable } = require('stream');

    https.get = (url, callback) => {
      const response = new Readable({
        read() {
          this.push(null);
        },
      });
      response.statusCode = 404;
      response.headers = {};
      process.nextTick(() => callback(response));

      return {
        on() {
          return this;
        },
      };
    };

    Object.defineProperty(process, 'platform', { value: 'linux' });
    Object.defineProperty(process, 'arch', { value: 'x64' });
    require('./npm/postinstall.js');
  `;

  return spawnSync(process.execPath, ['-e', script], {
    cwd: root,
    encoding: 'utf8',
  });
};

fs.rmSync(vendorDir, { recursive: true, force: true });

try {
  const result = runPostinstallWithHttpFailure();

  assert.equal(
    result.status,
    1,
    `expected failed binary download to exit 1\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`
  );
  assert.match(result.stderr, /failed to install binary: HTTP 404/);
  assert.doesNotMatch(result.stderr, /non-fatal/);
} finally {
  fs.rmSync(vendorDir, { recursive: true, force: true });
}
