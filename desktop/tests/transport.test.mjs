import test from 'node:test'
import assert from 'node:assert/strict'
import http from 'node:http'
import { once } from 'node:events'
import { createHash } from 'node:crypto'
import { createTransport, externalURL, isAppURL } from '../src/transport.mjs'

test(
  'configuration transfer keeps the preview digest and exact document across the desktop boundary',
  { timeout: 5000 },
  async (t) => {
    const document = '{"version":"config-transfer-v1.4","profiles":[]}\n'
    const digest = `sha256:${createHash('sha256').update(document).digest('hex')}`
    const received = []
    const server = http.createServer(async (request, response) => {
      let body = ''
      for await (const chunk of request) body += chunk
      received.push({ headers: request.headers, body, path: request.url })
      response.writeHead(
        request.headers['x-routevane-transfer-digest'] === digest ? 200 : 409,
        { 'Content-Type': 'application/json' },
      )
      response.end('{"applied":true}')
    })
    t.after(() => {
      server.closeAllConnections()
      server.close()
    })
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    const transport = createTransport(
      `http://127.0.0.1:${server.address().port}`,
      'owned-desktop-credential',
    )
    const result = await transport(
      new Request('routevane://app/v1/config-transfer/apply', {
        method: 'POST',
        headers: {
          Origin: 'routevane://app',
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
          'X-Routevane-Transfer-Digest': digest,
          'X-Routevane-Desktop': 'renderer-must-not-select-this',
          Authorization: 'Bearer untrusted',
          Cookie: 'untrusted',
        },
        body: document,
      }),
    )
    assert.equal(result.status, 200)
    await result.body.cancel()
    assert.equal(received.length, 1)
    assert.equal(received[0].path, '/v1/config-transfer/apply')
    assert.equal(received[0].body, document)
    assert.equal(received[0].headers['x-routevane-transfer-digest'], digest)
    assert.equal(
      received[0].headers['x-routevane-desktop'],
      'owned-desktop-credential',
    )
    assert.equal(received[0].headers.authorization, undefined)
    assert.equal(received[0].headers.cookie, undefined)
  },
)

test('only the internal app authority is navigable; external links exclude executable schemes', () => {
  assert.ok(isAppURL('routevane://app/profiles/new#tab=contents'))
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

test('private transport forwards POST and PUT mutations and blocks foreign callers without a backend request', async (t) => {
  let calls = 0
  const methods = []
  const server = http.createServer(async (req, res) => {
    calls++
    methods.push(req.method)
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
  for (const method of ['POST', 'PUT']) {
    const result = await transport(
      new Request('routevane://app/v1/profiles', {
        method,
        body: '{"name":"test"}',
        headers: {
          Origin: 'routevane://app',
          'Content-Type': 'application/json',
          'X-Routevane-Request': '1',
          Cookie: 'untrusted',
        },
      }),
    )
    assert.equal(result.status, 201, method)
    assert.deepEqual(await result.json(), { saved: true })
  }
  assert.deepEqual(methods, ['POST', 'PUT'])
  for (const request of [
    new Request('https://evil.test/v1/profiles'),
    new Request('routevane://app/v1/profiles', {
      headers: { Origin: 'https://evil.test' },
    }),
    new Request('routevane://app/v1/profiles', { method: 'DELETE' }),
  ])
    assert.equal((await transport(request)).status, 403)
  assert.equal(calls, 2)
})
