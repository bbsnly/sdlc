'use strict'

const assert = require('node:assert')
const { test } = require('node:test')

const { pathHint } = require('../lib/install.js')

// [Environment]::GetEnvironmentVariable returns %JAVA_HOME%\bin already
// expanded, so the line the installer printed for Windows wrote every such
// entry back frozen at today's value -- which install.ps1 goes out of its way
// not to do.
test('the Windows PATH line keeps the entries that name a variable', () => {
  const hint = pathHint('win32', 'C:\\Users\\ann\\AppData\\Local\\Programs\\sdlc\\bin').join('\n')
  assert.match(hint, /DoNotExpandEnvironmentNames/)
  assert.match(hint, /'ExpandString'/)
  assert.doesNotMatch(hint, /GetEnvironmentVariable/)
  assert.match(hint, /'C:\\Users\\ann\\AppData\\Local\\Programs\\sdlc\\bin;'/)
  // A registry write tells nobody; this is what makes a new terminal see it.
  assert.match(hint, /SetEnvironmentVariable\('SDLC_PATH_REFRESH', \$null, 'User'\)/)
})

test('the Windows PATH line survives an apostrophe in the directory', () => {
  const hint = pathHint('win32', "C:\\Users\\O'Brien\\bin").join('\n')
  assert.match(hint, /'C:\\Users\\O''Brien\\bin;'/)
})

test('elsewhere the PATH line is an export', () => {
  assert.deepStrictEqual(pathHint('linux', '/home/ann/.local/bin'), ['export PATH="/home/ann/.local/bin:$PATH"'])
})
