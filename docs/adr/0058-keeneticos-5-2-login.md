---
status: adopted
---

# ADR 0058: sign in to KeeneticOS 5.2 with its newer login

This decision extends the device session of
[ADR 0008](0008-device-deployment-boundary.md). The deployment lifecycle,
ownership and credential storage of later ADRs are unchanged.

## Context

Every Keenetic deployer signs in before its first RCI call. Until now it spoke
one login, `x-ndw2-interactive`: `GET /auth` returns a realm and a challenge in
`X-NDM-Realm` and `X-NDM-Challenge`, and `POST /auth` sends
`sha256(challenge + md5(login:realm:password))`.

KeeneticOS 5.2 offers a second login. Its `GET /auth` answers `401` with two
`WWW-Authenticate` values:

```text
WWW-Authenticate: x-ndw2-interactive realm="Keenetic Racer" challenge="…" session_id="…" session_cookie="…"
WWW-Authenticate: x-ndw4-interactive endpoint="/auth" data="e30="
```

On a Keenetic Racer (KN-4010) running 5.02.B.1.0-3, the 5.2 preview channel,
every deployment failed at the probe with `authentication status 401`, although
the same account signed in through the router's web interface. Forcing that web
interface onto the older login reproduced the `401` with the correct password;
letting it use the newer one signed in. The router therefore refuses the older
login for an account it holds in the newer form, and Routevane, which spoke only
the older login, could not reach the device at all.

No vendor documentation of the newer login was found. The exchange below is
reproduced from the router's own web interface. A question-and-answer post on
the Keenetic community forum, attributed to the firmware developers and
describing the 5.2 public alpha, says the older login is to be removed and that
third-party applications are meant to use an access token sent in `X-NDMA-TKN`,
obtained through a browser redirect to `/auth`; its format and bootstrap were not
documented when this decision was made.

## Decision

- **The newer login is preferred whenever it is offered.** When a
  `WWW-Authenticate` value names `x-ndw4-interactive`, the session uses it at the
  endpoint that value names. Firmware that does not offer it keeps the older
  login unchanged.
- **The exchange is the one the router's web interface performs.**
  1. `POST {"login", "nonce"}`, where the nonce is 16 random bytes in Base64. The
     device answers `401` with `X-NDM-Data`: Base64 of a JSON object with `salt`,
     `nonce` (the server nonce), `iter` and `memcost`.
  2. The client derives a 64-byte key with Argon2id (the password, the 16-byte
     salt, `iter` passes, `memcost` KiB, one lane), then
     `ClientKey = HMAC-SHA3-512(key, "NDW4 Interactive Client Key")`,
     `StoredKey = SHA3-512(ClientKey)` and
     `ServerKey = HMAC-SHA3-512(key, "NDW4 Interactive Server Key")`. Both
     proofs sign the transcript
     `login1=L,nonce1=N1;iter2=I,memcost2=M,nonce2=SN,salt2=S;login3=L,nonce3=SN`,
     with every value as the device sent it.
  3. `POST {"login", "nonce": SN, "proof"}` with
     `ClientKey XOR HMAC-SHA3-512(StoredKey, transcript)`. The device answers
     `401` with `X-NDM-Data` carrying its `signature`.
  4. The client requires the signature to equal
     `HMAC-SHA3-512(ServerKey, transcript)`. A device that cannot produce it does
     not know the password, and the login stops there.
  5. `POST {"login", "nonce": SN, "signature-proof"}` with
     `ClientKey XOR HMAC-SHA3-512(StoredKey, transcript + ";signature4=" + signature)`.
     `200` completes the login; the session cookie carries the RCI calls.
- **The older login is a fallback only before the proof.** If the newer exchange
  stops before anything derived from the password is sent (its first answer is
  not `401`, or its data or endpoint is unusable) and the device also offers the
  older challenge, the older login runs on a fresh challenge, as the web
  interface does. Once the proof has been sent, a refusal is final. Here the
  deployer is stricter than the web interface, which retries the older login
  after any failure except a wrong signature: a retry adds a failed attempt to
  the device's counter, and a 5.2 router may refuse the older login anyway.
- **The device's answers are bounded before any key is derived.** `X-NDM-Data`
  is at most 4 KiB; the salt is exactly 16 bytes; the server nonce is 1 to 256
  printable ASCII characters without `,` or `;`, so it cannot rewrite the signed
  fields; `iter` is 1 to 10 and `memcost` 8 to 65,536 KiB, each a plain JSON
  integer because the transcript signs its exact text; the signature is 64
  bytes; and the endpoint is a path on the same device. The router's own values
  are 2 passes over 8 KiB. The ceilings keep a hostile answer from spending this
  computer's memory and keep the exchange inside the session cookie, which the
  device sets for 20 seconds from the challenge.
- **The session keeps the latest value of each cookie.** A device that renews its
  session cookie during the login now replaces the earlier value instead of
  receiving both, and an expired cookie is dropped.
- **Dependencies.** Argon2id comes from `golang.org/x/crypto`, which was already
  in the module graph and is now a direct requirement; SHA3-512 and HMAC come
  from the standard library. No `gosec` exception is added.

## Alternatives

- **Keep only the older login.** Devices on 5.2 that refuse it cannot be reached,
  and the older login is announced for removal.
- **An access token in `X-NDMA-TKN`.** It would remove the stored router password,
  but neither the token nor its browser bootstrap had a published specification.
  It remains the candidate replacement once one exists.

## Consequences and verification limits

Routevane signs in to both firmware generations with the same saved password; the
operator changes nothing. A wrong password on 5.2 costs one failed attempt rather
than two.

The exchange is verified against a known-answer vector produced by an independent
implementation (Node's OpenSSL-backed Argon2id, HMAC and SHA3-512), against a
device double that verifies proofs from a stored key, through the command-line
deployment end to end, and against one physical KN-4010 on 5.02.B.1.0-3. Stable
5.2 releases, other models and an account that the newer login does not know are
unverified. Because the exchange has no vendor specification, a later firmware
may change it; the deployer then fails at the probe without touching the device.
