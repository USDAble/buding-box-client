import { writable } from 'svelte/store'

declare const __PORTABLE_FORCE_LOGIN__: boolean

const COOKIE_NAME = 'octo_access_key'
const LOGIN_STATE_KEY = 'buding_box_portable_logged_in'
const LEGACY_STORAGE_KEY = 'octo_access_key'
const FORCE_LOGIN_SESSION_KEY = 'buding_box_portable_force_login_seen'
const PROBE_ENDPOINT = '/api/sessions?limit=1'
const LANGUAGE_ENDPOINT = '/api/config/language'

// 静态激活/验证码只用于当前原型，正式发布前必须接入受控身份服务。
export const PORTABLE_ACTIVATION_CODE = 'BUDING-123456'
export const PORTABLE_VERIFICATION_CODE = '123456'
export const PORTABLE_PASSWORD = PORTABLE_VERIFICATION_CODE

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

// 热更新测试只在当前浏览器会话首次打开时强制展示登录页。
// 登录成功后的刷新继续读取 U 盘 WebView 的持久化状态；关闭并重新打开测试会话后仍会再次展示登录页。
function shouldForceLogin(): boolean {
  if (!__PORTABLE_FORCE_LOGIN__) return false
  if (sessionStorage.getItem(FORCE_LOGIN_SESSION_KEY) === '1') return false
  sessionStorage.setItem(FORCE_LOGIN_SESSION_KEY, '1')
  return true
}

function askUserForKey(): Promise<boolean> {
  return new Promise((resolve) => {
    resolvePrompt = resolve
    authPrompt.set({ retry: false })
  })
}

// 错误口令只更新当前页面，不关闭登录层，也不发起鉴权 API 请求。
export async function submitAuthKey(key: string | null, language?: 'en' | 'zh'): Promise<void> {
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
  // 密码仍在本地校验；设置 Cookie 后才把登录页语言同步到 U 盘内的 Octo 配置。
  if (language) {
    try {
      const response = await fetch(LANGUAGE_ENDPOINT, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ language }),
      })
      if (!response.ok) throw new Error(`language update failed: ${response.status}`)
    } catch {
      // 语言已保存在 WebView localStorage；配置同步失败不阻断本地登录。
    }
  }
  authPrompt.set(null)
  const resolve = resolvePrompt
  resolvePrompt = null
  resolve?.(true)
}

let checkPromise: Promise<boolean> | null = null

export function checkAuth(): Promise<boolean> {
  if (!checkPromise) {
    if (!shouldForceLogin() && localStorage.getItem(LOGIN_STATE_KEY) === '1') {
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
