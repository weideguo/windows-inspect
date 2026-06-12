package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// LogLevel 日志级别
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// parseLevel 从 LOG_LEVEL 环境变量解析级别，默认 Debug
func parseLevel(raw string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG", "DBG":
		return LevelDebug
	case "INFO", "INF":
		return LevelInfo
	case "WARN", "WARNING", "WRN":
		return LevelWarn
	case "ERROR", "ERR":
		return LevelError
	default:
		return LevelDebug
	}
}

// Logger 提供分级、带格式的日志
type Logger struct {
	level LogLevel
	name  string
}

// NewLogger 创建日志对象，从 LOG_LEVEL 环境读取级别，默认 debug
func NewLogger(name string) *Logger {
	lvl := parseLevel(os.Getenv("LOG_LEVEL"))
	return &Logger{level: lvl, name: name}
}

// SetLevel 动态设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// Level 返回当前级别
func (l *Logger) Level() LogLevel {
	return l.level
}

// logf 统一输出：时间 [级别] logger名 - 消息
func (l *Logger) logf(level LogLevel, format string, v ...interface{}) {
	if level < l.level {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, v...)
	log.Printf("%s [%s] %s - %s", ts, level.String(), l.name, msg)
}

func (l *Logger) Debug(format string, v ...interface{}) { l.logf(LevelDebug, format, v...) }
func (l *Logger) Info(format string, v ...interface{})  { l.logf(LevelInfo, format, v...) }
func (l *Logger) Warn(format string, v ...interface{})  { l.logf(LevelWarn, format, v...) }
func (l *Logger) Error(format string, v ...interface{}) { l.logf(LevelError, format, v...) }

// 包级日志对象（其他 handler 复用）
var logger = NewLogger("windows_execution")

type execRequest struct {
	Ps      string      `json:"ps"`
	Bat     string      `json:"bat"`
	Timeout interface{} `json:"timeout"`
}

type execResponse struct {
	Mode       string `json:"mode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ReturnCode int    `json:"returncode"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, *")
		w.Header().Set("Access-Control-Expose-Headers", "*")
		if origin != "*" {
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decodeOutput 按多候选编码尝试解码输出字节，首个成功即返回。
// 优先级：严格 UTF-8 -> UTF-16-LE -> GB18030 -> GBK -> CP936 -> Big5 -> CP850 -> ASCII 兜底
func decodeOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	// 1) 已为严格合法 UTF-8，直接返回
	if utf8.Valid(b) {
		return string(b)
	}

	// 2) 若首 2 字节为 UTF-16-LE BOM，按 UTF-16-LE 解析
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		if s, err := decodeUTF16LE(b[2:]); err == nil {
			return s
		}
	}

	// 3) UTF-16-LE（偶长度 + 高字节多为 0x00/0xFF 的启发）
	if len(b)%2 == 0 && looksLikeUTF16LE(b) {
		if s, err := decodeUTF16LE(b); err == nil {
			return s
		}
	}

	// 4) GB18030（含 GBK / CP936 覆盖）
	if s, err := decodeWith(simplifiedchinese.GB18030.NewDecoder(), b); err == nil {
		return s
	}
	if s, err := decodeWith(simplifiedchinese.GBK.NewDecoder(), b); err == nil {
		return s
	}
	if s, err := decodeWith(simplifiedchinese.HZGB2312.NewDecoder(), b); err == nil {
		return s
	}
	// 5) Big5
	if s, err := decodeWith(traditionalchinese.Big5.NewDecoder(), b); err == nil {
		return s
	}

	// 6) 常见西文 OEM/ANSI 代码页
	candidates := []*charmap.Charmap{
		charmap.CodePage850,
		charmap.CodePage437,
		charmap.Windows1252,
		charmap.ISO8859_1,
	}
	for _, cm := range candidates {
		if s, err := decodeWith(cm.NewDecoder(), b); err == nil {
			return s
		}
	}

	// 7) 最终兜底：按 UTF-8 原样返回（含替换字符）
	return string(b)
}

func looksLikeUTF16LE(b []byte) bool {
	// 简单启发：奇数位置（高字节）以 0x00 为主
	zeros := 0
	total := 0
	for i := 1; i < len(b); i += 2 {
		total++
		if b[i] == 0x00 || b[i] == 0xFF {
			zeros++
		}
	}
	if total == 0 {
		return false
	}
	return zeros*10 >= total*7 // >= 70%
}

