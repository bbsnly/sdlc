'use strict'

// Download the sdlc release for this machine, check it against the release's
// checksums, and put the binary somewhere on PATH.
//
// Two decisions shape this file, and both are deliberate.
//
// There is no postinstall script. Installing a native binary is something you
// asked for -- `npx @bbsnly/sdlc install` -- not something that happens while
// you were installing something else. It also means `--ignore-scripts`, which
// many organisations set, changes nothing here.
//
// What lands on your PATH is the native binary, never a Node wrapper around
// it. sdlc runs as a Claude Code hook, once per matching tool call; a Node
// process start costs around 45 ms and the binary's own start costs under 4,
// so a wrapper would be the most expensive thing in the loop.

const crypto = require('node:crypto')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const { execFileSync } = require('node:child_process')

const REPO = 'bbsnly/sdlc'

// SDLC_DOWNLOAD_BASE points this at somewhere other than GitHub. It exists so
// that this installer can be tested against a local mirror on every commit,
// rather than only by a real release.
const downloadBase = () =>
  process.env.SDLC_DOWNLOAD_BASE || `https://github.com/${REPO}/releases/download`

class Failure extends Error {
  constructor(what, advice = []) {
    super(what)
    this.advice = advice
  }
}

// assetFor names the release archive for one platform. The names are built
// here rather than discovered, so a missing build is a clear "not released
// for this platform" instead of a silent fallback to the wrong one.
function assetFor(platform, arch, version) {
  const goos = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[platform]
  const goarch = { x64: 'amd64', arm64: 'arm64' }[arch]
  if (!goos || !goarch) {
    throw new Failure(`there is no prebuilt sdlc for ${platform}/${arch}.`, [
      'Build from source instead:',
      `  go install github.com/${REPO}/cmd/sdlc@latest`,
    ])
  }
  const ext = goos === 'windows' ? 'zip' : 'tar.gz'
  return {
    archive: `sdlc_${version}_${goos}_${goarch}.${ext}`,
    binary: goos === 'windows' ? 'sdlc.exe' : 'sdlc',
  }
}

