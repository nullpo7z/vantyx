import API from './api.js'
import { t } from './i18n.js'
import { authMethodLabel } from './dom_helpers.js'
import { uiConfirm } from './ui_dialog.js'

function slugFromLabel(label) {
  return (label || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48)
}

function downloadTextFile(filename, content) {
  const blob = new Blob([content], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}

function keyTypeLabel(keyType) {
  const v = (keyType || '').trim()
  if (!v || v === 'UNKNOWN') return t('app.credentialKeyTypeUnknown')
  return v
}

const KEY_ICON_SVG = `<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>`
const IDENTITY_ICON_SVG = `<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 9h4M7 13h6"/></svg>`

function bindKeyDrop({ dropEl, pickBtn, inputEl, textareaEl }) {
  if (!dropEl || !textareaEl) return
  const readFile = (file) => {
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      textareaEl.value = String(reader.result || '')
    }
    reader.readAsText(file)
  }
  const setDrag = (on) => {
    dropEl.classList.toggle('ring-2', on)
    dropEl.classList.toggle('ring-sky-400', on)
  }
  dropEl.addEventListener('dragover', (e) => {
    e.preventDefault()
    setDrag(true)
  })
  dropEl.addEventListener('dragleave', () => setDrag(false))
  dropEl.addEventListener('drop', (e) => {
    e.preventDefault()
    setDrag(false)
    readFile(e.dataTransfer?.files?.[0])
  })
  if (pickBtn && inputEl) {
    pickBtn.addEventListener('click', () => inputEl.click())
    inputEl.addEventListener('change', () => {
      readFile(inputEl.files?.[0])
      inputEl.value = ''
    })
  }
}

