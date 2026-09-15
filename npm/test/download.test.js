'use strict'

const assert = require('node:assert')
const { execFile } = require('node:child_process')
const fs = require('node:fs')
const http = require('node:http')
const os = require('node:os')
const path = require('node:path')
const { test } = require('node:test')
const { promisify } = require('node:util')

const { download, install, Failure } = require('../lib/install.js')

// serve answers with body one byte every `gap` milliseconds or, given no body,
// sends the headers and then nothing at all.
async function serve(t, body, gap) {
  const server = http.createServer((req, res) => {
    res.writeHead(200, { 'content-type': 'application/octet-stream' })
    if (!body) return
    let sent = 0
    const next = () => {
      if (sent === body.length) return res.end()
      res.write(body.subarray(sent, ++sent))
      setTimeout(next, gap)
    }
    next()
  })
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  t.after(() => {
    server.closeAllConnections()
    server.close()
  })
  return `http://127.0.0.1:${server.address().port}/sdlc.tar.gz`
}

function target(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sdlc-download-'))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  return path.join(dir, 'sdlc.tar.gz')
}

// A deadline on the whole download failed every link too slow to finish inside
// it, however steadily the bytes arrived: here the download takes three times
// the limit, and no gap comes near it.
test('a slow download that keeps arriving is not cut off', { timeout: 10_000 }, async (t) => {
  const body = Buffer.from('0123456789abcdefghijklmnopqrst')
  const url = await serve(t, body, 50)
  const into = target(t)
  await download(url, into, 500)
  assert.deepStrictEqual(fs.readFileSync(into), body)
})

test('a download that goes quiet fails, and says why', { timeout: 10_000 }, async (t) => {
  const url = await serve(t, null, 0)
  const into = target(t)
  await assert.rejects(download(url, into, 200), (err) => {
    assert.ok(err instanceof Failure)
    assert.match(err.message, /could not download sdlc\.tar\.gz/)
    assert.ok(err.advice.some((line) => line.includes('sent nothing for 0.2 seconds')))
    return true
  })
  assert.ok(!fs.existsSync(into))
})

// The limit is a timer, and a timer still pending keeps Node running: the
// installer would finish and then sit there for the rest of the limit. Run in
// a process of its own, so that how long the process lasts can be measured.
test('a finished download leaves nothing behind to keep the installer running', { timeout: 20_000 }, async (t) => {
  const url = await serve(t, Buffer.from('sdlc'), 0)
  const into = target(t)
  const module = path.join(__dirname, '..', 'lib', 'install.js')
  const script = `require(${JSON.stringify(module)}).download(${JSON.stringify(url)}, ${JSON.stringify(into)}, 8000)`
  const started = Date.now()
  await promisify(execFile)(process.execPath, ['-e', script], { timeout: 15_000 })
  const lasted = Date.now() - started
  assert.ok(lasted < 4000, `the process lasted ${lasted} ms after a download that takes almost none`)
  assert.strictEqual(fs.readFileSync(into, 'utf8'), 'sdlc')
})

// A server that answers is not an unreachable one: a release that has no such
// file is a different problem with a different fix.
test('a download the server refuses says what it answered', { timeout: 10_000 }, async (t) => {
  const server = http.createServer((req, res) => {
    res.writeHead(404, 'Not Found')
    res.end()
  })
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  t.after(() => server.close())
  const url = `http://127.0.0.1:${server.address().port}/sdlc.tar.gz`
  await assert.rejects(download(url, target(t), 5000), (err) => {
    assert.ok(err instanceof Failure)
    assert.ok(err.advice.some((line) => line.includes('answered 404')), err.advice.join('\n'))
    assert.ok(!err.advice.some((line) => line.includes('could not be reached')))
    return true
  })
})

// closedPort is a local port nothing is listening on.
async function closedPort() {
  const server = http.createServer()
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  const { port } = server.address()
  await new Promise((resolve) => server.close(resolve))
  return port
}

// fetch reports every network failure as "fetch failed", and the installer
// passed that on as all it had to say -- behind a proxy it does not use, too.
test('a download that cannot connect says why, and what to do behind a proxy', { timeout: 30_000 }, async (t) => {
  const url = `http://127.0.0.1:${await closedPort()}/sdlc.tar.gz`
  await assert.rejects(download(url, target(t), 20_000), (err) => {
    assert.ok(err instanceof Failure)
    assert.ok(err.advice.some((line) => line.includes('ECONNREFUSED')), err.advice.join('\n'))
    assert.ok(err.advice.some((line) => line.includes('HTTPS_PROXY')))
    return true
  })
})

test('looking up the latest version without a connection says why', { timeout: 30_000 }, async (t) => {
  const saved = { SDLC_DOWNLOAD_BASE: process.env.SDLC_DOWNLOAD_BASE, SDLC_VERSION: process.env.SDLC_VERSION }
  t.after(() => {
    for (const [name, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[name]
      else process.env[name] = value
    }
  })
  process.env.SDLC_DOWNLOAD_BASE = `http://127.0.0.1:${await closedPort()}/releases/download`
  delete process.env.SDLC_VERSION
  await assert.rejects(install({ dir: path.dirname(target(t)), log: () => {} }), (err) => {
    assert.ok(err instanceof Failure)
    assert.match(err.message, /could not work out the latest version/)
    assert.ok(err.advice.some((line) => line.includes('ECONNREFUSED')), err.advice.join('\n'))
    return true
  })
})
