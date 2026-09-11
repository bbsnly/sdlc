#!/usr/bin/env node
'use strict'

const { install, Failure } = require('../lib/install.js')
const pkg = require('../package.json')

const USAGE = `Install sdlc, the story-driven delivery loop for Claude Code.

  npx @bbsnly/sdlc install [--version X.Y.Z] [--dir PATH]

  --version X.Y.Z   install that version instead of the latest
  --dir PATH        install somewhere other than the default
                    (~/.local/bin, or %LOCALAPPDATA%\\Programs\\sdlc\\bin)
  -h, --help        this

The same settings can come from the environment: SDLC_VERSION and
SDLC_INSTALL_DIR.

This package installs a native binary and then gets out of the way: what ends
up on your PATH is sdlc itself, not a Node wrapper around it.`

function parse(argv) {
  const options = {}
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === 'install') continue // `npx @bbsnly/sdlc install` reads better than the bare form
    if (arg === '-h' || arg === '--help') return { help: true }
    if (arg === '-v' || arg === '--print-version') return { printVersion: true }
    const [flag, inline] = arg.includes('=') ? [arg.slice(0, arg.indexOf('=')), arg.slice(arg.indexOf('=') + 1)] : [arg, null]
    const value = inline !== null ? inline : argv[++i]
    if (flag === '--version') options.version = value
    else if (flag === '--dir') options.dir = value
    else return { unknown: arg }
    if (value === undefined) return { missing: flag }
  }
  return options
}

async function main() {
  const options = parse(process.argv.slice(2))
  if (options.help) {
    console.log(USAGE)
    return
  }
  if (options.printVersion) {
    console.log(pkg.version)
    return
  }
  if (options.unknown) {
    console.error(`sdlc: unknown option ${options.unknown}\n`)
    console.error(USAGE)
    process.exitCode = 1
    return
  }
  if (options.missing) {
    console.error(`sdlc: ${options.missing} needs a value\n`)
    console.error(USAGE)
    process.exitCode = 1
    return
  }
  await install(options)
}

main().catch((err) => {
  if (err instanceof Failure) {
    console.error(`sdlc: ${err.message}`)
    for (const line of err.advice) console.error(`  ${line}`)
  } else {
    console.error(`sdlc: ${err && err.message ? err.message : err}`)
  }
  process.exitCode = 1
})
