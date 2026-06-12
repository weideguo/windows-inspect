import logging
import subprocess
import sys
import tempfile
import os
from flask import Flask, request, jsonify

try:
    # Python 3.3+ 内置，用于 UTF-16-LE / GBK 安全编码
    import codecs  # noqa: F401
except Exception:
    pass

app = Flask(__name__)

_LOG_FORMAT = '%(asctime)s [%(levelname)s] %(name)s - %(message)s'
_LOG_DATEFMT = '%Y-%m-%d %H:%M:%S'

_NAME_TO_LEVEL = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'WARN': logging.WARNING,
    'ERROR': logging.ERROR,
    'CRITICAL': logging.CRITICAL,
}


def _get_level_from_env(default_level=logging.DEBUG):
    raw = (os.environ.get('LOG_LEVEL') or '').strip().upper()
    if not raw:
        return default_level
    return _NAME_TO_LEVEL.get(raw, default_level)


def _build_formatter():
    return logging.Formatter(_LOG_FORMAT, _LOG_DATEFMT)


def _setup_logger():
    # 统一根 / werkzeug / flask 的日志格式，避免重复 handler
    root_logger = logging.getLogger()
    wsgi_logger = logging.getLogger('werkzeug')

    shared_handler = logging.StreamHandler(sys.stdout)
    shared_handler.setFormatter(_build_formatter())

    effective_level = _get_level_from_env(logging.DEBUG)

    for lg in (root_logger, wsgi_logger):
        lg.handlers.clear()
        lg.addHandler(shared_handler)
        lg.setLevel(effective_level)

    custom = logging.getLogger('windows_execution')
    if not custom.handlers:
        custom_handler = logging.StreamHandler(sys.stdout)
        custom_handler.setFormatter(_build_formatter())
        custom.addHandler(custom_handler)
    custom.setLevel(effective_level)
    custom.propagate = False
    return custom


_logger = _setup_logger()

try:
    from flask_cors import CORS  # type: ignore
    CORS(app, resources={r'/*': {'origins': '*'}})
    _HAS_FLASK_CORS = True
except Exception:
    _HAS_FLASK_CORS = False

try:
    import chardet  # type: ignore  # noqa: F401
    _HAS_CHARDET = True
except Exception:
    _HAS_CHARDET = False


@app.after_request
def _cors_after_request(response):
    origin = request.headers.get('Origin')
    response.headers['Access-Control-Allow-Origin'] = origin or '*'
    response.headers['Access-Control-Allow-Methods'] = 'GET, POST, PUT, DELETE, PATCH, OPTIONS'
    response.headers['Access-Control-Allow-Headers'] = 'Content-Type, Authorization, *'
    response.headers['Access-Control-Expose-Headers'] = '*'
    if origin:
        response.headers['Vary'] = 'Origin'
    return response


# 解码候选：与 main.go 的 decodeOutput 保持一致的优先级：
# 严格 UTF-8 -> UTF-16-LE（若有 BOM / 启发） -> GB18030/GBK/CP936/Hz-GB-2312
# -> Big5 -> 常见西文 OEM/ANSI 代码页 -> 最终 UTF-8 兜底
#
# 背景：像 w32tm、netstat 等部分 Windows 原生工具的输出代码页由内部 OEM 代码页
# （中文系统为 CP936/GBK）决定，即使我们在 bat/ps 里执行了 chcp 65001 或
# [Console]::OutputEncoding = UTF8，它们仍然按 OEM 代码页输出。所以必须做多
# 编码 fallback——这与 main.go 中 decodeOutput 的思想完全一致。
_DECODE_CANDIDATES = (
    # 1) 严格 UTF-8：大多数 PowerShell 脚本、以及显式 UTF-8 preamble 后的 PowerShell 输出
    'utf-8',
    # 2) UTF-16-LE（有时 .NET / 某些工具直接写 UTF-16；或被重定向成 UTF-16-LE）
    'utf-16-le',
    # 3) GB18030：GBK 的超集，兼容 CP936，是中文系统最常见的 OEM/ANSI 输出
    'gb18030',
    # 4) GBK / CP936
    'gbk',
    'cp936',
    # 5) GB2312
    'gb2312',
    # 6) Big5（繁体系统）
    'big5',
    # 7) 常见西文 OEM / ANSI 代码页
    'cp850',
    'cp437',
    'cp1252',
    'iso-8859-1',
)


def _looks_like_utf16_le(raw_bytes):
    """与 main.go 中的 looksLikeUTF16LE 相同：奇数位置以 0x00/0xFF 为主。"""
    if not raw_bytes or len(raw_bytes) % 2 != 0:
        return False
    zeros = 0
    total = 0
    for i in range(1, len(raw_bytes), 2):
        total += 1
        b = raw_bytes[i]
        if b == 0x00 or b == 0xFF:
            zeros += 1
    return total > 0 and zeros * 10 >= total * 7