func decodeUTF16LE(b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", fmt.Errorf("odd length")
	}
	runes := make([]rune, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		c := rune(b[i]) | (rune(b[i+1]) << 8)
		// 代理项对（basic BMP 覆盖大多数场景即可）
		if c >= 0xD800 && c <= 0xDBFF {
			if i+3 < len(b) {
				lo := rune(b[i+2]) | (rune(b[i+3]) << 8)
				if lo >= 0xDC00 && lo <= 0xDFFF {
					runes = append(runes, 0x10000+(c-0xD800)<<10+(lo-0xDC00))
					i += 2
					continue
				}
			}
		}
		runes = append(runes, c)
	}
	return string(runes), nil
}

func decodeWith(decoder transform.Transformer, b []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(b), decoder)
	out, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(out) {
		return "", fmt.Errorf("result not utf-8")
	}
	return string(out), nil
}

func runPowerShell(cmd string, timeout time.Duration) (stdout, stderr string, code int, err error) {
	// 统一使用 UTF-8 输入输出：
	// 1) chcp 65001 将控制台代码页切为 UTF-8
	// 2) [Console]::*Encoding = UTF8 让 PowerShell 自身以 UTF-8 读写控制台
	// 3) $OutputEncoding 影响 PowerShell 调用其他可执行文件时的管道编码
	// 在这之后，脚本正文（Write-Output / .NET 输出）一律以 UTF-8 字节写出，
	// Go 侧按 UTF-8 读取即可，无需再做多编码 fallback。
	const preamble = "chcp 65001 > $null; " +
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " +
		"[Console]::InputEncoding = [System.Text.Encoding]::UTF8; " +
		"$OutputEncoding = [System.Text.Encoding]::UTF8; "
	script := preamble + strings.TrimSpace(cmd)

	// 写入临时 .ps1 文件（UTF-16-LE with BOM），这样 PowerShell 可以正确解析任意 Unicode 脚本，
	// 同时避免 -EncodedCommand 的 stderr CLIXML 格式问题。
	f, err := os.CreateTemp("", "exec_*.ps1")
	if err != nil {
		return "", "", -1, err
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	// 1. 写入 UTF-16-LE BOM 头
	if _, err = f.Write([]byte{0xFF, 0xFE}); err != nil {
		f.Close()
		return "", "", -1, err
	}

	// 2. 使用官方 utf16 编码器进行安全的字节流转换
	encoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
	encodedBytes, err := encoder.Bytes([]byte(script)) // 注意：传入 []byte(script)，Go 内部会自动将 UTF-8 字符串正确转换为 UTF-16-LE
	if err != nil {
		f.Close()
		return "", "", -1, err
	}

	// 3. 一次性写入转换后的字节流
	if _, err = f.Write(encodedBytes); err != nil {
		f.Close()
		return "", "", -1, err
	}

	_ = f.Sync()
	if err = f.Close(); err != nil {
		return "", "", -1, err
	}

	c := exec.CommandContext(
		context.Background(),
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", tmpPath,
	)
	return runCmdWithTimeout(c, timeout)
}

func runBat(script string, timeout time.Duration) (stdout, stderr string, code int, err error) {
	// cmd.exe 读取批处理文件时使用系统 OEM 代码页（中文系统为 CP936/GBK），
	// 所以按 GBK 编码写入，确保中文在脚本内被正确解析。
	f, err := os.CreateTemp("", "exec_*.bat")
	if err != nil {
		return "", "", -1, err
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	content := "@echo off\r\n" + script
	if !strings.HasSuffix(strings.TrimRight(script, "\r\n"), "\n") {
		content += "\r\n"
	}

	// 用 GBK 编码写入批处理文件。
	encoded, encErr := simplifiedchinese.GBK.NewEncoder().String(content)
	if encErr != nil {
		// 回退：按原样字节写入（适用于纯 ASCII 场景）
		encoded = content
	}
	if _, err = f.WriteString(encoded); err != nil {
		f.Close()
		return "", "", -1, err
	}
	_ = f.Sync()
	if err = f.Close(); err != nil {
		return "", "", -1, err
	}

	c := exec.CommandContext(context.Background(), "cmd.exe", "/c", tmpPath)
	return runCmdWithTimeout(c, timeout)
}

func runCmdWithTimeout(c *exec.Cmd, timeout time.Duration) (stdout, stderr string, code int, err error) {
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	// 运行，带超时
	done := make(chan error, 1)
	go func() {
		done <- c.Run()
	}()

	select {
	case runErr := <-done:
		stdout = decodeOutput(outBuf.Bytes())
		stderr = decodeOutput(errBuf.Bytes())
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				return stdout, stderr, exitErr.ExitCode(), nil
			}
			return stdout, stderr, -1, runErr
		}
		return stdout, stderr, 0, nil
	case <-time.After(timeout):
		if c.Process != nil {
			_ = c.Process.Kill()
		}
		return decodeOutput(outBuf.Bytes()), decodeOutput(errBuf.Bytes()), -1, fmt.Errorf("command timeout")
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"go":     runtime.Version(),
		"os":     runtime.GOOS,
		"arch":   runtime.GOARCH,
	})
}

func execHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	var req execRequest
	body, _ := io.ReadAll(r.Body)
	logger.Debug("received request from=%s method=%s path=%s body_size=%d",
		r.RemoteAddr, r.Method, r.URL.Path, len(body))

	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			logger.Warn("invalid json body from=%s err=%v", r.RemoteAddr, err)
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid json body: " + err.Error()})
			return
		}
	}

	ps := strings.TrimSpace(req.Ps)
	bat := strings.TrimSpace(req.Bat)
	if ps != "" && bat != "" {
		logger.Warn("bad request from=%s: ps and bat both provided", r.RemoteAddr)
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "ps and bat cannot be used together"})
		return
	}
	if ps == "" && bat == "" {
		logger.Warn("bad request from=%s: neither ps nor bat provided", r.RemoteAddr)
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "either ps or bat is required"})
		return
	}

	timeoutSec := 30
	switch v := req.Timeout.(type) {
	case float64:
		if int(v) > 0 {
			timeoutSec = int(v)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	case nil:
	default:
		logger.Warn("bad request from=%s: invalid timeout=%T(%v)", r.RemoteAddr, v, v)
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "timeout must be an integer"})
		return
	}

	timeout := time.Duration(timeoutSec) * time.Second

	var (
		stdout, stderr string
		code           int
		err            error
		mode           string
	)

	if ps != "" {
		mode = "ps"
		logger.Debug("executing powershell from=%s timeout=%ss script=\n%s",
			r.RemoteAddr, timeoutSec, ps)
		stdout, stderr, code, err = runPowerShell(req.Ps, timeout)
	} else {
		mode = "bat"
		logger.Debug("executing batch from=%s timeout=%ss script=\n%s",
			r.RemoteAddr, timeoutSec, bat)
		stdout, stderr, code, err = runBat(req.Bat, timeout)
	}

	if err != nil && (stdout == "" && stderr == "") {
		logger.Error("exec failed mode=%s from=%s err=%v", mode, r.RemoteAddr, err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	logger.Info("exec finished mode=%s from=%s returncode=%d stdout=%d chars stderr=%d chars",
		mode, r.RemoteAddr, code, len(stdout), len(stderr))
	logger.Debug("exec finished mode=%s from=%s returncode=%d stdout=%s stderr=%s",
		mode, r.RemoteAddr, code, stdout, stderr)
	if code != 0 {
		logger.Warn("non-zero returncode mode=%s from=%s returncode=%d stderr=%s",
			mode, r.RemoteAddr, code, truncate(stderr, 400))
	}

	writeJSON(w, http.StatusOK, execResponse{
		Mode:       mode,
		Stdout:     stdout,
		Stderr:     stderr,
		ReturnCode: code,
	})
}

// truncate 截断长字符串，用于日志输出避免刷屏
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/exec", execHandler)
	mux.HandleFunc("/health", healthHandler)

	addr := ":5000"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		addr = v
	}

	handler := corsMiddleware(mux)

	// 为标准库 log 统一输出格式（不覆盖用户日志的前缀信息）：
	// 标准库的时间戳交给我们自己在 Logger.logf 里格式化，这里关闭其默认前缀，
	// 避免出现重复时间信息。
	log.SetFlags(0)

	logger.Info("windows_execution listening on %s (log_level=%s)", addr, logger.Level().String())
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error("server error: %v", err)
		os.Exit(1)
	}
}
