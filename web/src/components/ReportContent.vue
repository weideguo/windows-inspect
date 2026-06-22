<script setup>
import { reactive, ref, computed } from 'vue'
import { execCommand } from '../data/exec.js'
import { sections } from '../data/reportData.js'

const liveHostInfo = reactive([
  { k: '计算机名', v: '-' },
  { k: '操作系统', v: '-' },
  { k: '系统架构', v: '-' },
  { k: '域/工作组', v: '-' },
  { k: '当前用户', v: '-' },
  { k: '主板/机型', v: '-' },
  { k: 'BIOS', v: '-' },
  { k: '安全启动', v: '-' },
  { k: '许可证', v: '-' },
  { k: '运行时间', v: '-' },
  { k: '电源计划', v: '-' },
  { k: '时间同步', v: '-' }
])

// 主机基本信息：拆分 12 个子项，每项一条 PowerShell 命令
// 每条命令输出单行 key=value，便于前端稳定解析
// 优先用ps运行，bat可能出现编码问题
const hostInfoSteps = [
  {
    key: 'COMPUTERNAME',
    label: '计算机名',
    ps: String.raw`$c = Get-WmiObject Win32_ComputerSystem; Write-Output "COMPUTERNAME=$($c.Name)"`
  },
  {
    key: 'OSCAPTION',
    label: '操作系统',
    ps: String.raw`$os = Get-WmiObject Win32_OperatingSystem; Write-Output "OSCAPTION=$($os.Caption) ($($os.Version))"`
  },
  {
    key: 'OSARCH',
    label: '系统架构',
    ps: String.raw`$os = Get-WmiObject Win32_OperatingSystem; Write-Output "OSARCH=$($os.OSArchitecture)"`
  },
  {
    key: 'DOMAIN',
    label: '域/工作组',
    ps: String.raw`$c = Get-WmiObject Win32_ComputerSystem; Write-Output "DOMAIN=$($c.Domain)"`
  },
  {
    key: 'CURRENTUSER',
    label: '当前用户',
    ps: String.raw`$u = $env:USERDOMAIN + "\" + $env:USERNAME; Write-Output "CURRENTUSER=$u"`
  },
  {
    key: 'MODEL',
    label: '主板/机型',
    ps: String.raw`$cs = Get-WmiObject Win32_ComputerSystemProduct; Write-Output "MODEL=$($cs.Vendor) $($cs.Name)"`
  },
  {
    key: 'BIOS',
    label: 'BIOS',
    ps: String.raw`$b = Get-WmiObject Win32_BIOS; $d = $b.ReleaseDate; Write-Output "BIOS=$($b.Manufacturer) | $($b.SMBIOSBIOSVersion) | $($d.Substring(0,4))-$($d.Substring(4,2))-$($d.Substring(6,2))"`
  },
  {
    key: 'SECUREBOOT',
    label: '安全启动',
    ps: String.raw`$s = Confirm-SecureBootUEFI -ErrorAction SilentlyContinue; Write-Output "SECUREBOOT=$($s -eq $true)"`
  },
  {
    key: 'LICENSE',
    label: '许可证',
    ps: String.raw`$lic = (Get-CimInstance SoftwareLicensingProduct -Filter "PartialProductKey is not null" | Where-Object { $_.Name -like "*Windows*" -and $_.LicenseStatus -eq 1 } | Select-Object -First 1); Write-Output "LICENSE=$($lic.Description)"`
  },
  {
    key: 'UPTIME',
    label: '运行时间',
    ps: String.raw`$tb = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime; $span = New-TimeSpan -Start $tb -End (Get-Date); $up = "$($span.Days)天 $($span.Hours)时 $($span.Minutes)分"; Write-Output "UPTIME=$up (启动于 $($tb.ToString("yyyy-MM-dd HH:mm:ss")))"`
  },
  {
    key: 'POWERPLAN',
    label: '电源计划',
    ps: String.raw`powercfg /getactivescheme`
  },
  {
    key: 'TIMESYNC',
    label: '时间同步',
    ps: String.raw`w32tm /query /status`
  }
]

const hostProgress = reactive({
  total: hostInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const _pickValue = (stdout, key) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  const prefix = key + '='
  for (const l of lines) {
    if (l.indexOf(prefix) === 0) return l.slice(prefix.length).trim()
  }
  return ''
}

const _applyRow = (label, rawKey, raw) => {
  const idx = liveHostInfo.findIndex((r) => r.k === label)
  if (idx < 0) return
  if (rawKey === 'SECUREBOOT') {
    liveHostInfo[idx].v = (raw === 'True' || raw === 'true') ? '已启用' : '未启用'
    return
  }
  if (rawKey === 'LICENSE') {
    liveHostInfo[idx].v = raw || '未授权'
    return
  }
  if (rawKey === 'TIMESYNC') {
    const sourceLine = (raw || '').split(/\r?\n/).find((x) => /Source/i.test(x))
    liveHostInfo[idx].v = sourceLine
      ? ('正常，源: ' + sourceLine.replace(/^.*Source:\s*/i, '').trim())
      : (raw || '未知')
    return
  }
  if (rawKey === 'POWERPLAN') {
    // powercfg /getactivescheme 输出形如:
    //   Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (平衡)
    // 取最后一对括号中的中文名
    const text = (raw || '').trim()
    const m = text.match(/\(([^()]+)\)\s*$/) || text.match(/\(([^()]+)\)/)
    liveHostInfo[idx].v = m ? m[1].trim() : (text || '未知')
    return
  }
  liveHostInfo[idx].v = raw
}

const dismissHostProgress = () => {
  hostProgress.done = 0
  hostProgress.current = ''
  hostProgress.running = false
}

const runHostInfo = async () => {
  if (hostProgress.running) return
  hostProgress.running = true
  hostProgress.done = 0
  hostProgress.current = ''
  // 执行前先清空
  for (const row of liveHostInfo) row.v = ''

  try {
    for (const step of hostInfoSteps) {
      hostProgress.current = step.label
      try {
        // 优先 bat，否则回退到 ps
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        // 部分命令（如 powercfg /getactivescheme）不是 KEY=VALUE 格式，
        // 直接把整个 stdout 作为 raw 传给 _applyRow，由其内部解析
        const kv = _pickValue(result && result.stdout, step.key)
        const raw = kv || (result && result.stdout) || ''
        _applyRow(step.label, step.key, raw)
        console.log('[runHostInfo]', step.label, '→', raw, '(', (step.bat ? 'bat' : 'ps'), ')')
      } catch (err) {
        console.warn('[runHostInfo] fail:', step.label, err)
        _applyRow(step.label, step.key, '获取失败')
      } finally {
        hostProgress.done += 1
      }
    }
  } finally {
    hostProgress.current = '完成'
    hostProgress.running = false
    console.log('[runHostInfo] liveHostInfo:', JSON.parse(JSON.stringify(liveHostInfo)))
  }
}

// ================= 硬件资源状态 =================
const liveHardwareSummary = reactive([
  { label: 'CPU 使用率', value: 0, color: '#34a853' },
  { label: '内存 ', value: 0, color: '#34a853' },
  { label: '磁盘最高使用率', value: 0, color: '#34a853' }
])
const liveCpuInfo = reactive([
  { k: 'CPU', v: '-' },
  { k: '核心/线程', v: '-' },
  { k: '当前负载', v: '-' }
])
const liveMemoryInfo = reactive([])
const liveDiskInfo = reactive([])
const livePhysicalDisk = reactive([])
const liveGpuInfo = reactive([
  { k: '显卡', v: '-' },
  { k: '驱动版本', v: '-' },
  { k: '未签名驱动', v: '-' }
])

// 工具：取整到指定小数位
const _round = (n, d = 1) => {
  const p = Math.pow(10, d)
  return Math.round((Number(n) || 0) * p) / p
}

// 工具：从多行输出中解析第一段 "K=V" 格式值
const _pickKv = (stdout, key) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  const prefix = key + '='
  for (const l of lines) {
    if (l.indexOf(prefix) === 0) return l.slice(prefix.length).trim()
  }
  return ''
}

// 写回 liveCpuInfo 中的 k=v 条目
const _applyCpuKv = (key, val) => {
  const idx = liveCpuInfo.findIndex((r) => r.k === key)
  if (idx >= 0) liveCpuInfo[idx].v = val
}

