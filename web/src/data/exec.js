/**
 * execCommand —— 向 windows_execution 服务执行 PowerShell / Batch 命令
 *
 * 使用方式：
 *   import { execCommand } from '@/data/exec.js'
 *
 *   const r1 = await execCommand({ ps: '$env:COMPUTERNAME' })
 *   // => { mode: 'ps', returncode: 0, stderr: '', stdout: 'DESKTOP-747TFFE\n' }
 *
 *   const r2 = await execCommand({ bat: 'netstat -e' })
 *   // => { mode: 'bat', returncode: 0, stderr: '', stdout: '...' }
 *
 *   // 也可以指定超时时间（单位：秒）
 *   const r3 = await execCommand({ ps: '...', timeout: 60 })
 *
 * 实现细节：
 *   - baseUrl 从 localStorage['app:baseUrl'] 读取（可带/不带尾部斜杠）
 *   - 请求路径固定为 `${baseUrl}/exec`，方法 POST，Content-Type: application/json
 *   - body 必须包含 ps 或 bat 其中之一（不能同时存在、不能都为空）
 *   - 返回 { mode, returncode, stderr, stdout }
 *   - 当 baseUrl 为空时直接抛错（便于调用方捕获）
 */

const STORAGE_KEY = 'app:baseUrl'

/**
 * 从 localStorage 读取 baseUrl。
 * 为空时抛错，避免在后续请求中变成相对路径。
 */
function _readBaseUrl () {
  if (typeof localStorage === 'undefined') {
    throw new Error('execCommand: localStorage is not available in this environment')
  }
  const baseUrl = (localStorage.getItem(STORAGE_KEY) || '').trim()
  if (!baseUrl) {
    throw new Error(
      `execCommand: localStorage['${STORAGE_KEY}'] is empty; please set a baseUrl first`
    )
  }
  return baseUrl
}

/**
 * 构造请求体，并做参数校验。
 */
function _buildBody (opts) {
  if (!opts || typeof opts !== 'object') {
    throw new Error('execCommand: expected an object argument')
  }

  const hasPs = Object.prototype.hasOwnProperty.call(opts, 'ps') && opts.ps !== undefined
  const hasBat = Object.prototype.hasOwnProperty.call(opts, 'bat') && opts.bat !== undefined

  if (hasPs && hasBat) {
    throw new Error('execCommand: "ps" and "bat" cannot be used together')
  }
  if (!hasPs && !hasBat) {
    throw new Error('execCommand: either "ps" or "bat" is required')
  }

  const ps = hasPs ? String(opts.ps || '') : ''
  const bat = hasBat ? String(opts.bat || '') : ''

  if (!ps.trim() && !bat.trim()) {
    throw new Error('execCommand: the provided command must not be empty')
  }

  const body = {}
  if (hasPs) body.ps = ps
  if (hasBat) body.bat = bat

  if (opts.timeout !== undefined && opts.timeout !== null) {
    const t = Number(opts.timeout)
    if (!Number.isFinite(t) || t <= 0) {
      throw new Error('execCommand: "timeout" must be a positive number (seconds)')
    }
    body.timeout = Math.floor(t)
  }

  return body
}

/**
 * 执行命令。
 *
 * @param {{ ps?: string, bat?: string, timeout?: number }} opts
 * @returns {Promise<{ mode: 'ps'|'bat', returncode: number, stderr: string, stdout: string }>}
 */
export async function execCommand (opts) {
  const baseUrl = _readBaseUrl()
  const body = _buildBody(opts)

  // 规范化 baseUrl 末尾斜杠，避免出现 '//exec' 双斜杠。
  const url = baseUrl.replace(/\/+$/, '') + '/exec'

  let response
  try {
    response = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    })
  } catch (err) {
    // 网络层错误（跨域、DNS、TLS、未启动服务 等）
    const msg = err && err.message ? err.message : String(err)
    throw new Error(`execCommand: network error (${msg})`)
  }

  if (!response.ok) {
    let detail = `${response.status} ${response.statusText}`
    try {
      const j = await response.json()
      if (j && j.error) {
        detail += ` - ${j.error}`
      }
    } catch (_) {
      /* ignore JSON parse error on bad responses */
    }
    throw new Error(`execCommand: HTTP ${detail}`)
  }

  let data
  try {
    data = await response.json()
  } catch (err) {
    throw new Error('execCommand: invalid JSON response')
  }

  if (!data || !('mode' in data) || !('returncode' in data)) {
    throw new Error('execCommand: unexpected response payload')
  }

  return {
    mode: data.mode,
    returncode: data.returncode | 0,
    stderr: data.stderr || '',
    stdout: data.stdout || ''
  }
}


