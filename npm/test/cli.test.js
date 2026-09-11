'use strict'

const assert = require('node:assert')
const path = require('node:path')
const { execFileSync } = require('node:child_process')
const { test } = require('node:test')

const cli = path.join(__dirname, '..', 'bin', 'sdlc-install.js')

function run(args, expectFailure = false) {
  try {
    const out = execFileSync(process.execPath, [cli, ...args], { encoding: 'utf8', stdio: 'pipe' })
    assert.ok(!expectFailure, `expected ${args.join(' ')} to fail`)
    return out
  } catch (err) {
    assert.ok(expectFailure, `expected ${args.join(' ')} to succeed: ${err.stderr || err.message}`)
    return err.stderr
  }
}

test('--help explains the one thing this package does', () => {
  const out = run(['--help'])
  assert.match(out, /npx @bbsnly\/sdlc install/)
  assert.match(out, /--version X\.Y\.Z/)
  assert.match(out, /--dir PATH/)
})

// Nothing here should reach the network to answer a question about arguments.
test('an unknown option is refused with the usage, not a stack trace', () => {
  const out = run(['--nope'], true)
  assert.match(out, /unknown option --nope/)
  assert.match(out, /npx @bbsnly\/sdlc install/)
  assert.doesNotMatch(out, /at Object\./)
})

test('an option with no value is refused rather than guessed at', () => {
  assert.match(run(['--version'], true), /--version needs a value/)
  assert.match(run(['--dir'], true), /--dir needs a value/)
})

test('the package reports its own version', () => {
  const declared = require('../package.json').version
  assert.strictEqual(run(['--print-version']).trim(), declared)
})
