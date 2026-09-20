import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import LegalModal from './LegalModal.svelte'
import { locale } from '../../lib/i18n'
const { getAgreement } = vi.hoisted(() => ({ getAgreement: vi.fn() }))
vi.mock('../../lib/api', () => ({ getAgreement }))
let app: ReturnType<typeof mount> | undefined
let target: HTMLDivElement
beforeEach(() => { locale.set('zh'); getAgreement.mockReset(); target = document.createElement('div'); document.body.append(target) })
afterEach(async () => { if (app) await unmount(app); app = undefined; target.remove() })
const publication = { kind: 'box', version: 'v2', title: 'Server title', content: '<script>text</script>\nSecond line' }
it('loads on every open and renders server text without HTML', async () => {
  getAgreement.mockResolvedValue(publication)
  const show = () => mount(LegalModal, { target, props: { title: 'Terms', kind: 'box', open: true, onClose: vi.fn() } })
  app = show(); flushSync()
  await vi.waitFor(() => expect(target.textContent).toContain(publication.content))
  expect(target.querySelector('h2')?.textContent).toBe('Server title')
  expect(target.querySelector('script')).toBeNull()
  expect(getAgreement).toHaveBeenCalledWith('box', expect.any(AbortSignal))
  await unmount(app); app = show(); flushSync()
  await vi.waitFor(() => expect(getAgreement).toHaveBeenCalledTimes(2))
})
it('shows retry after failure and loads the privacy publication', async () => {
  getAgreement.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ ...publication, kind: 'privacy' })
  app = mount(LegalModal, { target, props: { title: 'Privacy', kind: 'privacy', open: true, onClose: vi.fn() } }); flushSync()
  await vi.waitFor(() => expect(target.querySelector('[role="alert"]')).not.toBeNull())
  const retry = Array.from(target.querySelectorAll('button')).find(b => b.textContent?.includes('重新加载'))!
  retry.click(); flushSync()
  await vi.waitFor(() => expect(target.textContent).toContain(publication.content))
  expect(getAgreement.mock.calls.every(c => c[0] === 'privacy')).toBe(true)
})
it('aborts pending reads when closed', async () => {
  getAgreement.mockImplementation(() => new Promise(() => {}))
  app = mount(LegalModal, { target, props: { title: 'Terms', kind: 'box', open: true, onClose: vi.fn() } }); flushSync()
  const signal = getAgreement.mock.calls[0][1] as AbortSignal
  await unmount(app); app = undefined
  expect(signal.aborted).toBe(true)
})

it('shows the publication without a consent action', async () => {
  getAgreement.mockResolvedValue(publication)
  app = mount(LegalModal, { target, props: { title: 'Terms', kind: 'box', open: true, onClose: vi.fn() } }); flushSync()
  await vi.waitFor(() => expect(target.textContent).toContain(publication.content))
  expect(target.querySelector('.legal-body button')).toBeNull()
  expect(target.textContent).not.toContain('我已阅读并同意')
})