export async function renderCredentialsPage({ mainContent, escapeHtml }) {
  mainContent.className = 'flex-1 overflow-auto p-6 flex flex-col items-center'
  mainContent.innerHTML = `
    <div class="w-full max-w-5xl flex-1 flex flex-col gap-8">
      <div>
        <h2 class="text-xl font-semibold text-slate-900">${t('app.credentialsTitle')}</h2>
        <p class="text-sm text-slate-600 mt-1">${t('app.credentialsHint')}</p>
      </div>
      <p id="cred-page-error" class="text-sm text-red-600 hidden"></p>

      <section>
        <div class="flex items-center justify-between gap-3 mb-3">
          <h3 class="text-sm font-semibold text-slate-800 uppercase tracking-wide">${t('app.sshKeysSection')}</h3>
          <div class="flex gap-2">
            <button type="button" id="cred-page-generate-key" class="rounded border border-sky-600 px-3 py-2 text-xs font-medium text-sky-700 hover:bg-sky-50 shadow-sm">${t('app.sshKeyGenerate')}</button>
            <button type="button" id="cred-page-add-key" class="rounded bg-sky-600 px-3 py-2 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('app.sshKeyAdd')}</button>
          </div>
        </div>
        <div id="cred-page-keys" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3"></div>
      </section>

      <section>
        <div class="flex items-center justify-between gap-3 mb-3">
          <h3 class="text-sm font-semibold text-slate-800 uppercase tracking-wide">${t('app.identitiesSection')}</h3>
          <button type="button" id="cred-page-add-identity" class="rounded bg-sky-600 px-3 py-2 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('app.identityAdd')}</button>
        </div>
        <div id="cred-page-identities" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3"></div>
      </section>
    </div>
  `

  const errorEl = mainContent.querySelector('#cred-page-error')
  const keysEl = mainContent.querySelector('#cred-page-keys')
  const identitiesEl = mainContent.querySelector('#cred-page-identities')

  function showError(err) {
    errorEl.textContent = err?.message || t('common.errorOccurred')
    errorEl.classList.remove('hidden')
  }
  function clearError() {
    errorEl.classList.add('hidden')
  }

  function closeDialog(dlg) {
    dlg.classList.add('hidden')
    dlg.innerHTML = ''
  }

  async function loadKeys() {
    const res = await API.sshKeys()
    return (res && res.items) || []
  }

  async function loadIdentities() {
    const res = await API.credentialIdentities()
    return (res && res.items) || []
  }

  async function renderKeys() {
    keysEl.innerHTML = `<div class="text-sm text-slate-500 col-span-full">${t('common.loading')}</div>`
    try {
      const items = await loadKeys()
      if (!items.length) {
        keysEl.innerHTML = `<div class="text-sm text-slate-500 col-span-full">${t('common.none')}</div>`
        return
      }
      keysEl.innerHTML = items
        .map(
          (k) => `
        <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm hover:border-sky-200 transition-colors">
          <div class="flex items-center gap-3">
            <div class="shrink-0 w-10 h-10 rounded-full bg-sky-500 text-white flex items-center justify-center">${KEY_ICON_SVG}</div>
            <div class="min-w-0 flex-1">
              <div class="font-medium text-slate-900 truncate">${escapeHtml(k.label || k.id)}</div>
              <div class="text-xs text-slate-500 mt-0.5">${escapeHtml(keyTypeLabel(k.key_type))}${k.has_passphrase ? ` · ${escapeHtml(t('app.authPublicKeyWithPp'))}` : ''}</div>
            </div>
            <div class="flex shrink-0 gap-1">
              <button type="button" class="cred-key-edit rounded-lg border border-slate-200 bg-white p-2 text-slate-600 hover:bg-slate-50" title="${escapeHtml(t('common.edit'))}" data-id="${escapeHtml(k.id)}" data-label="${escapeHtml(k.label || '')}">${t('common.edit')}</button>
              <button type="button" class="cred-key-del rounded-lg border border-rose-200 bg-white p-2 text-rose-600 hover:bg-rose-50" title="${escapeHtml(t('common.delete'))}" data-id="${escapeHtml(k.id)}">${t('common.delete')}</button>
            </div>
          </div>
        </div>`,
        )
        .join('')
      keysEl.querySelectorAll('.cred-key-del').forEach((btn) => {
        btn.addEventListener('click', async () => {
          if (!(await uiConfirm(t('app.sshKeyConfirmDelete'), { danger: true }))) return
          try {
            await API.deleteSSHKey(btn.dataset.id)
            await refreshAll()
          } catch (err) {
            showError(err)
          }
        })
      })
      keysEl.querySelectorAll('.cred-key-edit').forEach((btn) => {
        btn.addEventListener('click', () => showKeyEdit(btn.dataset.id, btn.dataset.label))
      })
    } catch (err) {
      showError(err)
      keysEl.innerHTML = ''
    }
  }

  async function renderIdentities() {
    identitiesEl.innerHTML = `<div class="text-sm text-slate-500 col-span-full">${t('common.loading')}</div>`
    try {
      const items = await loadIdentities()
      if (!items.length) {
        identitiesEl.innerHTML = `<div class="text-sm text-slate-500 col-span-full">${t('common.none')}</div>`
        return
      }
      identitiesEl.innerHTML = items
        .map(
          (ident) => `
        <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm hover:border-indigo-200 transition-colors">
          <div class="flex items-center gap-3">
            <div class="shrink-0 w-10 h-10 rounded-full bg-indigo-500 text-white flex items-center justify-center">${IDENTITY_ICON_SVG}</div>
            <div class="min-w-0 flex-1">
              <div class="font-medium text-slate-900 truncate">${escapeHtml(ident.label || ident.id)}</div>
              <div class="text-xs text-slate-500 mt-0.5">${escapeHtml(authMethodLabel(ident.auth_method))}</div>
            </div>
            <div class="flex shrink-0 gap-1">
              <button type="button" class="cred-ident-edit rounded-lg border border-slate-200 bg-white p-2 text-slate-600 hover:bg-slate-50" title="${escapeHtml(t('common.edit'))}"
              data-id="${escapeHtml(ident.id)}"
              data-label="${escapeHtml(ident.label || '')}"
              data-user="${escapeHtml(ident.ssh_username || '')}"
              data-key-id="${escapeHtml(ident.ssh_key_id || '')}"
              data-auth="${escapeHtml(ident.auth_method || '')}"
              data-has-password="${ident.has_password ? '1' : '0'}">${t('common.edit')}</button>
              <button type="button" class="cred-ident-del rounded-lg border border-rose-200 bg-white p-2 text-rose-600 hover:bg-rose-50" title="${escapeHtml(t('common.delete'))}" data-id="${escapeHtml(ident.id)}">${t('common.delete')}</button>
            </div>
          </div>
        </div>`,
        )
        .join('')
      identitiesEl.querySelectorAll('.cred-ident-del').forEach((btn) => {
        btn.addEventListener('click', async () => {
          if (!(await uiConfirm(t('app.identityConfirmDelete'), { danger: true }))) return
          try {
            await API.deleteCredentialIdentity(btn.dataset.id)
            await refreshAll()
          } catch (err) {
            showError(err)
          }
        })
      })
      identitiesEl.querySelectorAll('.cred-ident-edit').forEach((btn) => {
        btn.addEventListener('click', () =>
          showIdentityEdit({
            id: btn.dataset.id,
            label: btn.dataset.label,
            ssh_username: btn.dataset.user,
            ssh_key_id: btn.dataset.keyId,
            auth_method: btn.dataset.auth,
            has_password: btn.dataset.hasPassword === '1',
          }),
        )
      })
    } catch (err) {
      showError(err)
      identitiesEl.innerHTML = ''
    }
  }

  async function refreshAll() {
    clearError()
    await Promise.all([renderKeys(), renderIdentities()])
  }

  function credentialDialog() {
    return document.getElementById('credential-modal')
  }

  function showKeyAdd() {
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshKeyAdd')}</h3>
            <button type="button" id="key-add-close" class="text-2xl text-slate-500">&times;</button>
          </div>
          <form id="key-add-form" class="px-6 py-5 space-y-4">
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsName')}</label>
              <input type="text" id="key-add-label" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" required />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPrivateKey')}</label>
              <div id="key-add-drop" class="rounded border border-slate-300 p-2">
                <p class="text-xs text-slate-600 mb-2">${t('app.credentialsPrivateKeyDrop')}</p>
                <button type="button" id="key-add-pick" class="mb-2 rounded border border-slate-300 px-2 py-1 text-xs">${t('app.credentialsPrivateKeyPickFile')}</button>
                <textarea id="key-add-pem" rows="5" class="w-full rounded border border-slate-200 px-2 py-2 text-sm font-mono"></textarea>
                <input type="file" id="key-add-file" class="hidden" accept=".pem,.key,.txt,*/*" />
              </div>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPassphrase')}</label>
              <input type="password" id="key-add-pp" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" />
            </div>
            <p id="key-add-error" class="text-sm text-red-600 hidden"></p>
            <div class="flex justify-end gap-2">
              <button type="button" id="key-add-cancel" class="rounded border px-3 py-2 text-xs">${t('common.cancel')}</button>
              <button type="submit" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('common.add')}</button>
            </div>
          </form>
        </div>
      </div>`
    const close = () => closeDialog(dlg)
    const pemEl = dlg.querySelector('#key-add-pem')
    bindKeyDrop({
      dropEl: dlg.querySelector('#key-add-drop'),
      pickBtn: dlg.querySelector('#key-add-pick'),
      inputEl: dlg.querySelector('#key-add-file'),
      textareaEl: pemEl,
    })
    dlg.querySelector('#key-add-close').addEventListener('click', close)
    dlg.querySelector('#key-add-cancel').addEventListener('click', close)
    dlg.querySelector('#key-add-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = dlg.querySelector('#key-add-error')
      errEl.classList.add('hidden')
      const label = dlg.querySelector('#key-add-label').value.trim()
      const id = slugFromLabel(label) || `key-${Date.now()}`
      try {
        await API.createSSHKey({
          id,
          label,
          ssh_private_key: pemEl.value,
          ssh_private_key_passphrase: dlg.querySelector('#key-add-pp').value,
        })
        close()
        await refreshAll()
      } catch (err) {
        errEl.textContent = err.message
        errEl.classList.remove('hidden')
      }
    })
  }

  function showKeyGenerate() {
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshKeyGenerate')}</h3>
            <button type="button" id="key-gen-close" class="text-2xl text-slate-500">&times;</button>
          </div>
          <form id="key-gen-form" class="px-6 py-5 space-y-4">
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsName')}</label>
              <input type="text" id="key-gen-label" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" required />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.sshKeyGenerateType')}</label>
              <select id="key-gen-type" class="w-full rounded border border-slate-300 px-3 py-2 text-sm bg-white">
                <option value="ed25519" selected>${t('app.sshKeyGenerateTypeEd25519')}</option>
                <option value="rsa">${t('app.sshKeyGenerateTypeRsa')}</option>
              </select>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPassphrase')}</label>
              <input type="password" id="key-gen-pp" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" autocomplete="new-password" />
              <p class="text-xs text-slate-500 mt-1">${t('app.sshKeyGeneratePpHint')}</p>
            </div>
            <p id="key-gen-error" class="text-sm text-red-600 hidden"></p>
            <div class="flex justify-end gap-2">
              <button type="button" id="key-gen-cancel" class="rounded border px-3 py-2 text-xs">${t('common.cancel')}</button>
              <button type="submit" id="key-gen-submit" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('app.sshKeyGenerateSubmit')}</button>
            </div>
          </form>
        </div>
      </div>`
    const close = () => closeDialog(dlg)
    dlg.querySelector('#key-gen-close').addEventListener('click', close)
    dlg.querySelector('#key-gen-cancel').addEventListener('click', close)
    dlg.querySelector('#key-gen-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = dlg.querySelector('#key-gen-error')
      errEl.classList.add('hidden')
      const label = dlg.querySelector('#key-gen-label').value.trim()
      const id = slugFromLabel(label) || `key-${Date.now()}`
      const keyType = dlg.querySelector('#key-gen-type').value
      const passphrase = dlg.querySelector('#key-gen-pp').value
      const submitBtn = dlg.querySelector('#key-gen-submit')
      submitBtn.disabled = true
      try {
        const result = await API.generateSSHKey({ id, label, key_type: keyType, passphrase })
        showKeyGenerateResult(result)
      } catch (err) {
        errEl.textContent = err.message || t('app.sshKeyGenerateFailed')
        errEl.classList.remove('hidden')
        submitBtn.disabled = false
      }
    })
  }

  function showKeyGenerateResult(result) {
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshKeyGenerateDoneTitle')}</h3>
          </div>
          <div class="px-6 py-5 space-y-4">
            <p class="text-xs text-amber-800 bg-amber-50 border border-amber-200 rounded px-3 py-2">${t('app.sshKeyGenerateRevealWarning')}</p>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.sshKeyGeneratePrivateKeyLabel')}</label>
              <textarea id="key-gen-result-private" rows="8" readonly class="w-full rounded border border-slate-300 px-2 py-2 text-xs font-mono bg-slate-50">${escapeHtml(result.private_key || '')}</textarea>
              <div class="flex gap-2 mt-2">
                <button type="button" id="key-gen-download" class="rounded border border-slate-300 px-2 py-1 text-xs">${t('app.sshKeyGenerateDownload')}</button>
                <button type="button" id="key-gen-copy-private" class="rounded border border-slate-300 px-2 py-1 text-xs">${t('common.copy')}</button>
              </div>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.sshKeyGeneratePublicKeyLabel')}</label>
              <input type="text" id="key-gen-result-public" readonly value="${escapeHtml(result.public_key || '')}" class="w-full rounded border border-slate-300 px-2 py-2 text-xs font-mono bg-slate-50" />
              <div class="flex gap-2 mt-2">
                <button type="button" id="key-gen-copy-public" class="rounded border border-slate-300 px-2 py-1 text-xs">${t('common.copy')}</button>
              </div>
              <p class="text-xs text-slate-500 mt-1">${t('app.sshKeyGeneratePublicKeyHint')}</p>
            </div>
            <p id="key-gen-copy-status" class="text-xs text-emerald-600 hidden"></p>
          </div>
          <div class="px-6 py-4 bg-slate-50 flex justify-end border-t border-slate-200">
            <button type="button" id="key-gen-done" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('common.close')}</button>
          </div>
        </div>
      </div>`
    const statusEl = dlg.querySelector('#key-gen-copy-status')
    const showCopied = () => {
      statusEl.textContent = t('common.copied')
      statusEl.classList.remove('hidden')
    }
    dlg.querySelector('#key-gen-download').addEventListener('click', () => {
      downloadTextFile(result.id || 'id_ed25519', result.private_key || '')
    })
    dlg.querySelector('#key-gen-copy-private').addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(result.private_key || '')
        showCopied()
      } catch {
        dlg.querySelector('#key-gen-result-private').select()
      }
    })
    dlg.querySelector('#key-gen-copy-public').addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(result.public_key || '')
        showCopied()
      } catch {
        dlg.querySelector('#key-gen-result-public').select()
      }
    })
    const done = async () => {
      closeDialog(dlg)
      await refreshAll()
    }
    dlg.querySelector('#key-gen-done').addEventListener('click', done)
  }

  function showKeyEdit(id, label) {
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshKeyEdit')}</h3>
            <button type="button" id="key-edit-close" class="text-2xl text-slate-500">&times;</button>
          </div>
          <form id="key-edit-form" class="px-6 py-5 space-y-4">
            <div class="text-sm font-mono text-slate-600">${escapeHtml(id)}</div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsName')}</label>
              <input type="text" id="key-edit-label" value="${escapeHtml(label)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPrivateKey')}</label>
              <div id="key-edit-drop" class="rounded border border-slate-300 p-2">
                <p class="text-xs text-slate-600 mb-2">${t('app.credentialsPrivateKeyDrop')}</p>
                <button type="button" id="key-edit-pick" class="mb-2 rounded border border-slate-300 px-2 py-1 text-xs">${t('app.credentialsPrivateKeyPickFile')}</button>
                <textarea id="key-edit-pem" rows="5" class="w-full rounded border border-slate-200 px-2 py-2 text-sm font-mono" placeholder="${t('app.credentialsSavedHint')}"></textarea>
                <input type="file" id="key-edit-file" class="hidden" accept=".pem,.key,.txt,*/*" />
              </div>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPassphrase')}</label>
              <input type="password" id="key-edit-pp" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${t('app.credentialsSavedHint')}" />
            </div>
            <p id="key-edit-error" class="text-sm text-red-600 hidden"></p>
            <div class="flex justify-end gap-2">
              <button type="button" id="key-edit-cancel" class="rounded border px-3 py-2 text-xs">${t('common.cancel')}</button>
              <button type="submit" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('common.save')}</button>
            </div>
          </form>
        </div>
      </div>`
    const close = () => closeDialog(dlg)
    const pemEl = dlg.querySelector('#key-edit-pem')
    bindKeyDrop({
      dropEl: dlg.querySelector('#key-edit-drop'),
      pickBtn: dlg.querySelector('#key-edit-pick'),
      inputEl: dlg.querySelector('#key-edit-file'),
      textareaEl: pemEl,
    })
    dlg.querySelector('#key-edit-close').addEventListener('click', close)
    dlg.querySelector('#key-edit-cancel').addEventListener('click', close)
    dlg.querySelector('#key-edit-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = dlg.querySelector('#key-edit-error')
      errEl.classList.add('hidden')
      const payload = { label: dlg.querySelector('#key-edit-label').value.trim() }
      const pem = pemEl.value.trim()
      if (pem) payload.ssh_private_key = pem
      const pp = dlg.querySelector('#key-edit-pp').value
      if (pp !== '') payload.ssh_private_key_passphrase = pp
      try {
        await API.updateSSHKey(id, payload)
        close()
        await refreshAll()
      } catch (err) {
        errEl.textContent = err.message
        errEl.classList.remove('hidden')
      }
    })
  }

  function identityAuthFieldsHtml(prefix, keys, selectedKeyId) {
    const keyOpts = keys
      .map((k) => `<option value="${escapeHtml(k.id)}" ${k.id === selectedKeyId ? 'selected' : ''}>${escapeHtml(k.label || k.id)}</option>`)
      .join('')
    return `
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.authMethodLabel')}</label>
        <div class="space-y-2 text-sm">
          <label class="flex gap-2"><input type="radio" name="${prefix}-auth" value="password" /> ${t('app.authPassword')}</label>
          <label class="flex gap-2"><input type="radio" name="${prefix}-auth" value="key" /> ${t('app.authPublicKey')}</label>
          <label class="flex gap-2"><input type="radio" name="${prefix}-auth" value="password_and_key" /> ${t('app.credentialIdentityAuthPasswordAndKey')}</label>
        </div>
      </div>
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsUsername')}</label>
        <input type="text" id="${prefix}-user" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${escapeHtml(t('app.placeholderRoot'))}" />
      </div>
      <div id="${prefix}-pw-wrap">
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsPassword')}</label>
        <input type="password" id="${prefix}-pw" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" />
      </div>
      <div id="${prefix}-key-wrap" class="hidden">
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsLinkedKey')}</label>
        <select id="${prefix}-key-id" class="w-full rounded border border-slate-300 px-3 py-2 text-sm bg-white">
          <option value="">${t('app.credentialsPickKey')}</option>
          ${keyOpts}
        </select>
      </div>`
  }

  function wireIdentityAuth(prefix, initialAuth) {
    const sync = () => {
      const auth = document.querySelector(`input[name="${prefix}-auth"]:checked`)?.value || 'password'
      document.getElementById(`${prefix}-pw-wrap`)?.classList.toggle('hidden', auth === 'key')
      document.getElementById(`${prefix}-key-wrap`)?.classList.toggle('hidden', auth === 'password')
    }
    document.querySelectorAll(`input[name="${prefix}-auth"]`).forEach((r) => {
      if (r.value === initialAuth) r.checked = true
      r.addEventListener('change', sync)
    })
    sync()
  }

  async function showIdentityAdd() {
    const keys = await loadKeys()
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.identityAdd')}</h3>
            <button type="button" id="ident-add-close" class="text-2xl text-slate-500">&times;</button>
          </div>
          <form id="ident-add-form" class="px-6 py-5 space-y-4">
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsName')}</label>
              <input type="text" id="ident-add-label" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" required />
            </div>
            ${identityAuthFieldsHtml('ident-add', keys, '')}
            <p id="ident-add-error" class="text-sm text-red-600 hidden"></p>
            <div class="flex justify-end gap-2">
              <button type="button" id="ident-add-cancel" class="rounded border px-3 py-2 text-xs">${t('common.cancel')}</button>
              <button type="submit" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('common.add')}</button>
            </div>
          </form>
        </div>
      </div>`
    wireIdentityAuth('ident-add', 'password')
    const close = () => closeDialog(dlg)
    dlg.querySelector('#ident-add-close').addEventListener('click', close)
    dlg.querySelector('#ident-add-cancel').addEventListener('click', close)
    dlg.querySelector('#ident-add-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = dlg.querySelector('#ident-add-error')
      errEl.classList.add('hidden')
      const label = dlg.querySelector('#ident-add-label').value.trim()
      const id = slugFromLabel(label) || `ident-${Date.now()}`
      const auth = document.querySelector('input[name="ident-add-auth"]:checked')?.value
      const user = dlg.querySelector('#ident-add-user').value.trim()
      const pw = dlg.querySelector('#ident-add-pw').value
      const keyId = dlg.querySelector('#ident-add-key-id').value.trim()
      const payload = { id, label, ssh_username: user }
      if (auth === 'password' || auth === 'password_and_key') payload.ssh_password = pw
      if (auth === 'key' || auth === 'password_and_key') payload.ssh_key_id = keyId
      try {
        await API.createCredentialIdentity(payload)
        close()
        await refreshAll()
      } catch (err) {
        errEl.textContent = err.message
        errEl.classList.remove('hidden')
      }
    })
  }

  async function showIdentityEdit(meta) {
    const keys = await loadKeys()
    const dlg = credentialDialog()
    if (!dlg) return
    dlg.classList.remove('hidden')
    const initialAuth = meta.auth_method || (meta.has_password && meta.ssh_key_id ? 'password_and_key' : meta.ssh_key_id ? 'key' : 'password')
    dlg.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 border border-slate-200/50 overflow-hidden">
          <div class="px-5 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.identityEdit')}</h3>
            <button type="button" id="ident-edit-close" class="text-2xl text-slate-500">&times;</button>
          </div>
          <form id="ident-edit-form" class="px-6 py-5 space-y-4">
            <div class="text-sm font-mono text-slate-600">${escapeHtml(meta.id)}</div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.credentialsName')}</label>
              <input type="text" id="ident-edit-label" value="${escapeHtml(meta.label)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm" />
            </div>
            ${identityAuthFieldsHtml('ident-edit', keys, meta.ssh_key_id || '')}
            <p id="ident-edit-error" class="text-sm text-red-600 hidden"></p>
            <div class="flex justify-end gap-2">
              <button type="button" id="ident-edit-cancel" class="rounded border px-3 py-2 text-xs">${t('common.cancel')}</button>
              <button type="submit" class="rounded bg-sky-600 text-white px-3 py-2 text-xs">${t('common.save')}</button>
            </div>
          </form>
        </div>
      </div>`
    dlg.querySelector('#ident-edit-user').value = meta.ssh_username || ''
    wireIdentityAuth('ident-edit', initialAuth)
    const close = () => closeDialog(dlg)
    dlg.querySelector('#ident-edit-close').addEventListener('click', close)
    dlg.querySelector('#ident-edit-cancel').addEventListener('click', close)
    dlg.querySelector('#ident-edit-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = dlg.querySelector('#ident-edit-error')
      errEl.classList.add('hidden')
      const auth = document.querySelector('input[name="ident-edit-auth"]:checked')?.value
      const payload = {
        label: dlg.querySelector('#ident-edit-label').value.trim(),
        ssh_username: dlg.querySelector('#ident-edit-user').value.trim(),
      }
      const pw = dlg.querySelector('#ident-edit-pw').value
      if (auth === 'password' || auth === 'password_and_key') {
        if (pw !== '') payload.ssh_password = pw
      } else {
        payload.ssh_password = ''
      }
      const keyId = dlg.querySelector('#ident-edit-key-id').value.trim()
      if (auth === 'key' || auth === 'password_and_key') {
        payload.ssh_key_id = keyId
      } else {
        payload.ssh_key_id = ''
      }
      try {
        await API.updateCredentialIdentity(meta.id, payload)
        close()
        await refreshAll()
      } catch (err) {
        errEl.textContent = err.message
        errEl.classList.remove('hidden')
      }
    })
  }

  mainContent.querySelector('#cred-page-add-key').addEventListener('click', showKeyAdd)
  mainContent.querySelector('#cred-page-generate-key').addEventListener('click', showKeyGenerate)
  mainContent.querySelector('#cred-page-add-identity').addEventListener('click', showIdentityAdd)

  await refreshAll()
}
