'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const { createRequire } = require('node:module');
const path = require('node:path');
const { test } = require('node:test');
const webRequire = createRequire(path.join(__dirname, '../web/package.json'));
const boundariesRequire = createRequire(webRequire.resolve('eslint-plugin-boundaries'));
const micromatch = boundariesRequire('micromatch');
const braces = createRequire(boundariesRequire.resolve('micromatch'))('braces');

test('brace patterns preserve compilation, expansion, escaping and range semantics', () => {
  assert.deepEqual(braces('src/{features,entities}/**/*.{ts,vue}', { expand: true }), [
    'src/features/**/*.ts', 'src/features/**/*.vue',
    'src/entities/**/*.ts', 'src/entities/**/*.vue',
  ]);
  assert.equal(braces.compile('a/{b,c}/d'), 'a/(b|c)/d');
  assert.deepEqual(braces.expand('{01..03}'), ['01', '02', '03']);
  assert.equal(braces.stringify(braces.parse('a/\\{b,c\\}/d')), 'a/{b,c}/d');
});

test('excessive string depth is refused intentionally before stack exhaustion', () => {
  const allowed = '{'.repeat(100) + 'a,b' + '}'.repeat(100);
  assert.doesNotThrow(() => braces.compile(allowed));
  const hostile = '{'.repeat(101) + 'a,b' + '}'.repeat(101);
  for (const run of [braces, braces.parse, braces.compile, braces.expand, braces.stringify]) {
    assert.throws(() => run(hostile), { name: 'SyntaxError', message: /max depth/ });
  }
  assert.throws(() => braces.compile(hostile, { maxDepth: Infinity }), /max depth/);
  assert.throws(() => braces.compile('{a,b}', { maxDepth: -1 }), RangeError);
});

test('direct AST recursion and cyclic children cannot bypass the depth limit', () => {
  const nested = () => {
    let node = { type: 'text', value: 'x' };
    for (let index = 0; index < 102; index++) node = { type: 'root', nodes: [node] };
    return node;
  };
  for (const run of [braces.compile, braces.stringify, braces.expand]) {
    assert.throws(() => run(nested()), { name: 'RangeError', message: /max depth/ });
    const cycle = { type: 'root', nodes: [] };
    cycle.nodes.push(cycle);
    assert.throws(() => run(cycle), { name: 'RangeError', message: /max depth/ });
  }
});

test('real micromatch consumers retain callable, matching, capture and scan APIs', () => {
  assert.deepEqual(micromatch(['a.ts', 'a.css', 'a.vue'], ['*.{ts,vue}', '!*.css']), ['a.ts', 'a.vue']);
  assert.equal(micromatch.isMatch('src/features/a.ts', ['src/entities/**', 'src/features/**']), true);
  assert.deepEqual(micromatch.capture('src/*/*.ts', 'src/features/a.ts'), ['features', 'a']);
  assert.equal(micromatch.makeRe('*.{ts,vue}').test('a.vue'), true);
  assert.equal(micromatch.scan('src/**/*.vue').isGlob, true);
});

test('installed forge consumer resolves the upstream release the manifest pins', () => {
  const listhenRequire = createRequire(webRequire.resolve('listhen'));
  const installedRoot = path.dirname(path.dirname(listhenRequire.resolve('node-forge')));
  assert.equal(fs.lstatSync(installedRoot).isSymbolicLink(), false);
  const installed = JSON.parse(fs.readFileSync(path.join(installedRoot, 'package.json')));
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, '../web/package.json')));
  assert.equal(installed.name, 'node-forge');
  assert.equal(installed.version, manifest.overrides['node-forge']);
  const forge = listhenRequire('node-forge');
  assert.equal(forge.md.sha256.create().update('hello world').digest().toHex(),
    'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9');
});
