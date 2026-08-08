const assert = require('assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const test = require('node:test');

const {
  FatalInstallError,
  checksumForArchive,
  sha256File,
  verifyArchiveChecksum,
} = require('./postinstall');

const withTempFile = (contents) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'optiqor-postinstall-'));
  const filePath = path.join(dir, 'archive.tar.gz');
  fs.writeFileSync(filePath, contents);
  return { dir, filePath };
};

test('checksumForArchive returns the SHA-256 for the exact archive name', () => {
  const checksums = [
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  optiqor_1.2.3_linux_amd64.tar.gz',
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  optiqor_1.2.3_darwin_arm64.tar.gz',
  ].join('\n');

  assert.equal(
    checksumForArchive(checksums, 'optiqor_1.2.3_darwin_arm64.tar.gz'),
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  );
});

test('checksumForArchive supports star-prefixed checksum filenames', () => {
  const checksums =
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc *optiqor_1.2.3_linux_arm64.tar.gz';

  assert.equal(
    checksumForArchive(checksums, 'optiqor_1.2.3_linux_arm64.tar.gz'),
    'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
  );
});

test('verifyArchiveChecksum accepts a matching archive hash', async () => {
  const { dir, filePath } = withTempFile('trusted archive');
  try {
    const hash = await sha256File(filePath);
    await verifyArchiveChecksum(filePath, 'archive.tar.gz', `${hash}  archive.tar.gz`);
    assert.equal(fs.existsSync(filePath), true);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('verifyArchiveChecksum deletes a mismatched archive and fails loudly', async () => {
  const { dir, filePath } = withTempFile('tampered archive');
  try {
    await assert.rejects(
      verifyArchiveChecksum(
        filePath,
        'archive.tar.gz',
        'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd  archive.tar.gz',
      ),
      FatalInstallError,
    );
    assert.equal(fs.existsSync(filePath), false);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('verifyArchiveChecksum fails loudly when the archive is missing from checksums.txt', async () => {
  const { dir, filePath } = withTempFile('trusted archive');
  try {
    await assert.rejects(
      verifyArchiveChecksum(
        filePath,
        'archive.tar.gz',
        'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee  other.tar.gz',
      ),
      FatalInstallError,
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
