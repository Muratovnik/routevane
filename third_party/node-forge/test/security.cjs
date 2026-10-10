'use strict';

const assert = require('node:assert/strict');
const { test } = require('node:test');
const forge = require('..');
const vector = require('./upstream-digestalgorithm.json');

test('upstream nested DigestAlgorithm regression rejects the malformed signature', () => {
  const key = forge.pki.rsa.setPublicKey(
    new forge.jsbn.BigInteger(vector.modulus, 16),
    new forge.jsbn.BigInteger('3'),
  );
  const digest = forge.md.sha256.create().update(vector.message).digest().getBytes();
  assert.throws(() => key.verify(digest, forge.util.hexToBytes(vector.signature)),
    /ASN.1 object does not contain a valid RSASSA-PKCS1-v1_5 DigestInfo value/);
});

test('valid signatures and certificate/PKCS12 APIs remain usable', () => {
  const keys = forge.pki.rsa.generateKeyPair({ bits: 1024, e: 0x10001 });
  const md = forge.md.sha256.create().update('Routevane compatibility control');
  const signature = keys.privateKey.sign(md);
  assert.equal(keys.publicKey.verify(md.digest().getBytes(), signature), true);
  const cert = forge.pki.createCertificate();
  cert.publicKey = keys.publicKey;
  cert.serialNumber = '01';
  cert.validity.notBefore = new Date('2020-01-01');
  cert.validity.notAfter = new Date('2030-01-01');
  cert.setSubject([{ name: 'commonName', value: 'localhost' }]);
  cert.setIssuer(cert.subject.attributes);
  cert.sign(keys.privateKey, forge.md.sha256.create());
  assert.equal(cert.verify(cert), true);
  assert.equal(forge.pki.certificateFromPem(forge.pki.certificateToPem(cert)).serialNumber, '01');
  const p12 = forge.pkcs12.toPkcs12Asn1(keys.privateKey, cert, 'test-only', { algorithm: '3des' });
  assert.equal(forge.pkcs12.pkcs12FromAsn1(forge.asn1.fromDer(forge.asn1.toDer(p12)), 'test-only').safeContents.length > 0, true);
});
