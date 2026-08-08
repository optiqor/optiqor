#!/usr/bin/env node
// Downloads the platform-specific Go binary from the matching GitHub Release
// and places it at vendor/optiqor. Designed to fail loudly on unsupported
// platforms instead of silently degrading.

const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
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

const releaseBaseUrl = `https://github.com/optiqor/optiqor-cli/releases/download/v${VERSION}`;
const archiveName = `optiqor_${VERSION}_${target}.tar.gz`;
const url = `${releaseBaseUrl}/${archiveName}`;
const checksumsUrl = `${releaseBaseUrl}/checksums.txt`;
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

class FatalInstallError extends Error {
  constructor(message) {
    super(message);
    this.name = 'FatalInstallError';
  }
}

const unlinkIfExists = (filePath) => {
  try {
    fs.unlinkSync(filePath);
  } catch (err) {
    if (err.code !== 'ENOENT') {
      throw err;
    }
  }
};

const responseText = async (res) => {
  const chunks = [];
  for await (const chunk of res) {
    chunks.push(Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString('utf8');
};

const checksumForArchive = (checksumsText, name) => {
  for (const rawLine of checksumsText.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) {
      continue;
    }
    const match = line.match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (match && path.basename(match[2].trim()) === name) {
      return match[1].toLowerCase();
    }
  }
  return null;
};

const sha256File = (filePath) =>
  new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    fs.createReadStream(filePath)
      .on('data', (chunk) => hash.update(chunk))
      .on('error', reject)
      .on('end', () => resolve(hash.digest('hex')));
  });

const verifyArchiveChecksum = async (filePath, name, checksumsText) => {
  const expected = checksumForArchive(checksumsText, name);
  if (!expected) {
    throw new FatalInstallError(`checksum entry not found for ${name}`);
  }

  const actual = await sha256File(filePath);
  if (actual !== expected) {
    unlinkIfExists(filePath);
    throw new FatalInstallError(
      `checksum mismatch for ${name}: expected ${expected}, got ${actual}`,
    );
  }
};

const install = async () => {
  try {
    console.log(`optiqor: downloading binary for ${key}...`);
    const res = await get(url);
    await pipelineP(res, fs.createWriteStream(tarballPath));
    const checksums = await responseText(await get(checksumsUrl));
    await verifyArchiveChecksum(tarballPath, archiveName, checksums);
    // tar -xzf using system tar (avoids adding tar npm dep).
    execFileSync('tar', ['-xzf', tarballPath, '-C', vendorDir], { stdio: 'inherit' });
    unlinkIfExists(tarballPath);
    console.log('optiqor: ready. Run `optiqor --version` to verify.');
  } catch (err) {
    unlinkIfExists(tarballPath);
    console.error('optiqor: failed to install binary:', err.message);
    if (err instanceof FatalInstallError) {
      console.error('optiqor: refusing to use an unverified release archive.');
      process.exit(1);
    }
    console.error('optiqor: this is non-fatal — build from source if needed:');
    console.error('  go install github.com/optiqor/optiqor-cli/cmd/optiqor@latest');
    // Exit 0 so npm install does not abort entirely.
    process.exit(0);
  }
};

if (require.main === module) {
  install();
}

module.exports = {
  checksumForArchive,
  sha256File,
  verifyArchiveChecksum,
  FatalInstallError,
};