// 硬件命令：每条命令 + 一个 write(stdout) 函数
// 所有 PowerShell 命令统一以 UTF-8 输出，以便解析中文字段
const hwInfoSteps = [
  {
    label: 'CPU 使用率',
    ps: String.raw`$cpu = (Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average | Select-Object -ExpandProperty Average); $cpuT = (Get-CimInstance Win32_Processor | Select-Object -First 1); Write-Output ("CPUUSAGE=" + [math]::Round($cpu,1)); Write-Output ("CPUNAME=" + $cpuT.Name); Write-Output ("CPUCORES=" + $cpuT.NumberOfCores + "/" + $cpuT.NumberOfLogicalProcessors + " @ " + $cpuT.MaxClockSpeed + "MHz")`
  },
  {
    label: '内存概要',
    ps: String.raw`$cs = Get-CimInstance Win32_ComputerSystem; $os = Get-CimInstance Win32_OperatingSystem; $totalGB = [math]::Round($cs.TotalPhysicalMemory/1GB,2); $freeGB = [math]::Round($os.FreePhysicalMemory/1MB,2); $usedGB = [math]::Round($totalGB - $freeGB,2); $usage = [math]::Round(($usedGB/$totalGB)*100,1); Write-Output ("MEMTOTAL=" + $totalGB + "GB"); Write-Output ("MEMFREE=" + $freeGB + "GB"); Write-Output ("MEMUSED=" + $usedGB + "GB"); Write-Output ("MEMUSAGE=" + $usage)`
  },
  {
    label: '内存条清单',
    ps: String.raw`Get-CimInstance Win32_PhysicalMemory | ForEach-Object { $cap = [math]::Round($_.Capacity/1GB,0); Write-Output ("MEMROW|MANU=" + $_.Manufacturer + "|CAP=" + $cap + "GB|FREQ=" + $_.ConfiguredClockSpeed + "MHz|PN=" + $_.PartNumber) }`
  },
  {
    label: '逻辑磁盘',
    ps: String.raw`Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3'|ForEach-Object{$t=[math]::Round($_.Size/1GB,1);$f=[math]::Round($_.FreeSpace/1GB,1);$u=[math]::Round($t-$f,1);$p=if($t-gt 0){[math]::Round($u/$t*100,1)}else{0};Write-Output "$($_.DeviceID)|$($_.VolumeName)|$t|$f|$u|$p"}`
  },
  {
    label: '物理磁盘健康',
    ps: String.raw`Get-CimInstance MSFT_PhysicalDisk -Namespace root/Microsoft/Windows/Storage -ErrorAction SilentlyContinue | ForEach-Object { $cap = [math]::Round($_.Size/1GB,0); $health = switch ($_.HealthStatus){0{"健康"} 1{"警告"} 2{"错误"} 3{"未知"} 4{"降级"} 5{"恢复中"} default{"未知"}}; $type = switch ($_.MediaType){3{"HDD"} 4{"SSD"} default{"未知"}}; Write-Output ("PDISKROW|NAME=" + $_.FriendlyName + "|TYPE=" + $type + "|CAP=" + $cap + "GB|BUS=" + $_.BusType + "|HEALTH=" + $health) }`
  },
  {
    label: 'GPU 信息',
    ps: String.raw`$gpus = Get-CimInstance Win32_VideoController -ErrorAction SilentlyContinue; $names = ($gpus | Select-Object -ExpandProperty Name) -join " / "; $drivers = ($gpus | Select-Object -ExpandProperty DriverVersion) -join " / "; Write-Output ("GPUNAME=" + $names); Write-Output ("GPUDRIVER=" + $drivers); $unsigned = (Get-CimInstance Win32_SystemDriver -Filter "Started=True" | Where-Object { $_.Name -match "gpu|video|display|igfx|nvlddmkm|amdkmdag" } | Where-Object { try { (Get-AuthenticodeSignature -FilePath $_.PathName -ErrorAction SilentlyContinue).Status -ne "Valid" } catch { $false } }).Count; if ($unsigned -eq $null) { $unsigned = 0 }; Write-Output ("GPUUNSIGNED=" + $unsigned + " 个")`
  }
]

const hwProgress = reactive({
  total: hwInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const _applyHardwareRow = (label, stdout) => {
  const text = (stdout || '').trim()
  if (!text) return

  // 解析每行 K=V，行内以 | 分隔多字段
  const lines = text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean)

  // 1) CPU 汇总
  if (label === 'CPU 使用率') {
    const usage = Number(_pickKv(text, 'CPUUSAGE')) || 0
    liveHardwareSummary[0].value = _round(usage, 1)
    liveHardwareSummary[0].color = usage >= 80 ? '#d93636' : (usage >= 60 ? '#f9ab00' : '#34a853')
    _applyCpuKv('当前负载', _round(usage, 1) + '%')
    const name = _pickKv(text, 'CPUNAME')
    if (name) _applyCpuKv('CPU', name)
    const cores = _pickKv(text, 'CPUCORES')
    if (cores) _applyCpuKv('核心/线程', cores)
    return
  }

  // 2) 内存概要
  if (label === '内存概要') {
    const total = _pickKv(text, 'MEMTOTAL')
    const used = _pickKv(text, 'MEMUSED')
    const free = _pickKv(text, 'MEMFREE')
    const usage = Number(_pickKv(text, 'MEMUSAGE')) || 0
    liveHardwareSummary[1].label = '内存 ' + (used || '0GB') + ' / ' + (total || '0GB')
    liveHardwareSummary[1].value = _round(usage, 1)
    liveHardwareSummary[1].color = usage >= 85 ? '#d93636' : (usage >= 60 ? '#f9ab00' : '#34a853')
    return
  }

  // 3) 内存条清单（形如 MEMROW|MANU=Hynix|CAP=32GB|FREQ=5600MHz|PN=HMCG88AGBUA084N）
  if (label === '内存条清单') {
    const rows = []
    for (const l of lines) {
      if (!l.startsWith('MEMROW|')) continue
      const fields = Object.fromEntries(
        l.slice('MEMROW|'.length).split('|').map((seg) => {
          const [k, ...rest] = seg.split('=')
          return [k, rest.join('=')]
        })
      )
      rows.push({ maker: fields.MANU || '', cap: fields.CAP || '', freq: fields.FREQ || '', model: fields.PN || '' })
    }
    liveMemoryInfo.splice(0, liveMemoryInfo.length, ...rows)
    return
  }

  // 4) 逻辑磁盘
  // 每行格式：盘符|卷标|总容量|可用|已用|使用率（单位：GB，使用率 0-100）
  // 示例：C:|系统|476.6|319.7|156.9|32.9
  if (label === '逻辑磁盘') {
    const rows = []
    let maxUsage = 0
    for (const l of lines) {
      // 同时兼容旧版 DISKROW|.. 与新版 C:|.. 两种格式
      let cols = []
      if (l.startsWith('DISKROW|')) {
        const fields = Object.fromEntries(
          l.slice('DISKROW|'.length).split('|').map((seg) => {
            const [k, ...rest] = seg.split('=')
            return [k, rest.join('=')]
          })
        )
        cols = [fields.DRIVE || '', fields.LABEL || '', fields.TOTAL || '', fields.FREE || '', fields.USED || '', fields.USAGE || '0']
      } else {
          cols = l.split('|')
          while (cols.length < 6) cols.push('')
        }
      const drive = String(cols[0] || '').trim()
      const labelVal = String(cols[1] || '').trim()
      const totalVal = String(cols[2] || '').trim()
      const freeVal = String(cols[3] || '').trim()
      const usedVal = String(cols[4] || '').trim()
      const usageVal = String(cols[5] || '0').trim()
      const usage = Number(usageVal) || 0
      if (usage > maxUsage) maxUsage = usage
      rows.push({
        drive: drive,
        label: labelVal,
        total: totalVal ? totalVal + ' GB' : '',
        free: freeVal ? freeVal + ' GB' : '',
        used: usedVal ? usedVal + ' GB' : '',
        usage: _round(usage, 1)
      })
    }
    liveDiskInfo.splice(0, liveDiskInfo.length, ...rows)
    // 同步更新 summary 的“磁盘最高使用率”
    liveHardwareSummary[2].value = _round(maxUsage, 1)
    liveHardwareSummary[2].color = maxUsage >= 85 ? '#d93636' : (maxUsage >= 60 ? '#f9ab00' : '#34a853')
    return
  }

  // 5) 物理磁盘
  if (label === '物理磁盘健康') {
    const rows = []
    for (const l of lines) {
      if (!l.startsWith('PDISKROW|')) continue
      const fields = Object.fromEntries(
        l.slice('PDISKROW|'.length).split('|').map((seg) => {
          const [k, ...rest] = seg.split('=')
          return [k, rest.join('=')]
        })
      )
      const health = fields.HEALTH || '未知'
      const badge = health === '健康' ? 'g' : (health === '警告' ? 'w' : 'b')
      rows.push({
        name: fields.NAME || '',
        type: fields.TYPE || '',
        cap: fields.CAP || '',
        bus: fields.BUS || '',
        health: health,
        badge: badge
      })
    }
    livePhysicalDisk.splice(0, livePhysicalDisk.length, ...rows)
    return
  }

  // 6) GPU
  if (label === 'GPU 信息') {
    const name = _pickKv(text, 'GPUNAME')
    const driver = _pickKv(text, 'GPUDRIVER')
    const unsigned = _pickKv(text, 'GPUUNSIGNED')
    const idxGpuName = liveGpuInfo.findIndex((r) => r.k === '显卡')
    if (idxGpuName >= 0) liveGpuInfo[idxGpuName].v = name || '-'
    const idxGpuDriver = liveGpuInfo.findIndex((r) => r.k === '驱动版本')
    if (idxGpuDriver >= 0) liveGpuInfo[idxGpuDriver].v = driver || '-'
    const idxGpuUnsigned = liveGpuInfo.findIndex((r) => r.k === '未签名驱动')
    if (idxGpuUnsigned >= 0) liveGpuInfo[idxGpuUnsigned].v = unsigned || '-'
    return
  }
}

const runHardwareInfo = async () => {
  if (hwProgress.running) return
  hwProgress.running = true
  hwProgress.done = 0
  hwProgress.current = ''

  // 先把所有 live hardware 数据清空
  for (const it of liveHardwareSummary) { it.value = 0 }
  for (const row of liveCpuInfo) row.v = ''
  liveMemoryInfo.splice(0, liveMemoryInfo.length)
  liveDiskInfo.splice(0, liveDiskInfo.length)
  livePhysicalDisk.splice(0, livePhysicalDisk.length)
  for (const row of liveGpuInfo) row.v = ''

  try {
    for (const step of hwInfoSteps) {
      hwProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        const raw = (result && result.stdout) || ''
        _applyHardwareRow(step.label, raw)
        console.log('[runHardwareInfo]', step.label, '→', raw)
      } catch (err) {
        console.warn('[runHardwareInfo] fail:', step.label, err)
      } finally {
        hwProgress.done += 1
      }
    }
  } finally {
    hwProgress.current = '完成'
    hwProgress.running = false
  }
}

const dismissHardwareProgress = () => {
  hwProgress.done = 0
  hwProgress.current = ''
  hwProgress.running = false
}

// ================= 网络配置与连接 =================
const liveNetworkAdapters = reactive([])
const liveIpAddresses = reactive([])
const liveNetworkKv = reactive([
  { k: '默认网关', v: '-' },
  { k: 'DNS 服务器', v: '-' },
  { k: '网络流量', v: '-' },
  { k: '连接统计', v: '-' }
])
const liveListeningPorts = reactive([])
const liveListeningPortsTotal = reactive({ total: 0 })
const liveSharedFolders = reactive([])
const portExpanded = ref(false)
const portSortKey = ref('port')
const portSortOrder = ref('asc')