// defaultDir is the same place install.sh and install.ps1 use, so the three
// routes to a working sdlc all end in one file rather than three.
function defaultDir(platform = process.platform, env = process.env) {
  if (env.SDLC_INSTALL_DIR) return env.SDLC_INSTALL_DIR
  if (platform === 'win32') {
    return path.join(env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local'), 'Programs', 'sdlc', 'bin')
  }
  return path.join(os.homedir(), '.local', 'bin')
}

// parseChecksums reads the `sha256  filename` lines goreleaser writes. The
// asterisk is what some sha256sum implementations put before a name read in
// binary mode; it is not part of the name.
function parseChecksums(text) {
  const out = new Map()
  for (const line of text.split('\n')) {
    const match = line.trim().match(/^([0-9a-f]{64})\s+\*?(.+)$/i)
    if (match) out.set(match[2].trim(), match[1].toLowerCase())
  }
  return out
}

function onPath(dir, env = process.env) {
  const entries = (env.PATH || '').split(path.delimiter).filter(Boolean)
  const same = (a, b) =>
    process.platform === 'win32' ? a.toLowerCase() === b.toLowerCase() : a === b
  return entries.some((entry) => same(path.resolve(entry), path.resolve(dir)))
}

async function download(url, into) {
  // Without a deadline a stalled connection hangs `npx` indefinitely, with
  // nothing on screen after "downloading". Sixty seconds is long enough for a
  // slow link and short enough to be a failure rather than a hang.
  const response = await fetch(url, { redirect: 'follow', signal: AbortSignal.timeout(60_000) })
  if (!response.ok) {
    throw new Failure(`could not download ${path.basename(into)}`, [
      `why  ${url} answered ${response.status} ${response.statusText}`,
      'fix  check that this version is released, and that this machine can reach github.com',
    ])
  }
  fs.writeFileSync(into, Buffer.from(await response.arrayBuffer()))
}

// The "latest" lookup is the sibling of the download path, so a mirror that
// serves one serves the other. That is what lets the resolve-the-latest-version
// path -- the one every reader of the documentation takes -- be tested at all.
function latestURL() {
  return downloadBase().replace(/\/download$/, '/latest')
}

async function latestVersion() {
  // Through the redirect rather than the API: the API is rate limited per IP,
  // and a shared network can exhaust it for everyone on it.
  const url = latestURL()
  const response = await fetch(url, { redirect: 'manual', signal: AbortSignal.timeout(60_000) })
  const location = response.headers.get('location') || ''
  const match = location.match(/\/tag\/v([0-9][^/]*)$/)
  if (!match) {
    throw new Failure('could not work out the latest version of sdlc.', [
      `why  ${url} did not redirect to a tag;`,
      '     the usual cause is no network, or no release yet',
      'fix  pass one: npx @bbsnly/sdlc install --version X.Y.Z',
    ])
  }
  return match[1]
}

// checksumsFor prefers the copy published inside this package: it was written
// when the release was built and cannot be changed afterwards, which a file
// fetched now, from the same place as the download, cannot promise.
//
// That copy describes exactly one release, though, and --version or
// SDLC_VERSION can ask for another. When the archive being installed is not
// one it lists, the pins for that version are fetched instead -- treating the
// embedded file as authoritative there would report a missing platform build
// for a platform the release certainly has. Either fallback is said out loud.
async function checksumsFor(version, archive, tmp, log) {
  const embedded = path.join(__dirname, '..', 'checksums.txt')
  if (fs.existsSync(embedded)) {
    const pins = parseChecksums(fs.readFileSync(embedded, 'utf8'))
    if (pins.has(archive)) return pins
    log(`sdlc: this package carries the pins for a different release, so the v${version} ones are being fetched`)
  } else {
    log(`sdlc: this package carries no checksums, so they are being fetched from the v${version} release`)
  }
  const fetched = path.join(tmp, 'checksums.txt')
  await download(`${downloadBase()}/v${version}/checksums.txt`, fetched)
  return parseChecksums(fs.readFileSync(fetched, 'utf8'))
}

// psQuote makes a value safe inside a single-quoted PowerShell string. The
// temporary directory sits under the user's profile, so a username with an
// apostrophe in it -- O'Brien -- would otherwise end the string early and turn
// a verified download into a parse error.
function psQuote(value) {
  return value.replace(/'/g, "''")
}

function unpack(archive, into, binary) {
  if (archive.endsWith('.zip')) {
    execFileSync('powershell', ['-NoProfile', '-NonInteractive', '-Command',
      `Expand-Archive -LiteralPath '${psQuote(archive)}' -DestinationPath '${psQuote(into)}' -Force`], { stdio: 'pipe' })
  } else {
    execFileSync('tar', ['-xzf', archive, '-C', into, binary], { stdio: 'pipe' })
  }
  return path.join(into, binary)
}

async function install(options = {}) {
  const log = options.log || console.log
  const version = (options.version || process.env.SDLC_VERSION || '').replace(/^v/, '') || (await latestVersion())
  const dir = options.dir || defaultDir()
  const { archive, binary } = assetFor(process.platform, process.arch, version)
  const target = path.join(dir, binary)

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'sdlc-'))
  let incoming = null
  try {
    log(`sdlc: downloading ${archive}`)
    const downloaded = path.join(tmp, archive)
    await download(`${downloadBase()}/v${version}/${archive}`, downloaded)

    const expected = (await checksumsFor(version, archive, tmp, log)).get(archive)
    if (!expected) {
      throw new Failure(`checksums.txt does not list ${archive}`, [
        'why  the release is missing the build for this platform',
        `fix  report it at https://github.com/${REPO}/issues`,
      ])
    }
    // Verified before anything is made executable or moved onto a PATH
    // directory. A release you cannot check is a release you should not
    // install.
    const actual = crypto.createHash('sha256').update(fs.readFileSync(downloaded)).digest('hex')
    if (actual !== expected) {
      throw new Failure('the download does not match its checksum, so it is not being installed.', [
        `expected  ${expected}`,
        `got       ${actual}`,
        'This is either a corrupted download or something worse. Try again; if it',
        `happens twice, report it at https://github.com/${REPO}/issues`,
      ])
    }

    const unpacked = unpack(downloaded, tmp, binary)
    fs.mkdirSync(dir, { recursive: true })
    fs.chmodSync(unpacked, 0o755)
    // Through a temporary name in the target's own directory and then rename:
    // a half written binary on PATH is worse than no binary on PATH, and
    // rename is the only step that is atomic.
    incoming = path.join(dir, `.sdlc.incoming.${process.pid}`)
    fs.copyFileSync(unpacked, incoming)
    fs.chmodSync(incoming, 0o755)
    fs.renameSync(incoming, target)
    incoming = null
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true })
    // The staging name is inside the install directory, which the temporary
    // directory's cleanup does not reach. A failed copy would otherwise leave
    // a partial executable there for good.
    if (incoming) fs.rmSync(incoming, { force: true })
  }

  log(`sdlc: installed v${version} in ${dir}`)
  if (!onPath(dir)) {
    log('')
    log(`  ${dir} is not on your PATH. Add it:`)
    log('')
    log(process.platform === 'win32'
      ? `    setx PATH "${dir};%PATH%"`
      : `    export PATH="${dir}:$PATH"`)
    log('')
    log(process.platform === 'win32'
      ? '  and open a new terminal for it to take effect.'
      : '  and put that line in your shell profile (~/.zshrc, ~/.bashrc) to keep it.')
  }
  log('')
  log('Next, in Claude Code:')
  log('')
  log('  /plugin marketplace add bbsnly/sdlc')
  log('  /plugin install sdlc@sdlc')
  log('')
  log('Then run `sdlc doctor` in a project to check the install.')
  return { version, dir, target }
}

module.exports = { install, assetFor, defaultDir, parseChecksums, onPath, Failure, REPO }
