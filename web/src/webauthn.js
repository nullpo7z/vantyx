/**
 * @file Browser-side WebAuthn helpers: convert the server's JSON options
 * (base64url) to the ArrayBuffers navigator.credentials wants and the
 * resulting credential back to JSON.
 */

export function webauthnSupported() {
  return typeof window !== 'undefined' && !!window.PublicKeyCredential && !!navigator.credentials
}

function b64ToBuf(s) {
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const bin = window.atob((s + pad).replaceAll('-', '+').replaceAll('_', '/'))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out.buffer
}

function bufToB64(buf) {
  const bytes = new Uint8Array(buf)
  let bin = ''
  bytes.forEach((b) => (bin += String.fromCharCode(b)))
  return window.btoa(bin).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
}

function decodeCreationOptions(o) {
  const pk = { ...o.publicKey }
  pk.challenge = b64ToBuf(pk.challenge)
  pk.user = { ...pk.user, id: b64ToBuf(pk.user.id) }
  if (pk.excludeCredentials) pk.excludeCredentials = pk.excludeCredentials.map((c) => ({ ...c, id: b64ToBuf(c.id) }))
  return { publicKey: pk }
}

function decodeRequestOptions(o) {
  const pk = { ...o.publicKey }
  pk.challenge = b64ToBuf(pk.challenge)
  if (pk.allowCredentials) pk.allowCredentials = pk.allowCredentials.map((c) => ({ ...c, id: b64ToBuf(c.id) }))
  return { publicKey: pk }
}

/** Run navigator.credentials.create() and return the JSON the server expects. */
export async function createPasskey(options) {
  const cred = await navigator.credentials.create(decodeCreationOptions(options))
  const r = cred.response
  return {
    id: cred.id,
    rawId: bufToB64(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment || undefined,
    response: {
      clientDataJSON: bufToB64(r.clientDataJSON),
      attestationObject: bufToB64(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : [],
    },
  }
}

/** Run navigator.credentials.get() and return the JSON the server expects. */
export async function getPasskeyAssertion(options) {
  const cred = await navigator.credentials.get(decodeRequestOptions(options))
  const r = cred.response
  return {
    id: cred.id,
    rawId: bufToB64(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment || undefined,
    response: {
      clientDataJSON: bufToB64(r.clientDataJSON),
      authenticatorData: bufToB64(r.authenticatorData),
      signature: bufToB64(r.signature),
      userHandle: r.userHandle ? bufToB64(r.userHandle) : null,
    },
  }
}