// 辅助：把 00:11:22:33:44:55 统一成 00-11-22-33-44-55
const _normalizeMac = (m) => (m || '').replace(/[:.]/g, '-').toUpperCase()

// 新命令输出格式（已去除前缀，JS 负责解析文本）：
//   网络适配器:  每行  name|status|linkSpeed|macAddress
//   IP:          每行  adapter|ip|prefixLength|prefixOrigin
//   网关:        单行文本（IP 地址）
//   DNS:         单行文本（多个 IP 以 ", " 拼接）
//   连接统计:    单行形如 "已建立 56 | 监听 53 | TIME_WAIT 1"
//   网络流量:    单行形如 "接收 2.13 GB / 发送 3.11 GB"
//   监听端口:    每行  port|process|localAddress
//   共享文件夹:  每行  name|path|description|currentUsers
const netInfoSteps = [
  {
    label: '网络适配器',
    ps: String.raw`Get-NetAdapter -Physical -EA SilentlyContinue|ForEach-Object{Write-Output "$($_.Name)|$($_.Status)|$($_.LinkSpeed)|$($_.MacAddress)"}`
  },
  {
    label: 'IP 地址分配',
    ps: String.raw`Get-NetIPAddress -AddressFamily IPv4 -EA SilentlyContinue|Where-Object{$_.IPAddress -ne '127.0.0.1' -and $_.PrefixOrigin -ne 'WellKnown'}|Sort-Object InterfaceAlias|ForEach-Object{Write-Output "$($_.InterfaceAlias)|$($_.IPAddress)|$($_.PrefixLength)|$($_.PrefixOrigin)"}`
  },
  {
    label: '网关',
    ps: String.raw`(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -EA SilentlyContinue|Select-Object -First 1).NextHop`
  },
  {
    label: 'DNS',
    ps: String.raw`(Get-DnsClientServerAddress -AddressFamily IPv4 -EA SilentlyContinue|Where-Object{$_.ServerAddresses}|Select-Object -First 1).ServerAddresses -join ', '`
  },
  {
    label: '连接统计',
    ps: String.raw`$c=netstat -an; $e=($c|Select-String -Pattern 'ESTABLISHED' -SimpleMatch).Count; $l=($c|Select-String -Pattern 'LISTENING' -SimpleMatch).Count; $t=($c|Select-String -Pattern 'TIME_WAIT' -SimpleMatch).Count; Write-Output ('已建立 '+$e+' | 监听 '+$l+' | TIME_WAIT '+$t)`
  },
  {
    label: '网络流量',
    ps: String.raw`$stats = Get-NetAdapterStatistics | Measure-Object -Property ReceivedBytes, SentBytes -Sum;$gb = [math]::Round($stats[0].Sum / 1GB, 2);$gbs = [math]::Round($stats[1].Sum / 1GB, 2);Write-Output "接收 $gb GB / 发送 $gbs GB"`
  },
  {
    label: '监听端口',
    ps: String.raw`Get-NetTCPConnection -State Listen -EA SilentlyContinue|Select-Object LocalAddress,LocalPort,@{N='Process';E={(Get-Process -Id $_.OwningProcess -EA SilentlyContinue).ProcessName}}|Sort-Object LocalPort|ForEach-Object{Write-Output "$($_.LocalPort)|$($_.Process)|$($_.LocalAddress)"}`
  },
  {
    label: '共享文件夹',
    ps: String.raw`Get-SmbShare -EA SilentlyContinue|ForEach-Object{Write-Output "$($_.Name)|$($_.Path)|$($_.Description)|$($_.CurrentUsers)"}`
  }
]

const netProgress = reactive({
  total: netInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const sortedPorts = computed(() => {
  const list = portExpanded.value ? liveListeningPorts : liveListeningPorts.slice(0, 10)
  return [...list].sort((a, b) => {
    let diff = 0
    if (portSortKey.value === 'port') {
      diff = parseInt(a.port) - parseInt(b.port)
    } else if (portSortKey.value === 'process') {
      diff = a.process.localeCompare(b.process)
    } else if (portSortKey.value === 'scope') {
      diff = a.scope.localeCompare(b.scope)
    }
    return portSortOrder.value === 'asc' ? diff : -diff
  })
})

const togglePortSort = (key) => {
  if (portSortKey.value === key) {
    portSortOrder.value = portSortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    portSortKey.value = key
    portSortOrder.value = 'asc'
  }
}

const _applyNetworkRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  // 1) 网络适配器: 每行 name|status|linkSpeed|macAddress
  if (label === '网络适配器') {
    const rows = []
    for (const l of lines) {
      const cols = l.split('|')
      while (cols.length < 4) cols.push('')
      const status = (cols[1] || '').trim() || '-'
      rows.push({
        name: cols[0] || '',
        status: status,
        speed: cols[2] || '-',
        mac: _normalizeMac(cols[3]),
        badge: /^up$/i.test(status) ? 'g' : 'b'
      })
    }
    liveNetworkAdapters.splice(0, liveNetworkAdapters.length, ...rows)
    return
  }

  // 2) IP 地址分配: 每行 adapter|ip|prefixLength|prefixOrigin
  if (label === 'IP 地址分配') {
    const rows = []
    for (const l of lines) {
      const cols = l.split('|')
      while (cols.length < 4) cols.push('')
      rows.push({
        adapter: cols[0] || '',
        ip: cols[1] || '',
        subnet: '/' + (cols[2] || ''),
        source: cols[3] || ''
      })
    }
    liveIpAddresses.splice(0, liveIpAddresses.length, ...rows)
    return
  }

  // 3) 网关: 单行纯文本（一行 IP 地址）
  if (label === '网关') {
    const gw = lines.join(', ').trim()
    for (const row of liveNetworkKv) if (row.k === '默认网关') row.v = gw || '-'
    return
  }

  // 4) DNS: 单行纯文本（多个 DNS IP，"," 连接）
  if (label === 'DNS') {
    const dns = lines.join(', ').trim()
    for (const row of liveNetworkKv) if (row.k === 'DNS 服务器') row.v = dns || '-'
    return
  }

  // 5) 连接统计: 单行  "已建立 56 | 监听 53 | TIME_WAIT 1"
  if (label === '连接统计') {
    const text = lines.join(' | ').trim()
    for (const row of liveNetworkKv) if (row.k === '连接统计') row.v = text || '-'
    return
  }

  // 6) 网络流量: 单行  "接收 2.13 GB / 发送 3.11 GB"
  if (label === '网络流量') {
    const text = lines.join(' | ').trim()
    for (const row of liveNetworkKv) if (row.k === '网络流量') row.v = text || '-'
    return
  }

  // 6) 监听端口: 每行 port|process|localAddress
  if (label === '监听端口') {
    const rows = []
    for (const l of lines) {
      const cols = l.split('|')
      while (cols.length < 3) cols.push('')
      const port = Number(cols[0])
      const proc = (cols[1] || '').trim()
      const addr = (cols[2] || '').trim()
      rows.push({
        port: port || 0,
        process: proc,
        scope: addr === '0.0.0.0' || addr === '::' ? '全部' : (addr === '127.0.0.1' || addr === '::1' ? '本机' : addr),
        desc: ''
      })
    }
    liveListeningPorts.splice(0, liveListeningPorts.length, ...rows)
    liveListeningPortsTotal.total = rows.length
    return
  }

  // 7) 共享文件夹: 每行 name|path|description|currentUsers
  if (label === '共享文件夹') {
    const rows = []
    for (const l of lines) {
      const cols = l.split('|')
      while (cols.length < 4) cols.push('')
      rows.push({
        name: cols[0] || '',
        path: cols[1] || '',
        desc: cols[2] || '',
        conn: (cols[3] || '0').trim()
      })
    }
    liveSharedFolders.splice(0, liveSharedFolders.length, ...rows)
    return
  }
}

const runNetworkInfo = async () => {
  if (netProgress.running) return
  netProgress.running = true
  netProgress.done = 0
  netProgress.current = ''
  // 执行前先清空
  liveNetworkAdapters.splice(0, liveNetworkAdapters.length)
  liveIpAddresses.splice(0, liveIpAddresses.length)
  for (const row of liveNetworkKv) row.v = ''
  liveListeningPorts.splice(0, liveListeningPorts.length)
  liveListeningPortsTotal.total = 0
  liveSharedFolders.splice(0, liveSharedFolders.length)
  try {
    for (const step of netInfoSteps) {
      netProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applyNetworkRow(step.label, result && result.stdout)
        console.log('[runNetworkInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runNetworkInfo] fail:', step.label, err)
      } finally {
        netProgress.done += 1
      }
    }
  } finally {
    netProgress.current = '完成'
    netProgress.running = false
  }
}

const dismissNetworkProgress = () => {
  netProgress.done = 0
  netProgress.current = ''
  netProgress.running = false
}

// ================= 安全配置审计 =================
const liveSecurityKv = reactive([
  { k: '防火墙', v: '-', badge: 'g' },
  { k: '远程桌面 (RDP)', v: '-' },
  { k: 'BitLocker 加密', v: '-' },
  { k: '审计策略', v: '-' }
])
const livePasswordPolicy = reactive([
  { k: '超时后强制注销', v: '-' },
  { k: '密码最短使用期限(天)', v: '-' },
  { k: '密码最长使用期限(天)', v: '-' },
  { k: '密码最短长度', v: '-' },
  { k: '密码历史记录长度', v: '-' },
  { k: '账户锁定阈值(次)', v: '-' },
  { k: '账户锁定时长(分钟)', v: '-' },
  { k: '锁定观察窗口(分钟)', v: '-' },
  { k: '计算机角色', v: '-' }
])
const liveWindowsUpdates = reactive([])
const liveMaintenanceKv = reactive([
  { k: '时间同步', v: '-', v2: '-', badge: 'g' },
  { k: '系统还原点', v: '-' },
  { k: '电源计划', v: '-' }
])

