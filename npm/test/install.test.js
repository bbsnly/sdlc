'use strict'

const assert = require('node:assert')
const path = require('node:path')
const { test } = require('node:test')

const { assetFor, defaultDir, parseChecksums, onPath, Failure } = require('../lib/install.js')

test('an archive name is built the same way the release builds it', () => {
  assert.deepStrictEqual(assetFor('darwin', 'arm64', '1.2.3'), {
    archive: 'sdlc_1.2.3_darwin_arm64.tar.gz',
    binary: 'sdlc',
  })
  assert.deepStrictEqual(assetFor('linux', 'x64', '1.2.3'), {
    archive: 'sdlc_1.2.3_linux_amd64.tar.gz',
    binary: 'sdlc',
  })
  assert.deepStrictEqual(assetFor('win32', 'x64', '1.2.3'), {
    archive: 'sdlc_1.2.3_windows_amd64.zip',
    binary: 'sdlc.exe',
  })
})

// Guessing at an archive name for a platform that was never built would turn
// "not released for your machine" into a download failure nobody can read.
test('a platform with no build says so, and says what to do instead', () => {
  assert.throws(() => assetFor('freebsd', 'x64', '1.2.3'), (err) => {
    assert.ok(err instanceof Failure)
    assert.match(err.message, /no prebuilt sdlc for freebsd\/x64/)
    assert.ok(err.advice.some((line) => line.includes('go install')))
    return true
  })
  assert.throws(() => assetFor('linux', 'mips', '1.2.3'), Failure)
})

test('the install directory follows the environment first', () => {
  assert.strictEqual(defaultDir('linux', { SDLC_INSTALL_DIR: '/opt/bin' }), '/opt/bin')
  assert.strictEqual(defaultDir('win32', { SDLC_INSTALL_DIR: 'C:\\bin' }), 'C:\\bin')
})

test('the install directory is the same one the shell installers use', () => {
  assert.ok(defaultDir('linux', {}).endsWith(path.join('.local', 'bin')))
  assert.strictEqual(
    defaultDir('win32', { LOCALAPPDATA: 'C:\\Users\\dev\\AppData\\Local' }),
    path.join('C:\\Users\\dev\\AppData\\Local', 'Programs', 'sdlc', 'bin'),
  )
})

test('checksums are read the way goreleaser writes them', () => {
  const sums = parseChecksums([
    'a'.repeat(64) + '  sdlc_1.2.3_darwin_arm64.tar.gz',
    'b'.repeat(64) + ' *sdlc_1.2.3_windows_amd64.zip',
    '',
    '# a comment nobody writes but nobody should crash on',
  ].join('\n'))
  assert.strictEqual(sums.get('sdlc_1.2.3_darwin_arm64.tar.gz'), 'a'.repeat(64))
  assert.strictEqual(sums.get('sdlc_1.2.3_windows_amd64.zip'), 'b'.repeat(64))
  assert.strictEqual(sums.size, 2)
})

test('a directory already on PATH is not advertised again', () => {
  const dir = path.resolve('/tmp/sdlc-bin')
  assert.strictEqual(onPath(dir, { PATH: ['/usr/bin', dir].join(path.delimiter) }), true)
  assert.strictEqual(onPath(dir, { PATH: '/usr/bin' }), false)
  assert.strictEqual(onPath(dir, {}), false)
})
