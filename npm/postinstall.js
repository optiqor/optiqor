#!/usr/bin/env node
// Downloads the platform-specific Go binary from the matching GitHub Release
// and places it at vendor/optiqor. Designed to fail loudly on unsupported
// platforms instead of silently degrading.

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const zlib = require('zlib');
const { pipeline } = require('stream');
const { promisify } = require('util');
const { execFileSync } = require('child_process');

const pkg = require('../package.json');
const VERSION = pkg.version;

// Skip download in CI/dev when the user is building from source.
if (process.env.OPTIQOR_SKIP_POSTINSTALL === '1') {
  console.log('optiqor: OPTIQOR_SKIP_POSTINSTALL=1, skipping binary download.');
  process.exit(0);
}

const platform = process.platform;
const arch = process.arch;

const supported = {
  'darwin-x64': 'darwin_amd64',
  'darwin-arm64': 'darwin_arm64',
  'linux-x64': 'linux_amd64',
  'linux-arm64': 'linux_arm64',
};

const key = `${platform}-${arch}`;
const target = supported[key];

if (!target) {
  console.error(`optiqor: unsupported platform ${key}.`);
  console.error('optiqor: build from source instead:');
  console.error('  go install github.com/optiqor/optiqor-cli/cmd/optiqor@latest');
  process.exit(1);
}

const url = `https://github.com/optiqor/optiqor-cli/releases/download/v${VERSION}/optiqor_${VERSION}_${target}.tar.gz`;
const checksumsUrl = `https://github.com/optiqor/optiqor-cli/releases/download/v${VERSION}/checksums.txt`;
const archiveName = `optiqor_${VERSION}_${target}.tar.gz`;
const vendorDir = path.join(__dirname, '..', 'vendor');
fs.mkdirSync(vendorDir, { recursive: true });

const tarballPath = path.join(vendorDir, 'optiqor.tar.gz');

const get = (u) =>
  new Promise((resolve, reject) => {
    https
      .get(u, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return resolve(get(res.headers.location));
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`HTTP ${res.statusCode} fetching ${u}`));
        }
        resolve(res);
      })
      .on('error', reject);
  });

const pipelineP = promisify(pipeline);

// Returns the hex digest for archiveName from GoReleaser checksums.txt.
const expectedDigest = (checksumsText) => {
  for (const line of checksumsText.split(/\r?\n/)) {
    const [digest, name] = line.trim().split(/\s+/);
    if (name !== archiveName) continue;
    return /^[0-9a-f]{64}$/i.test(digest) ? digest.toLowerCase() : null;
  }
  return null;
};

const sha256Of = (file) =>
  crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');

const fetchText = async (u) => {
  const res = await get(u);
  let body = '';
  res.setEncoding('utf8');
  for await (const chunk of res) body += chunk;
  return body;
};

(async () => {
  try {
    console.log(`optiqor: downloading binary for ${key}...`);
    const res = await get(url);
    await pipelineP(res, fs.createWriteStream(tarballPath));

    // Verify the SHA-256 digest against the release's checksums.txt
    // before extracting, so a MITM'd or hijacked release asset never
    // executes on the user's machine.
    const checksumsText = await fetchText(checksumsUrl);
    const digest = expectedDigest(checksumsText);
    if (!digest) {
      fs.unlinkSync(tarballPath);
      console.error(`optiqor: ${archiveName} missing from ${checksumsUrl}`);
      console.error('optiqor: refusing to install an unverified binary.');
      process.exit(1);
    }
    const actual = sha256Of(tarballPath);
    if (actual !== digest) {
      fs.unlinkSync(tarballPath);
      console.error(`optiqor: checksum mismatch for ${archiveName}`);
      console.error(`optiqor:   expected ${digest}`);
      console.error(`optiqor:   actual   ${actual}`);
      console.error('optiqor: refusing to install a tampered binary.');
      process.exit(1);
    }

    // tar -xzf using system tar (avoids adding tar npm dep).
    execFileSync('tar', ['-xzf', tarballPath, '-C', vendorDir], { stdio: 'inherit' });
    fs.unlinkSync(tarballPath);
    console.log('optiqor: ready. Run `optiqor --version` to verify.');
  } catch (err) {
    console.error('optiqor: failed to install binary:', err.message);
    console.error('optiqor: this is non-fatal — build from source if needed:');
    console.error('  go install github.com/optiqor/optiqor-cli/cmd/optiqor@latest');
    // Exit 0 so npm install does not abort entirely.
    process.exit(0);
  }
})();