const securityInfoSteps = [
  {
    label: '防火墙状态',
    ps: String.raw`Get-NetFirewallProfile | ForEach-Object { Write-Output "$($_.Name)|$($_.Enabled)" }`
  },
  {
    label: '远程桌面',
    ps: String.raw`Write-Output "enabled|$(if((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server' -EA SilentlyContinue).fDenyTSConnections -eq 0){'已启用'}else{'已禁用'})"; Write-Output "nla|$(if((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp' -EA SilentlyContinue).UserAuthentication -eq 1){'已启用'}else{'已禁用'})"; $portnum=$(Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp' -EA SilentlyContinue).PortNumber; Write-Output "port|$portnum"`
  },
  {
    label: 'BitLocker',
    ps: String.raw`try{Get-BitLockerVolume -EA Stop|ForEach-Object{Write-Output "$($_.MountPoint)|$($_.ProtectionStatus)|$($_.VolumeStatus)"}}catch{'unavailable'}`
  },
  {
    label: '审计策略',
    ps: String.raw`try{$a=auditpol /get /category:*; if($LASTEXITCODE -eq 0){Write-Output '已配置'}else{Write-Output '未配置'}}catch{Write-Output '不可用'}`
  },
  {
    label: '密码策略',
    ps: String.raw`net accounts`
  },
  {
    label: '系统更新',
    ps: String.raw`Get-HotFix|Sort-Object InstalledOn -Desc -EA SilentlyContinue|Select-Object -First 5|ForEach-Object{Write-Output "$($_.HotFixID)|$($_.Description)|$($_.InstalledOn.ToString('yyyy-MM-dd'))"}`
  },
  {
    label: '时间同步',
    ps: String.raw`$ts=w32tm /query /source 2>&1; $ts2=w32tm /query /status 2>&1; Write-Output "source|$($ts.Trim())"; Write-Output "status|$(if($ts2){'正常'}else{'异常'})"`
  },
  {
    label: '系统还原点',
    ps: String.raw`try{$r=Get-ComputerRestorePoint -EA Stop;Write-Output $r.Count}catch{'unavailable'}`
  },
  {
    label: '电源计划',
    ps: String.raw`powercfg /getactivescheme`
  }
]

const secProgress = reactive({
  total: securityInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const PW_LABELS_MAP = {
  'Force user logoff how long after time expires?': '超时后强制注销',
  'Minimum password age (days)': '密码最短使用期限(天)',
  'Maximum password age (days)': '密码最长使用期限(天)',
  'Minimum password length': '密码最短长度',
  'Length of password history maintained': '密码历史记录长度',
  'Lockout threshold': '账户锁定阈值(次)',
  'Lockout duration (minutes)': '账户锁定时长(分钟)',
  'Lockout observation window (minutes)': '锁定观察窗口(分钟)',
  'Computer role': '计算机角色',
}

const _applySecurityRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  if (label === '防火墙状态') {
    let fwDomain = 'OFF', fwPrivate = 'OFF', fwPublic = 'OFF'
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 2) {
        const val = (cols[1] || '').trim()
        const status = val === 'True' || val === '1' ? 'ON' : 'OFF'
        if (cols[0] === 'Domain') fwDomain = status
        else if (cols[0] === 'Private') fwPrivate = status
        else if (cols[0] === 'Public') fwPublic = status
      }
    }
    for (const row of liveSecurityKv) {
      if (row.k === '防火墙') {
        row.v = `域=${fwDomain}  专用=${fwPrivate}  公用=${fwPublic}`
        row.badge = (fwDomain === 'ON' && fwPrivate === 'ON' && fwPublic === 'ON') ? 'g' : 'r'
      }
    }
    return
  }

  if (label === '远程桌面') {
    let rdpEnabled = '已禁用', rdpNla = '已禁用', rdpPort = '3389'
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 2) {
        if (cols[0] === 'enabled') rdpEnabled = cols[1]
        else if (cols[0] === 'nla') rdpNla = cols[1]
        else if (cols[0] === 'port') rdpPort = cols[1] || '3389'
      }
    }
    for (const row of liveSecurityKv) {
      if (row.k === '远程桌面 (RDP)') {
        row.v = `已启用（NLA: ${rdpNla}，端口: ${rdpPort}）`
        row.badge = rdpEnabled === '已启用' ? 'y' : 'g'
      }
    }
    return
  }

  if (label === 'BitLocker') {
    if (lines[0] === 'unavailable') {
      for (const row of liveSecurityKv) {
        if (row.k === 'BitLocker 加密') {
          row.v = '不可用'
          row.badge = ''
        }
      }
      return
    }
    const blList = []
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 3) {
        blList.push(`${cols[0]}: ${cols[1]} ${cols[2]}`)
      }
    }
    for (const row of liveSecurityKv) {
      if (row.k === 'BitLocker 加密') {
        row.v = blList.join('、') || '未启用'
        row.badge = blList.some(b => b.includes('On')) ? 'g' : 'y'
      }
    }
    return
  }

  if (label === '审计策略') {
    const status = lines[0] || '不可用'
    for (const row of liveSecurityKv) {
      if (row.k === '审计策略') {
        row.v = status
        row.badge = status === '已配置' ? 'g' : 'y'
      }
    }
    return
  }

  if (label === '密码策略') {
    const pwData = []
    for (const l of lines) {
      if (l.includes(':')) {
        const [k, v] = l.split(':', 1).map(s => s.trim())
        const rest = l.substring(l.indexOf(':') + 1).trim()
        if (k && rest && !k.toLowerCase().includes('command') && !k.includes('成功')) {
          const label = PW_LABELS_MAP[k] || k
          pwData.push({ k: label, v: rest })
        }
      }
    }
    livePasswordPolicy.splice(0, livePasswordPolicy.length, ...pwData)
    return
  }

  if (label === '系统更新') {
    const updates = []
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 3) {
        updates.push({ kb: cols[0], type: cols[1], date: cols[2] })
      }
    }
    liveWindowsUpdates.splice(0, liveWindowsUpdates.length, ...updates)
    return
  }

  if (label === '时间同步') {
    let tsSource = '', tsStatus = '异常'
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 2) {
        if (cols[0] === 'source') tsSource = cols[1]
        else if (cols[0] === 'status') tsStatus = cols[1]
      }
    }
    for (const row of liveMaintenanceKv) {
      if (row.k === '时间同步') {
        row.v = tsStatus
        row.v2 = tsSource ? `源: ${tsSource}` : ''
        row.badge = tsStatus === '正常' ? 'g' : 'y'
      }
    }
    return
  }

  if (label === '系统还原点') {
    const count = lines[0] === 'unavailable' ? '不可用' : `${lines[0]} 个`
    for (const row of liveMaintenanceKv) {
      if (row.k === '系统还原点') {
        row.v = count
      }
    }
    return
  }

  if (label === '电源计划') {
    let plan = 'N/A'
    for (const l of lines) {
      if (l.includes('(')) {
        const idx = l.indexOf('(')
        const endIdx = l.indexOf(')', idx)
        if (endIdx > idx) {
          plan = l.substring(idx + 1, endIdx).trim()
          break
        }
      }
    }
    for (const row of liveMaintenanceKv) {
      if (row.k === '电源计划') {
        row.v = plan
      }
    }
    return
  }
}

const runSecurityInfo = async () => {
  if (secProgress.running) return
  secProgress.running = true
  secProgress.done = 0
  secProgress.current = ''
  for (const row of liveSecurityKv) { row.v = ''; row.badge = '' }
  livePasswordPolicy.splice(0, livePasswordPolicy.length)
  liveWindowsUpdates.splice(0, liveWindowsUpdates.length)
  for (const row of liveMaintenanceKv) { row.v = ''; row.v2 = ''; row.badge = '' }
  try {
    for (const step of securityInfoSteps) {
      secProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applySecurityRow(step.label, result && result.stdout)
        console.log('[runSecurityInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runSecurityInfo] fail:', step.label, err)
      } finally {
        secProgress.done += 1
      }
    }
  } finally {
    secProgress.current = '完成'
    secProgress.running = false
  }
}

const dismissSecurityProgress = () => {
  secProgress.done = 0
  secProgress.current = ''
  secProgress.running = false
}

// ================= 用户与权限 =================
const liveLocalUsers = reactive([])
const liveAdminMembers = reactive({ value: '-' })

const userInfoSteps = [
  {
    label: '本地用户',
    ps: String.raw`Get-LocalUser|ForEach-Object{Write-Output "$($_.Name)|$($_.Enabled)|$($_.LastLogon)"}`
  },
  {
    label: '管理员组成员',
    ps: String.raw`(Get-LocalGroupMember -Group 'Administrators' -EA SilentlyContinue).Name -join ', '`
  },
  {
    label: '当前登录用户',
    ps: String.raw`(quser 2>$null|Select-Object -Skip 1|ForEach-Object{$_ -replace '\s{2,}','|'}).Trim()`
  }
]

const userProgress = reactive({
  total: userInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const _applyUserRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  if (label === '本地用户') {
    const users = []
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 3) {
        const enabled = cols[1] === 'True' ? '是' : '否'
        users.push({
          name: cols[0] || '',
          enabled: enabled,
          lastLogin: cols[2] || '',
          badge: enabled === '是' ? 'g' : 'gr'
        })
      }
    }
    liveLocalUsers.splice(0, liveLocalUsers.length, ...users)
    return
  }

  if (label === '管理员组成员') {
    liveAdminMembers.value = lines.join(', ') || 'N/A'
    return
  }

  if (label === '当前登录用户') {
    return
  }
}

const runUserInfo = async () => {
  if (userProgress.running) return
  userProgress.running = true
  userProgress.done = 0
  userProgress.current = ''
  liveLocalUsers.splice(0, liveLocalUsers.length)
  liveAdminMembers.value = ''
  try {
    for (const step of userInfoSteps) {
      userProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applyUserRow(step.label, result && result.stdout)
        console.log('[runUserInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runUserInfo] fail:', step.label, err)
      } finally {
        userProgress.done += 1
      }
    }
  } finally {
    userProgress.current = '完成'
    userProgress.running = false
  }
}

const dismissUserProgress = () => {
  userProgress.done = 0
  userProgress.current = ''
  userProgress.running = false
}