def _decode_output(raw_bytes):
    """与 main.go 的 decodeOutput 对齐：
    - 空 -> 空串
    - UTF-8 BOM -> 跳过 -> UTF-8 严格解码
    - UTF-16-LE BOM（0xFF 0xFE） -> UTF-16-LE
    - 启发式判定 UTF-16-LE -> 尝试
    - 否则按候选表依次尝试；第一个成功的即返回
    - 最终 UTF-8 兜底（非法字节替换为 U+FFFD）"""
    if not raw_bytes:
        return ''

    # UTF-8 BOM（EF BB BF）
    if len(raw_bytes) >= 3 and raw_bytes[0:3] == b'\xef\xbb\xbf':
        try:
            return raw_bytes[3:].decode('utf-8')
        except UnicodeDecodeError:
            pass

    # UTF-16-LE BOM（FF FE）
    if len(raw_bytes) >= 2 and raw_bytes[0] == 0xFF and raw_bytes[1] == 0xFE:
        try:
            return raw_bytes[2:].decode('utf-16-le')
        except UnicodeDecodeError:
            pass

    # 启发式 UTF-16-LE
    if _looks_like_utf16_le(raw_bytes):
        try:
            return raw_bytes.decode('utf-16-le')
        except UnicodeDecodeError:
            pass

    # 多编码 fallback：要求严格解码（不允许替换）
    for enc in _DECODE_CANDIDATES:
        try:
            return raw_bytes.decode(enc)
        except (UnicodeDecodeError, LookupError):
            continue

    # 最终兜底：UTF-8 带替换
    return raw_bytes.decode('utf-8', errors='replace')


# PowerShell 开头的 UTF-8 开关：
# 1) chcp 65001 把控制台代码页切换为 UTF-8
# 2) [Console]*=UTF8 让 PowerShell 本身按 UTF-8 读写
# 3) $OutputEncoding 影响 PowerShell 调用其他命令时的管道编码
# 与 windows_execution\main.go 中的写法保持一致。
_PS_UTF8_PREAMBLE = (
    'chcp 65001 > $null; '
    '[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; '
    '[Console]::InputEncoding = [System.Text.Encoding]::UTF8; '
    '$OutputEncoding = [System.Text.Encoding]::UTF8; '
)


def _write_ps_script(user_ps):
    """与 main.go 的 runPowerShell 保持一致：
    - 在脚本前注入 UTF-8 preamble
    - 写入 .ps1 临时文件，文件以 UTF-16-LE + BOM 编码，
      这样 PowerShell 能正确解析任意 Unicode 脚本内容。
    返回 (临时文件路径, 清理函数)。"""
    script = _PS_UTF8_PREAMBLE + (user_ps or '').strip()

    tmp = tempfile.NamedTemporaryFile(suffix='.ps1', delete=False, mode='wb')
    try:
        # UTF-16-LE BOM (0xFF 0xFE) + 内容按 UTF-16-LE 编码
        tmp.write(b'\xff\xfe')
        tmp.write(script.encode('utf-16-le'))
        tmp.flush()
    finally:
        tmp.close()
    path = tmp.name

    def cleanup():
        try:
            os.unlink(path)
        except OSError:
            pass

    return path, cleanup


def _write_bat_script(user_bat):
    """与 main.go 的 runBat 保持一致：
    - cmd.exe 默认以 OEM 代码页（中文系统为 CP936/GBK）读取批处理文件，
      所以按 GBK 编码写入脚本。
    - 在脚本体内显式执行 chcp 65001 > nul，让后续管道输出以 UTF-8 编码，
      Python 侧按 UTF-8 读取 stdout/stderr 即可。
    返回 (临时文件路径, 清理函数)。"""
    content_lines = ['@echo off', 'chcp 65001 > nul']
    user_content = (user_bat or '').strip()
    if user_content:
        for ln in user_content.splitlines():
            content_lines.append(ln)
    body = '\r\n'.join(content_lines) + '\r\n'

    tmp = tempfile.NamedTemporaryFile(suffix='.bat', delete=False, mode='wb')
    try:
        # 按 GBK 写入，匹配 cmd.exe 默认读取行为；无法编码的字符用 '?' 替换
        tmp.write(body.encode('gbk', errors='replace'))
        tmp.flush()
    finally:
        tmp.close()
    path = tmp.name

    def cleanup():
        try:
            os.unlink(path)
        except OSError:
            pass

    return path, cleanup


