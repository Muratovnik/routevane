import test from 'node:test'
import assert from 'node:assert/strict'
import http from 'node:http'
import { once } from 'node:events'
import { createTransport, externalURL, isAppURL } from '../src/transport.mjs'

test('only the internal app authority is navigable; external links exclude executable schemes', () => {
  assert.ok(isAppURL('routevane://app/lists/new#tab=contents'))
  for (const url of [
    'routevane://evil/',
    'routevane://app.evil/',
    'routevane://user@app/',
    'routevane://app:90/',
    'file:///tmp/a',
    'https://app/',
  ]) {
    assert.equal(isAppURL(url), false, url)
  }
  assert.equal(
    externalURL('https://example.com/help'),
    'https://example.com/help',
  )
  for (const url of [
    'file:///etc/passwd',
    'javascript:alert(1)',
    'cmd:run',
    'https://user:secret@example.com/',
  ]) {
    assert.equal(externalURL(url), undefined, url)
  }
})

test('private transport preserves mutation guards and blocks foreign callers without a backend request', async (t) => {
  let calls = 0
  const server = http.createServer(async (req, res) => {
    calls++
    assert.equal(req.headers['x-routevane-desktop'], 'private-credential')
    assert.equal(req.headers['x-routevane-request'], '1')
    assert.equal(
      req.headers.origin,
      `http://127.0.0.1:${server.address().port}`,
    )
    assert.equal(req.headers.cookie, undefined)
    let body = ''
    for await (const part of req) body += part
    assert.equal(body, '{"name":"test"}')
    res.writeHead(201, { 'Content-Type': 'application/json' })
    res.end('{"saved":true}')
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  t.after(() => {
    server.closeAllConnections()
    server.close()
  })
  const transport = createTransport(
    `http://127.0.0.1:${server.address().port}`,
    'private-credential',
  )
  const result = await transport(
    new Request('routevane://app/v1/lists', {
      method: 'POST',
      body: '{"name":"test"}',
      headers: {
        Origin: 'routevane://app',
        'Content-Type': 'application/json',
        'X-Routevane-Request': '1',
        Cookie: 'untrusted',
      },
    }),
  )
  assert.equal(result.status, 201)
  assert.deepEqual(await result.json(), { saved: true })
  for (const request of [
    new Request('https://evil.test/v1/lists'),
    new Request('routevane://app/v1/lists', {
      headers: { Origin: 'https://evil.test' },
    }),
    new Request('routevane://app/v1/lists', { method: 'DELETE' }),
  ])
    assert.equal((await transport(request)).status, 403)
  assert.equal(calls, 1)
})