// ================= 进程与服务分析 =================
const liveProcessSummary = reactive({ total: 0, services: 0 })
const liveTopMemoryProcesses = reactive([])
const procExpanded = ref(false)
const procSortKey = ref('mb')
const procSortOrder = ref('desc')

const sortedProcesses = computed(() => {
  const list = procExpanded.value ? liveTopMemoryProcesses : liveTopMemoryProcesses.slice(0, 10)
  return [...list].sort((a, b) => {
    if (procSortKey.value === 'mb') {
      const diff = parseInt(a.mb) - parseInt(b.mb)
      return procSortOrder.value === 'asc' ? diff : -diff
    } else {
      const diff = a.name.localeCompare(b.name)
      return procSortOrder.value === 'asc' ? diff : -diff
    }
  })
})

const toggleProcSort = (key) => {
  if (procSortKey.value === key) {
    procSortOrder.value = procSortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    procSortKey.value = key
    procSortOrder.value = key === 'mb' ? 'desc' : 'asc'
  }
}

const processInfoSteps = [
  {
    label: '进程列表',
    ps: String.raw`Get-Process | Select-Object ProcessName, WorkingSet64 | Sort-Object WorkingSet64 -Descending | ForEach-Object { Write-Output "$($_.ProcessName)|$([math]::Round($_.WorkingSet64 / 1MB, 0))" }`
  },
  {
    label: '进程数',
    ps: String.raw`(Get-Process).Count`
  },
  {
    label: '运行中服务',
    ps: String.raw`(Get-Service | Where-Object { $_.Status -eq 'Running' }).Count`
  }
]

const procProgress = reactive({
  total: processInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const _applyProcessRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  if (label === '进程列表') {
    const procs = []
    for (const l of lines) {
      const cols = l.split('|')
      if (cols.length >= 2) {
        procs.push({
          name: cols[0] || '',
          mb: Number(cols[1]) || 0
        })
      }
    }
    liveTopMemoryProcesses.splice(0, liveTopMemoryProcesses.length, ...procs)
    return
  }

  if (label === '进程数') {
    liveProcessSummary.total = Number(lines[0]) || 0
    return
  }

  if (label === '运行中服务') {
    liveProcessSummary.services = Number(lines[0]) || 0
    return
  }
}

const runProcessInfo = async () => {
  if (procProgress.running) return
  procProgress.running = true
  procProgress.done = 0
  procProgress.current = ''
  liveProcessSummary.total = 0
  liveProcessSummary.services = 0
  liveTopMemoryProcesses.splice(0, liveTopMemoryProcesses.length)
  try {
    for (const step of processInfoSteps) {
      procProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applyProcessRow(step.label, result && result.stdout)
        console.log('[runProcessInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runProcessInfo] fail:', step.label, err)
      } finally {
        procProgress.done += 1
      }
    }
  } finally {
    procProgress.current = '完成'
    procProgress.running = false
  }
}

const dismissProcessProgress = () => {
  procProgress.done = 0
  procProgress.current = ''
  procProgress.running = false
}

// ================= 启动项与计划任务 =================
const liveStartupItems = reactive([])
const liveScheduledTasks = reactive([])
const liveScheduledTasksTotal = ref(0)
const taskExpanded = ref(false)
const taskSortKey = ref('name')
const taskSortOrder = ref('asc')

const startupInfoSteps = [
  {
    label: '启动项',
    ps: String.raw`$items=@();'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run'|ForEach-Object{if(Test-Path $_){(Get-ItemProperty $_ -EA SilentlyContinue).PSObject.Properties|Where-Object{$_.Name -notlike 'PS*'}|ForEach-Object{ Write-Output "$($_.Name)|$($_.Value)"}}}`
  },
  {
    label: '计划任务',
    ps: String.raw`Get-ScheduledTask -EA SilentlyContinue|Where-Object{$_.TaskPath -notlike '\Microsoft\*' -and $_.State -ne 'Disabled'}|ForEach-Object{$a=($_.Actions|ForEach-Object{$_.Execute}) -join ';';Write-Output "$($_.TaskName)|$($_.State)|$a"}`
  }
]

const startupProgress = reactive({
  total: startupInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const sortedTasks = computed(() => {
  const list = taskExpanded.value ? liveScheduledTasks : liveScheduledTasks.slice(0, 10)
  return [...list].sort((a, b) => {
    let diff = 0
    if (taskSortKey.value === 'name') {
      diff = a.name.localeCompare(b.name)
    } else if (taskSortKey.value === 'status') {
      diff = a.status.localeCompare(b.status)
    } else if (taskSortKey.value === 'cmd') {
      diff = a.cmd.localeCompare(b.cmd)
    }
    return taskSortOrder.value === 'asc' ? diff : -diff
  })
})

const toggleTaskSort = (key) => {
  if (taskSortKey.value === key) {
    taskSortOrder.value = taskSortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    taskSortKey.value = key
    taskSortOrder.value = 'asc'
  }
}

const _applyStartupRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  if (label === '启动项') {
    const items = []
    for (const l of lines) {
      const p = l.split('|', 2)
      if (p.length === 2) {
        items.push({
          name: p[0] || '',
          cmd: p[1] || ''
        })
      }
    }
    liveStartupItems.splice(0, liveStartupItems.length, ...items)
    return
  }

  if (label === '计划任务') {
    const tasks = []
    for (const l of lines) {
      const p = l.split('|')
      if (p.length >= 3) {
        tasks.push({
          name: p[0] || '',
          status: p[1] || '',
          cmd: p[2] || ''
        })
      }
    }
    liveScheduledTasks.splice(0, liveScheduledTasks.length, ...tasks)
    liveScheduledTasksTotal.value = tasks.length
    return
  }
}

const runStartupInfo = async () => {
  if (startupProgress.running) return
  startupProgress.running = true
  startupProgress.done = 0
  startupProgress.current = ''
  liveStartupItems.splice(0, liveStartupItems.length)
  liveScheduledTasks.splice(0, liveScheduledTasks.length)
  liveScheduledTasksTotal.value = 0
  try {
    for (const step of startupInfoSteps) {
      startupProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applyStartupRow(step.label, result && result.stdout)
        console.log('[runStartupInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runStartupInfo] fail:', step.label, err)
      } finally {
        startupProgress.done += 1
      }
    }
  } finally {
    startupProgress.current = '完成'
    startupProgress.running = false
  }
}

const dismissStartupProgress = () => {
  startupProgress.done = 0
  startupProgress.current = ''
  startupProgress.running = false
}

// ================= 已安装软件 =================
const liveInstalledSoftware = reactive([])
const liveInstalledSoftwareTotal = ref(0)
const swExpanded = ref(false)
const swSortKey = ref('name')
const swSortOrder = ref('asc')

const softwareInfoSteps = [
  {
    label: '已安装软件',
    ps: String.raw`$apps=@();'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'|ForEach-Object{Get-ItemProperty $_ -EA SilentlyContinue|Where-Object{$_.DisplayName}|ForEach-Object{$date=$_.InstallDate;if($date -and $date.Length -eq 8){$date=$date.Substring(0,4)+'-'+$date.Substring(4,2)+'-'+$date.Substring(6,2)};$apps+="$($_.DisplayName)|$($_.DisplayVersion)|$($_.Publisher)|$date"}};Write-Output "COUNT:$($apps.Count)";$apps|Sort-Object|Get-Unique|ForEach-Object{Write-Output $_}`
  }
]

const swProgress = reactive({
  total: softwareInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const sortedSoftware = computed(() => {
  const list = swExpanded.value ? liveInstalledSoftware : liveInstalledSoftware.slice(0, 10)
  return [...list].sort((a, b) => {
    let diff = 0
    if (swSortKey.value === 'name') {
      diff = a.name.localeCompare(b.name)
    } else if (swSortKey.value === 'version') {
      diff = a.version.localeCompare(b.version)
    } else if (swSortKey.value === 'publisher') {
      diff = a.publisher.localeCompare(b.publisher)
    } else if (swSortKey.value === 'date') {
      diff = a.date.localeCompare(b.date)
    }
    return swSortOrder.value === 'asc' ? diff : -diff
  })
})

const toggleSwSort = (key) => {
  if (swSortKey.value === key) {
    swSortOrder.value = swSortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    swSortKey.value = key
    swSortOrder.value = 'asc'
  }
}

const _applySoftwareRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  if (label === '已安装软件') {
    const apps = []
    let count = 0
    for (const l of lines) {
      if (l.startsWith('COUNT:')) {
        count = Number(l.replace('COUNT:', '')) || 0
      } else if (l.includes('|')) {
        const p = l.split('|')
        if (p.length >= 4) {
          apps.push({
            name: p[0] || '',
            version: p[1] || '',
            publisher: p[2] || '',
            date: p[3] || ''
          })
        }
      }
    }
    liveInstalledSoftwareTotal.value = count
    liveInstalledSoftware.splice(0, liveInstalledSoftware.length, ...apps)
    return
  }
}

const runSoftwareInfo = async () => {
  if (swProgress.running) return
  swProgress.running = true
  swProgress.done = 0
  swProgress.current = ''
  liveInstalledSoftwareTotal.value = 0
  liveInstalledSoftware.splice(0, liveInstalledSoftware.length)
  try {
    for (const step of softwareInfoSteps) {
      swProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applySoftwareRow(step.label, result && result.stdout)
        console.log('[runSoftwareInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runSoftwareInfo] fail:', step.label, err)
      } finally {
        swProgress.done += 1
      }
    }
  } finally {
    swProgress.current = '完成'
    swProgress.running = false
  }
}

const dismissSoftwareProgress = () => {
  swProgress.done = 0
  swProgress.current = ''
  swProgress.running = false
}

// ================= 事件日志分析 =================
const liveSystemLogSummary = reactive({ errors: 0, categories: 0, timeRange: '- ~ -' })
const liveSystemLogEvents = reactive([])
const liveAppLogSummary = reactive({ errors: 0, categories: 0, timeRange: '- ~ -' })
const liveAppLogEvents = reactive([])