@app.route('/exec', methods=['POST', 'OPTIONS'])
def exec_command():
    if request.method == 'OPTIONS':
        return jsonify({'ok': True}), 200

    data = request.get_json(silent=True) or {}
    ps = data.get('ps', '') or ''
    bat = data.get('bat', '') or ''
    timeout_raw = data.get('timeout', 30)

    _logger.info(
        'received exec request from %s: ps=%s chars, bat=%s chars, timeout=%r',
        request.remote_addr,
        len(ps) if ps else 0,
        len(bat) if bat else 0,
        timeout_raw,
    )

    if ps and bat:
        _logger.warning('invalid request: ps and bat provided together')
        return jsonify({'error': 'ps and bat cannot be used together'}), 400
    if not ps and not bat:
        _logger.warning('invalid request: neither ps nor bat provided')
        return jsonify({'error': 'either ps or bat is required'}), 400

    try:
        timeout = int(timeout_raw)
        if timeout <= 0:
            timeout = 30
    except (TypeError, ValueError):
        return jsonify({'error': 'timeout must be an integer'}), 400

    try:
        # 统一按 raw bytes 读取 stdout/stderr，再用 _decode_output（UTF-8）解码，
        # 与 main.go 中 runCmdWithTimeout 的处理方式一致。
        if ps:
            _logger.info('executing powershell command (timeout=%s):\n%s', timeout, ps)
            tmp_path, cleanup = _write_ps_script(ps)
            try:
                result = subprocess.run(
                    ['powershell', '-NoProfile', '-NonInteractive',
                     '-ExecutionPolicy', 'Bypass', '-File', tmp_path],
                    capture_output=True,
                    timeout=timeout,
                )
            finally:
                cleanup()
            stdout = _decode_output(result.stdout)
            stderr = _decode_output(result.stderr)
            returncode = result.returncode
            mode = 'ps'
        else:
            _logger.info('executing batch script (timeout=%s):\n%s', timeout, bat)
            tmp_path, cleanup = _write_bat_script(bat)
            try:
                result = subprocess.run(
                    ['cmd.exe', '/c', tmp_path],
                    capture_output=True,
                    timeout=timeout,
                )
            finally:
                cleanup()
            stdout = _decode_output(result.stdout)
            stderr = _decode_output(result.stderr)
            returncode = result.returncode
            mode = 'bat'

        _logger.info(
            'exec finished mode=%s returncode=%s stdout=%s chars stderr=%s chars',
            mode, returncode, len(stdout or ''), len(stderr or ''),
        )
        _logger.debug(
            'exec finished mode=%s returncode=%s stdout=%s stderr=%s',
            mode, returncode, stdout or '', stderr or '',
        )
        if returncode != 0:
            _logger.warning(
                'non-zero returncode mode=%s returncode=%s stderr=%s',
                mode, returncode, (stderr or '').strip()[:400],
            )

        return jsonify({
            'mode': mode,
            'stdout': stdout,
            'stderr': stderr,
            'returncode': returncode
        })
    except subprocess.TimeoutExpired:
        _logger.error('exec timeout after %ss', timeout, exc_info=True)
        return jsonify({'error': 'command timeout'}), 504
    except Exception as e:
        _logger.exception('exec failed: %s', e)
        return jsonify({'error': str(e)}), 500


@app.route('/health', methods=['GET'])
def health():
    return jsonify({'status': 'ok', 'python': sys.version})


_VALID_BIND_HOSTS = {'', '0.0.0.0', '127.0.0.1', 'localhost', '::', '::1'}


def _is_valid_host(host):
    if not host:
        return False
    if host in _VALID_BIND_HOSTS:
        return True
    if host.startswith('[') and host.endswith(']'):
        return False
    segments = host.split('.')
    if len(segments) == 4:
        try:
            return all(0 <= int(s) <= 255 for s in segments)
        except ValueError:
            pass
    return False


def _parse_listen_addr(default_host='0.0.0.0', default_port=5000):
    listen_addr = os.environ.get('LISTEN_ADDR', '').strip().strip('"').strip("'").strip()
    if not listen_addr:
        return default_host, default_port
    if listen_addr.startswith('['):
        idx = listen_addr.find(']:')
        if idx != -1:
            host = listen_addr[1:idx]
            port_str = listen_addr[idx + 2:]
        else:
            host = listen_addr.strip('[]')
            port_str = ''
    else:
        colon_count = listen_addr.count(':')
        if colon_count == 1:
            host, _, port_str = listen_addr.partition(':')
        elif colon_count == 0:
            host = listen_addr
            port_str = ''
        else:
            host, _, port_str = listen_addr.rpartition(':')
    host = host.strip()
    port_str = port_str.strip()
    if not _is_valid_host(host):
        host = default_host
    try:
        port = int(port_str) if port_str else default_port
        if port <= 0 or port > 65535:
            port = default_port
    except ValueError:
        port = default_port
    return host, port


if __name__ == '__main__':
    host, port = _parse_listen_addr()
    listen_env = os.environ.get('LISTEN_ADDR', '')
    _logger.info('LISTEN_ADDR=%r -> bind host=%r port=%r log_level=%s',
                 listen_env, host, port, logging.getLevelName(_logger.level))
    app.run(host=host, port=port, debug=False)
