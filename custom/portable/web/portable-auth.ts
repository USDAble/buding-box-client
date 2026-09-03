import { writable } from 'svelte/store'

declare const __PORTABLE_FORCE_LOGIN__: boolean

const COOKIE_NAME = 'octo_access_key'
const LOGIN_STATE_KEY = 'buding_box_portable_logged_in'
const LEGACY_STORAGE_KEY = 'octo_access_key'
const PROBE_ENDPOINT = '/api/sessions?limit=1'

// 临时便携版口令：仅用于当前阶段，正式发布前必须替换为可配置的安全凭据。
export const PORTABLE_PASSWORD = '123456'

export const authPrompt = writable<{ retry: boolean } | null>(null)

let resolvePrompt: ((ok: boolean) => void) | null = null

function setCookie(): void {
  const secure = location.protocol === 'https:' ? '; Secure' : ''
  document.cookie = `${COOKIE_NAME}=${PORTABLE_PASSWORD}; path=/; SameSite=Strict${secure}`
}

function clearCookie(): void {
  document.cookie = `${COOKIE_NAME}=; path=/; max-age=0; SameSite=Strict`
}

function clearLogin(): void {
  clearCookie()
  localStorage.removeItem(LOGIN_STATE_KEY)
  localStorage.removeItem(LEGACY_STORAGE_KEY)
}

function askUserForKey(): Promise<boolean> {
  return new Promise((resolve) => {
    resolvePrompt = resolve
    authPrompt.set({ retry: false })
  })
}

// 错误口令只更新当前页面，不关闭登录层，也不发起鉴权 API 请求。
export function submitAuthKey(key: string | null): void {
  if (key !== PORTABLE_PASSWORD) {
    if (key === null) {
      authPrompt.set(null)
      const resolve = resolvePrompt
      resolvePrompt = null
      resolve?.(false)
      return
    }
    authPrompt.set({ retry: true })
    return
  }

  setCookie()
  localStorage.setItem(LOGIN_STATE_KEY, '1')
  localStorage.removeItem(LEGACY_STORAGE_KEY)
  authPrompt.set(null)
  const resolve = resolvePrompt
  resolvePrompt = null
  resolve?.(true)
}

let checkPromise: Promise<boolean> | null = null

export function checkAuth(): Promise<boolean> {
  if (!checkPromise) {
    if (!__PORTABLE_FORCE_LOGIN__ && localStorage.getItem(LOGIN_STATE_KEY) === '1') {
      setCookie()
      checkPromise = Promise.resolve(true)
    } else {
      clearLogin()
      checkPromise = askUserForKey()
    }
  }
  return checkPromise
}

// WebSocket 异常断开时才探测现有凭据；登录表单本身不依赖该接口校验口令。
export async function isUnauthorized(): Promise<boolean> {
  try {
    return (await fetch(PROBE_ENDPOINT)).status === 401
  } catch {
    return false
  }
}

export async function reauth(): Promise<boolean> {
  clearLogin()
  return askUserForKey()
}