const logInfoSteps = [
  {
    label: '系统日志',
    ps: String.raw`Get-WinEvent -FilterHashtable @{LogName='System';Level=2} -MaxEvents 200 -EA SilentlyContinue | ForEach-Object { $msg = $_.Message.Split([char]10)[0]; if ($msg.Length -gt 120) { $msg = $msg.Substring(0, 120) }; Write-Output "$($_.Id)|$($_.ProviderName)|$($_.TimeCreated.ToString('yyyy-MM-dd HH:mm'))|$msg" }`
  },
  {
    label: '应用日志',
    ps: String.raw`Get-WinEvent -FilterHashtable @{LogName='Application';Level=2} -MaxEvents 200 -EA SilentlyContinue | ForEach-Object { $msg = $_.Message.Split([char]10)[0]; if ($msg.Length -gt 120) { $msg = $msg.Substring(0, 120) }; Write-Output "$($_.Id)|$($_.ProviderName)|$($_.TimeCreated.ToString('yyyy-MM-dd HH:mm'))|$msg" }`
  }
]

const EVENT_KB = {
  '6008|EventLog': ['高', '上次系统关机不正常（非正常断电或蓝屏）'],
  '41|Microsoft-Windows-Kernel-Power': ['高', '系统意外重启（Kernel-Power 41）'],
  '129|Disk': ['高', '磁盘控制器超时或I/O错误'],
  '20|Microsoft-Windows-WindowsUpdateClient': ['中', 'Windows 更新安装失败'],
  '10010|Microsoft-Windows-DistributedCOM': ['低', 'DCOM 服务器未在超时时间内注册'],
  '5719|NETLOGON': ['低', '无法建立安全会话'],
  '1000|Application Error': ['中', '应用程序崩溃'],
  '1002|Application Hang': ['低', '应用程序挂起'],
  '1023|Microsoft-Windows-Perflib': ['低', '无法加载性能计数器'],
  '1014|Microsoft-Windows-Security-SPP': ['低', '许可证获取失败'],
  '8200|Microsoft-Windows-Security-SPP': ['低', '许可证获取失败'],
  '8198|Microsoft-Windows-Security-SPP': ['低', '许可证激活失败']
}

const _getEventInfo = (key) => {
  if (EVENT_KB[key]) {
    return EVENT_KB[key]
  }
  return ['低', '']
}

const _parseLogLines = (lines) => {
  const groups = {}
  for (const l of lines) {
    const p = l.split('|', 4)
    if (p.length >= 4) {
      const key = p[0] + '|' + p[1]
      if (!groups[key]) {
        groups[key] = { id: p[0], source: p[1], count: 0, first: p[2], last: p[2], msg: p[3] }
      }
      groups[key].count += 1
      groups[key].last = p[2]
      if (!groups[key].first || p[2] < groups[key].first) {
        groups[key].first = p[2]
      }
    }
  }
  const sorted = Object.values(groups).sort((a, b) => b.count - a.count)
  const events = []
  let total = 0
  const times = []
  for (const g of sorted) {
    total += g.count
    times.push(g.first, g.last)
    const [level, kbDesc] = _getEventInfo(g.id + '|' + g.source)
    const levelBadge = level === '高' ? 'r' : level === '中' ? 'y' : 'gr'
    events.push({
      problem: g.source + ' (Event ' + g.id + ')',
      count: g.count,
      level: level,
      levelBadge: levelBadge,
      time: g.last,
      desc: kbDesc || (g.msg || '').substring(0, 120)
    })
  }
  times.sort()
  const timeRange = times.length >= 2 ? times[0] + ' ~ ' + times[times.length - 1] : 'N/A'
  return { summary: { errors: total, categories: sorted.length, timeRange }, events: events }
}

const logProgress = reactive({
  total: logInfoSteps.length,
  done: 0,
  current: '',
  running: false
})

const _applyLogRow = (label, stdout) => {
  const lines = (stdout || '').split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
  if (lines.length === 0) return

  const result = _parseLogLines(lines)

  if (label === '系统日志') {
    Object.assign(liveSystemLogSummary, result.summary)
    liveSystemLogEvents.splice(0, liveSystemLogEvents.length, ...result.events)
    return
  }

  if (label === '应用日志') {
    Object.assign(liveAppLogSummary, result.summary)
    liveAppLogEvents.splice(0, liveAppLogEvents.length, ...result.events)
    return
  }
}

const runLogInfo = async () => {
  if (logProgress.running) return
  logProgress.running = true
  logProgress.done = 0
  logProgress.current = ''
  Object.assign(liveSystemLogSummary, { errors: 0, categories: 0, timeRange: '' })
  liveSystemLogEvents.splice(0, liveSystemLogEvents.length)
  Object.assign(liveAppLogSummary, { errors: 0, categories: 0, timeRange: '' })
  liveAppLogEvents.splice(0, liveAppLogEvents.length)
  try {
    for (const step of logInfoSteps) {
      logProgress.current = step.label
      try {
        const payload = (step.bat && step.bat.trim())
          ? { bat: step.bat }
          : { ps: step.ps }
        const result = await execCommand(payload)
        _applyLogRow(step.label, result && result.stdout)
        console.log('[runLogInfo]', step.label, '→', result && result.stdout)
      } catch (err) {
        console.warn('[runLogInfo] fail:', step.label, err)
      } finally {
        logProgress.done += 1
      }
    }
  } finally {
    logProgress.current = '完成'
    logProgress.running = false
  }
}

const dismissLogProgress = () => {
  logProgress.done = 0
  logProgress.current = ''
  logProgress.running = false
}

// ================= 事件日志分析 =================
</script>

<template>
  <div class="report-wrap">
    <!-- 主机基本信息 -->
    <section :id="sections[0].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[0].title }}</h2>
        <button type="button" class="sec-btn" @click="runHostInfo" :disabled="hostProgress.running">
          {{ hostProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="hostProgress.done > 0 || hostProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ hostProgress.running ? '正在获取：' + hostProgress.current : hostProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ hostProgress.done }} / {{ hostProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissHostProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div
            class="host-progress-inner"
            :style="{ width: (hostProgress.done / hostProgress.total * 100) + '%' }"
          ></div>
        </div>
      </div>

      <table class="kv">
        <tr v-for="(it, idx) in liveHostInfo" :key="idx">
          <td>{{ it.k }}</td>
          <td>{{ it.v }}</td>
        </tr>
      </table>
    </section>

    <!-- 二 硬件资源 -->
    <section :id="sections[1].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[1].title }}</h2>
        <button type="button" class="sec-btn" @click="runHardwareInfo" :disabled="hwProgress.running">
          {{ hwProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="hwProgress.done > 0 || hwProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ hwProgress.running ? '正在获取：' + hwProgress.current : hwProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ hwProgress.done }} / {{ hwProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissHardwareProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (hwProgress.done / hwProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <div class="summary-cards">
        <div class="sc" v-for="(it, i) in liveHardwareSummary" :key="i">
          <div class="v">
            <div class="bar">
              <div class="bar-inner" :style="{ width: it.value + '%', background: it.color }"></div>
            </div>
            <b>{{ it.value }}%</b>
          </div>
          <div class="l">{{ it.label }}</div>
        </div>
      </div>

      <h3>处理器</h3>
      <table class="kv">
        <tr v-for="(it, idx) in liveCpuInfo" :key="idx">
          <td>{{ it.k }}</td>
          <td>{{ it.v }}</td>
        </tr>
      </table>

      <h3>内存</h3>
      <table>
        <thead>
          <tr><th>制造商</th><th>容量</th><th>频率</th><th>型号</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveMemoryInfo" :key="idx">
            <td>{{ it.maker }}</td><td>{{ it.cap }}</td><td>{{ it.freq }}</td><td>{{ it.model }}</td>
          </tr>
        </tbody>
      </table>

      <h3>磁盘存储</h3>
      <table>
        <thead>
          <tr><th>盘符</th><th>卷标</th><th>总容量</th><th>可用</th><th>已用</th><th>使用率</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveDiskInfo" :key="idx">
            <td><b>{{ it.drive }}</b></td>
            <td>{{ it.label }}</td>
            <td>{{ it.total }}</td>
            <td>{{ it.free }}</td>
            <td>{{ it.used }}</td>
            <td>
              <div class="bar-sm"><div class="bar-sm-inner" :style="{ width: it.usage + '%' }"></div></div>
              <b>{{ it.usage }}%</b>
            </td>
          </tr>
        </tbody>
      </table>

      <h3>物理磁盘健康</h3>
      <table>
        <thead>
          <tr><th>名称</th><th>类型</th><th>容量</th><th>总线</th><th>健康状态</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in livePhysicalDisk" :key="idx">
            <td>{{ it.name }}</td>
            <td>{{ it.type }}</td>
            <td>{{ it.cap }}</td>
            <td>{{ it.bus }}</td>
            <td><span class="badge" :class="it.badge">{{ it.health }}</span></td>
          </tr>
        </tbody>
      </table>

      <h3>GPU</h3>
      <table class="kv">
        <tbody>
          <tr v-for="(it, idx) in liveGpuInfo" :key="idx">
            <td>{{ it.k }}</td>
            <td>{{ it.v }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 三 网络 -->
    <section :id="sections[2].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[2].title }}</h2>
        <button type="button" class="sec-btn" @click="runNetworkInfo" :disabled="netProgress.running">
          {{ netProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="netProgress.done > 0 || netProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ netProgress.running ? '正在获取：' + netProgress.current : netProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ netProgress.done }} / {{ netProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissNetworkProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (netProgress.done / netProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <h3>网络适配器</h3>
      <table>
        <thead>
          <tr><th>适配器</th><th>状态</th><th>速率</th><th>MAC 地址</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveNetworkAdapters" :key="idx">
            <td>{{ it.name }}</td>
            <td><span class="badge" :class="it.badge">{{ it.status }}</span></td>
            <td>{{ it.speed }}</td>
            <td><code>{{ it.mac }}</code></td>
          </tr>
        </tbody>
      </table>

      <h3>IP 地址分配</h3>
      <table>
        <thead>
          <tr><th>适配器</th><th>IPv4 地址</th><th>子网</th><th>来源</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveIpAddresses" :key="idx">
            <td>{{ it.adapter }}</td>
            <td><b>{{ it.ip }}</b></td>
            <td>{{ it.subnet }}</td>
            <td>{{ it.source }}</td>
          </tr>
        </tbody>
      </table>

      <table class="kv">
        <tbody>
          <tr v-for="(it, idx) in liveNetworkKv" :key="idx">
            <td>{{ it.k }}</td>
            <td><span style="white-space:pre-wrap;word-break:break-all">{{ it.v }}</span></td>
          </tr>
        </tbody>
      </table>

      <h3>监听端口（共 {{ liveListeningPortsTotal.total }} 个，显示{{ portExpanded ? liveListeningPorts.length : 10 }}条）</h3>
      <table>
        <thead>
          <tr>
            <th @click="togglePortSort('port')" class="sortable">{{ portSortKey === 'port' ? (portSortOrder === 'asc' ? '↑' : '↓') : '' }} 端口</th>
            <th @click="togglePortSort('process')" class="sortable">{{ portSortKey === 'process' ? (portSortOrder === 'asc' ? '↑' : '↓') : '' }} 进程</th>
            <th @click="togglePortSort('scope')" class="sortable">{{ portSortKey === 'scope' ? (portSortOrder === 'asc' ? '↑' : '↓') : '' }} 监听地址</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in sortedPorts" :key="idx">
            <td><b>{{ it.port }}</b></td>
            <td>{{ it.process }}</td>
            <td>{{ it.scope }}</td>
          </tr>
          <tr v-if="liveListeningPorts.length > 10" @click="portExpanded = !portExpanded" style="cursor: pointer;">
            <td colspan="3" class="expand-row">{{ portExpanded ? '点击此处收缩为仅显示10条' : '... 共 ' + liveListeningPorts.length + ' 个端口，点击展开显示全部' }}</td>
          </tr>
        </tbody>
      </table>

      <h3>共享文件夹</h3>
      <table>
        <thead>
          <tr><th>名称</th><th>路径</th><th>说明</th><th>当前连接</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveSharedFolders" :key="idx">
            <td>{{ it.name }}</td>
            <td>{{ it.path }}</td>
            <td>{{ it.desc }}</td>
            <td>{{ it.conn }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 四 安全 -->
    <section :id="sections[3].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[3].title }}</h2>
        <button type="button" class="sec-btn" @click="runSecurityInfo" :disabled="secProgress.running">
          {{ secProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="secProgress.done > 0 || secProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ secProgress.running ? '正在获取：' + secProgress.current : secProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ secProgress.done }} / {{ secProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissSecurityProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (secProgress.done / secProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <h3>防护状态</h3>
      <table class="kv">
        <tbody>
          <tr v-for="(it, idx) in liveSecurityKv" :key="idx">
            <td>{{ it.k }}</td>
            <td>
              <span v-if="it.badge" class="badge" :class="it.badge">{{ it.v }}</span>
              <span v-else>{{ it.v }}</span>
            </td>
          </tr>
        </tbody>
      </table>

      <h3>密码策略</h3>
      <table class="kv">
        <tbody>
          <tr v-for="(it, idx) in livePasswordPolicy" :key="idx">
            <td>{{ it.k }}</td>
            <td>{{ it.v }}</td>
          </tr>
        </tbody>
      </table>

      <h3>系统更新</h3>
      <table>
        <thead>
          <tr><th>补丁号</th><th>类型</th><th>安装日期</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveWindowsUpdates" :key="idx">
            <td><b>{{ it.kb }}</b></td>
            <td>{{ it.type }}</td>
            <td>{{ it.date }}</td>
          </tr>
        </tbody>
      </table>

      <h3>系统维护</h3>
      <table class="kv">
        <tbody>
          <tr v-for="(it, idx) in liveMaintenanceKv" :key="idx">
            <td>{{ it.k }}</td>
            <td>
              <span v-if="it.badge" class="badge" :class="it.badge">{{ it.v }}</span>
              <span v-else>{{ it.v }}</span>
              <span v-if="it.v2"> {{ it.v2 }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 五 用户与权限 -->
    <section :id="sections[4].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[4].title }}</h2>
        <button type="button" class="sec-btn" @click="runUserInfo" :disabled="userProgress.running">
          {{ userProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="userProgress.done > 0 || userProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ userProgress.running ? '正在获取：' + userProgress.current : userProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ userProgress.done }} / {{ userProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissUserProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (userProgress.done / userProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <h3>本地用户账户</h3>
      <table>
        <thead>
          <tr><th>用户名</th><th>启用</th><th>最后登录</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveLocalUsers" :key="idx">
            <td>{{ it.name }}</td>
            <td><span class="badge" :class="it.badge">{{ it.enabled }}</span></td>
            <td>{{ it.lastLogin }}</td>
          </tr>
        </tbody>
      </table>

      <h3>管理员组成员</h3>
      <p style="padding:8px 0">{{ liveAdminMembers.value }}</p>
    </section>

    <!-- 六 进程与服务 -->
    <section :id="sections[5].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[5].title }}</h2>
        <button type="button" class="sec-btn" @click="runProcessInfo" :disabled="procProgress.running">
          {{ procProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="procProgress.done > 0 || procProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ procProgress.running ? '正在获取：' + procProgress.current : procProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ procProgress.done }} / {{ procProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissProcessProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (procProgress.done / procProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <p>当前进程数: <b>{{ liveProcessSummary.total }}</b> / 运行中服务: <b>{{ liveProcessSummary.services }}</b></p>

      <h3>内存占用 Top {{ procExpanded ? liveTopMemoryProcesses.length : 10 }}</h3>
      <table>
        <thead>
          <tr>
            <th @click="toggleProcSort('name')" class="sortable">{{ procSortKey === 'name' ? (procSortOrder === 'asc' ? '↑' : '↓') : '' }} 进程名</th>
            <th @click="toggleProcSort('mb')" class="sortable">{{ procSortKey === 'mb' ? (procSortOrder === 'asc' ? '↑' : '↓') : '' }} 内存占用</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in sortedProcesses" :key="idx">
            <td>{{ it.name }}</td>
            <td><b>{{ it.mb }}</b> MB</td>
          </tr>
          <tr v-if="liveTopMemoryProcesses.length > 10" @click="procExpanded = !procExpanded" style="cursor: pointer;">
            <td colspan="2" class="expand-row">{{ procExpanded ? '点击此处收缩为仅显示10条' : '... 共 ' + liveTopMemoryProcesses.length + ' 个进程，点击展开显示全部' }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 七 启动项与计划任务 -->
    <section :id="sections[6].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[6].title }}</h2>
        <button type="button" class="sec-btn" @click="runStartupInfo" :disabled="startupProgress.running">
          {{ startupProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="startupProgress.done > 0 || startupProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ startupProgress.running ? '正在获取：' + startupProgress.current : startupProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ startupProgress.done }} / {{ startupProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissStartupProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (startupProgress.done / startupProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <h3>注册表启动项</h3>
      <table>
        <thead>
          <tr><th>名称</th><th>命令</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveStartupItems" :key="idx">
            <td><b>{{ it.name }}</b></td>
            <td style="font-size:12px;word-break:break-all">{{ it.cmd }}</td>
          </tr>
        </tbody>
      </table>

      <h3>计划任务（非微软任务，共 {{ liveScheduledTasksTotal }} 个，显示{{ taskExpanded ? liveScheduledTasks.length : 10 }}条）</h3>
      <table>
        <thead>
          <tr>
            <th @click="toggleTaskSort('name')" class="sortable">{{ taskSortKey === 'name' ? (taskSortOrder === 'asc' ? '↑' : '↓') : '' }} 任务名</th>
            <th @click="toggleTaskSort('status')" class="sortable">{{ taskSortKey === 'status' ? (taskSortOrder === 'asc' ? '↑' : '↓') : '' }} 状态</th>
            <th @click="toggleTaskSort('cmd')" class="sortable">{{ taskSortKey === 'cmd' ? (taskSortOrder === 'asc' ? '↑' : '↓') : '' }} 操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in sortedTasks" :key="idx">
            <td style="font-size:12px">{{ it.name }}</td>
            <td>{{ it.status }}</td>
            <td style="font-size:12px;word-break:break-all">{{ it.cmd }}</td>
          </tr>
          <tr v-if="liveScheduledTasks.length > 10" @click="taskExpanded = !taskExpanded" style="cursor: pointer;">
            <td colspan="3" class="expand-row">{{ taskExpanded ? '点击此处收缩为仅显示10条' : '... 共 ' + liveScheduledTasks.length + ' 个任务，点击展开显示全部' }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 八 已安装软件 -->
    <section :id="sections[7].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[7].title }}（共 {{ liveInstalledSoftwareTotal }} 个，显示{{ swExpanded ? liveInstalledSoftware.length : 10 }}条）</h2>
        <button type="button" class="sec-btn" @click="runSoftwareInfo" :disabled="swProgress.running">
          {{ swProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="swProgress.done > 0 || swProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ swProgress.running ? '正在获取：' + swProgress.current : swProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ swProgress.done }} / {{ swProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissSoftwareProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (swProgress.done / swProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <table>
        <thead>
          <tr>
            <th @click="toggleSwSort('name')" class="sortable">{{ swSortKey === 'name' ? (swSortOrder === 'asc' ? '↑' : '↓') : '' }} 名称</th>
            <th @click="toggleSwSort('version')" class="sortable">{{ swSortKey === 'version' ? (swSortOrder === 'asc' ? '↑' : '↓') : '' }} 版本</th>
            <th @click="toggleSwSort('publisher')" class="sortable">{{ swSortKey === 'publisher' ? (swSortOrder === 'asc' ? '↑' : '↓') : '' }} 发布者</th>
            <th @click="toggleSwSort('date')" class="sortable">{{ swSortKey === 'date' ? (swSortOrder === 'asc' ? '↑' : '↓') : '' }} 安装日期</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in sortedSoftware" :key="idx">
            <td>{{ it.name }}</td>
            <td>{{ it.version }}</td>
            <td>{{ it.publisher }}</td>
            <td>{{ it.date }}</td>
          </tr>
          <tr v-if="liveInstalledSoftware.length > 10" @click="swExpanded = !swExpanded" style="cursor: pointer;">
            <td colspan="4" class="expand-row">{{ swExpanded ? '点击此处收缩为仅显示10条' : '... 共 ' + liveInstalledSoftware.length + ' 个软件，点击展开显示全部' }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 九 事件日志 -->
    <section :id="sections[8].id" class="sec">
      <div class="sec-header">
        <h2 class="sec-title">{{ sections[8].title }}</h2>
        <button type="button" class="sec-btn" @click="runLogInfo" :disabled="logProgress.running">
          {{ logProgress.running ? '执行中...' : '执行' }}
        </button>
      </div>

      <div class="host-progress" v-if="logProgress.done > 0 || logProgress.running">
        <div class="host-progress-top">
          <span class="host-progress-label">
            {{ logProgress.running ? '正在获取：' + logProgress.current : logProgress.current || '完成' }}
          </span>
          <span class="host-progress-count">{{ logProgress.done }} / {{ logProgress.total }}</span>
          <button type="button" class="host-progress-close" @click="dismissLogProgress" title="关闭进度条">×</button>
        </div>
        <div class="host-progress-bar">
          <div class="host-progress-inner" :style="{ width: (logProgress.done / logProgress.total * 100) + '%' }"></div>
        </div>
      </div>

      <h3 style="margin-bottom:10px;font-size:13px">
        <b>系统日志</b>: 共 <b>{{ liveSystemLogSummary.errors }}</b> 条错误，
        {{ liveSystemLogSummary.categories }} 类事件，时间范围 {{ liveSystemLogSummary.timeRange }}
      </h3>
      <table>
        <thead>
          <tr><th style="min-width:200px">问题</th><th style="width:60px">次数</th><th style="width:90px">严重程度</th><th>时间范围</th><th>说明</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveSystemLogEvents" :key="idx">
            <td style="font-size:12px">{{ it.problem }}</td>
            <td style="text-align:center"><b>{{ it.count }}</b></td>
            <td style="text-align:center"><span class="badge" :class="it.levelBadge">{{ it.level }}</span></td>
            <td style="font-size:12px;white-space:nowrap">{{ it.time }}</td>
            <td style="font-size:12px">{{ it.desc }}</td>
          </tr>
        </tbody>
      </table>

      <h3 style="margin-bottom:10px;font-size:13px;margin-top:18px">
        <b>应用日志</b>: 共 <b>{{ liveAppLogSummary.errors }}</b> 条错误，
        {{ liveAppLogSummary.categories }} 类事件，时间范围 {{ liveAppLogSummary.timeRange }}
      </h3>
      <table>
        <thead>
          <tr><th style="min-width:200px">问题</th><th style="width:60px">次数</th><th style="width:90px">严重程度</th><th>时间范围</th><th>说明</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, idx) in liveAppLogEvents" :key="idx">
            <td style="font-size:12px">{{ it.problem }}</td>
            <td style="text-align:center"><b>{{ it.count }}</b></td>
            <td style="text-align:center"><span class="badge" :class="it.levelBadge">{{ it.level }}</span></td>
            <td style="font-size:12px;white-space:nowrap">{{ it.time }}</td>
            <td style="font-size:12px">{{ it.desc }}</td>
          </tr>
        </tbody>
      </table>
    </section>

  </div>
</template>

<style scoped>
.report-wrap {
  background: #fff;
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.06);
  border-radius: 4px;
  overflow: hidden;
}

/* 头部信息栏 */
.report-header {
  background: linear-gradient(135deg, #2a3142 0%, #1f2430 100%);
  color: #e6edf3;
  padding: 22px 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}
.rh-title {
  font-size: 20px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 1px;
}
.rh-host {
  font-size: 12.5px;
  color: #a9b2c0;
  margin-top: 4px;
}
.rh-meta {
  display: flex;
  align-items: center;
  gap: 28px;
  flex-wrap: wrap;
}
.rh-btn {
  background: #4c9aff;
  color: #fff;
  border: none;
  padding: 7px 16px;
  font-size: 13px;
  font-weight: 600;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s ease, transform 0.1s ease;
  font-family: inherit;
}
.rh-btn:hover {
  background: #3c8aef;
}
.rh-btn:active {
  transform: translateY(1px);
}
.rh-item {
  font-size: 13px;
  color: #c9d1d9;
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.rh-item span {
  color: #8a94a6;
}
.rh-item b {
  color: #fff;
  font-weight: 600;
}
.rh-item .risk {
  color: #f9ab00;
  font-weight: 700;
}

/* 章节 */
.sec {
  padding: 10px 40px 30px;
}
.sec-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 20px -40px 14px;
  background: #2a3142;
  padding: 8px 40px;
}
.sec-title {
  font-size: 15px;
  color: #fff;
  letter-spacing: 0.5px;
  font-weight: 600;
  margin: 0;
  padding: 0;
  background: transparent;
}
.sec-btn {
  background: #4c9aff;
  color: #fff;
  border: none;
  padding: 6px 16px;
  font-size: 13px;
  font-weight: 600;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s ease, transform 0.1s ease;
  font-family: inherit;
}
.sec-btn:hover { background: #3c8aef; }
.sec-btn:active { transform: translateY(1px); }
.sec-btn[disabled] {
  background: #7fa7d9;
  cursor: not-allowed;
}

/* 执行进度条 */
.host-progress {
  margin: 0 0 16px;
  padding: 10px 14px;
  background: #f4f6fb;
  border: 1px solid #e3e7f1;
  border-radius: 6px;
}
.host-progress-top {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  color: #333;
  margin-bottom: 8px;
}
.host-progress-label { color: #555; }
.host-progress-count {
  margin-left: auto;
  color: #4c9aff;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.host-progress-close {
  appearance: none;
  border: 1px solid #d6dcea;
  background: #fff;
  color: #666;
  width: 22px;
  height: 22px;
  line-height: 20px;
  padding: 0;
  border-radius: 50%;
  cursor: pointer;
  font-size: 16px;
  font-weight: 700;
  transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
}
.host-progress-close:hover {
  background: #fff2f2;
  border-color: #ff7a7a;
  color: #d93636;
}
.host-progress-bar {
  width: 100%;
  height: 8px;
  background: #e3e7f1;
  border-radius: 999px;
  overflow: hidden;
}
.host-progress-inner {
  height: 100%;
  background: linear-gradient(90deg, #4c9aff, #7fcdff);
  width: 0%;
  transition: width 0.25s ease;
  border-radius: 999px;
}

h3 {
  font-size: 13.5px;
  color: #222;
  margin: 16px 0 8px;
  padding-left: 10px;
  border-left: 3px solid #4c9aff;
  font-weight: 600;
}

/* 表格 */
table {
  width: 100%;
  border-collapse: collapse;
  margin: 6px 0 14px;
  font-size: 12.5px;
}
th {
  background: #f2f3f5;
  color: #555;
  font-size: 12.5px;
  font-weight: 600;
  text-align: left;
  padding: 8px 12px;
  border: 1px solid #dfe1e5;
}
td {
  padding: 7px 12px;
  border: 1px solid #dfe1e5;
  vertical-align: top;
}
tr:nth-child(even) {
  background: #fafbfc;
}
.kv td:first-child {
  background: #f2f3f5;
  font-weight: 600;
  color: #444;
  width: 180px;
  white-space: nowrap;
}

/* 标签 */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 3px;
  font-size: 11.5px;
  font-weight: 600;
  color: #fff;
}
.g { background: #34a853; }
.y { background: #e8a000; }
.r { background: #d93025; }
.b { background: #1a73e8; }
.gr { background: #9aa0a6; }

/* 资源卡片 */
.summary-cards {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin: 10px 0 20px;
}
.sc {
  background: #f7f8f9;
  border: 1px solid #e6e8eb;
  border-radius: 4px;
  padding: 14px 16px;
}
.sc .v {
  font-size: 17px;
  font-weight: 700;
  color: #2c2c2c;
  display: flex;
  align-items: center;
  gap: 10px;
}
.sc .l {
  font-size: 12px;
  color: #666;
  margin-top: 6px;
}
.bar {
  background: #e4e7eb;
  border-radius: 4px;
  height: 14px;
  width: 130px;
  overflow: hidden;
}
.bar-inner {
  height: 100%;
  transition: width 0.3s;
}
.bar-sm {
  background: #e4e7eb;
  border-radius: 3px;
  height: 12px;
  width: 110px;
  display: inline-block;
  vertical-align: middle;
  overflow: hidden;
  margin-right: 6px;
}
.bar-sm-inner {
  background: #34a853;
  height: 100%;
}

/* 风险框 */
.risk-box {
  display: inline-block;
  border: 2px solid #f9ab00;
  border-radius: 4px;
  padding: 8px 20px;
  margin: 10px 0;
}
.risk-box .level {
  font-size: 18px;
  font-weight: 700;
  color: #f9ab00;
}

p { margin: 4px 0; color: #333; font-size: 13px; line-height: 1.7; }

code {
  background: #f4f4f6;
  padding: 1px 6px;
  border-radius: 3px;
  font-family: Consolas, monospace;
  font-size: 12px;
  color: #444;
}

.footer {
  background: #2a3142;
  color: #c9d1d9;
  font-size: 12px;
  padding: 16px 40px;
  text-align: center;
  line-height: 1.8;
}
.footer b { color: #fff; }

.expand-row {
  text-align: center;
  color: #4c9aff;
  font-size: 12px;
  padding: 8px 16px;
  background: #f7f9ff;
}
.expand-row:hover {
  background: #eef3ff;
}

.sortable {
  cursor: pointer;
  user-select: none;
}
.sortable:hover {
  background: rgba(76, 154, 255, 0.1);
}

@media (max-width: 900px) {
  .summary-cards { grid-template-columns: 1fr; }
  .sec, .report-header { padding-left: 20px; padding-right: 20px; }
  .sec-header { margin-left: -20px; margin-right: -20px; padding-left: 20px; padding-right: 20px; }
}
</style>
