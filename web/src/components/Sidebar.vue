<script setup>
import { ref } from 'vue'
import { sections } from '../data/reportData.js'

const props = defineProps({
  activeId: String,
  collapsed: Boolean
})

const emit = defineEmits(['navigate', 'toggle'])

const STORAGE_KEY = 'app:baseUrl'

const settingsOpen = ref(false)
const baseUrl = ref('')
const saveTip = ref('')

const handleClick = (id) => {
  emit('navigate', id)
}

const openSettings = () => {
  baseUrl.value = (typeof localStorage !== 'undefined' && localStorage.getItem(STORAGE_KEY)) || ''
  saveTip.value = ''
  settingsOpen.value = true
}

const closeSettings = () => {
  settingsOpen.value = false
  saveTip.value = ''
}

const onOverlayClick = (e) => {
  if (e.target === e.currentTarget) closeSettings()
}

const confirmSettings = () => {
  if (typeof localStorage !== 'undefined') {
    const value = (baseUrl.value || '').trim()
    if (value) {
      localStorage.setItem(STORAGE_KEY, value)
      saveTip.value = '已保存：' + value
    } else {
      localStorage.removeItem(STORAGE_KEY)
      saveTip.value = '已清空 baseUrl'
    }
  }
  setTimeout(() => closeSettings(), 400)
}
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="sb-header">
      <div class="sb-title-line">
        <span class="sb-title">{{ collapsed ? '报告' : 'Windows 报告' }}</span>
        <button class="sb-toggle" type="button" @click="emit('toggle')" :title="collapsed ? '展开侧边栏' : '收起侧边栏'">
          <span class="sb-toggle-icon" :class="{ flipped: !collapsed }">◀</span>
        </button>
      </div>
    </div>

    <nav class="sb-nav">
      <ul>
        <li
          v-for="s in sections"
          :key="s.id"
          :class="['sb-item', { active: activeId === s.id }]"
          :title="s.title"
          @click="handleClick(s.id)"
        >
          <span class="sb-dot"></span>
          <span class="sb-label">{{ s.title }}</span>
        </li>
      </ul>
    </nav>

    <div class="sb-footer">
      <button type="button" class="sb-settings-btn" :title="collapsed ? '设置' : '偏好设置'" @click="openSettings">
        <span class="sb-settings-icon">⚙</span>
        <span v-if="!collapsed" class="sb-settings-text">设置</span>
      </button>
    </div>

    <!-- 自绘弹窗 -->
    <transition name="modal-fade">
      <div v-if="settingsOpen" class="sb-modal-overlay" @click="onOverlayClick">
        <div class="sb-modal" @click.stop>
          <div class="sb-modal-header">
            <span>设置</span>
            <button type="button" class="sb-modal-close" title="关闭" @click="closeSettings">×</button>
          </div>

          <div class="sb-modal-body">
            <div class="sb-modal-group">
              <label class="sb-input-label" for="sb-baseurl">Base URL</label>
              <input
                id="sb-baseurl"
                v-model="baseUrl"
                type="text"
                class="sb-input"
                placeholder="连接的http服务，默认 http://localhost:5000"
                @keyup.enter="confirmSettings"
              />
            </div>

            <div v-if="saveTip" class="sb-modal-note">{{ saveTip }}</div>
          </div>

          <div class="sb-modal-footer">
            <button type="button" class="sb-btn sb-btn-ghost" @click="closeSettings">取消</button>
            <button type="button" class="sb-btn sb-btn-primary" @click="confirmSettings">确定</button>
          </div>
        </div>
      </div>
    </transition>
  </aside>
</template>

<style scoped>
.sidebar {
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  width: 200px;
  background: #1f2430;
  color: #c9d1d9;
  display: flex;
  flex-direction: column;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.08);
  z-index: 10;
  transition: width 0.25s ease;
  overflow: hidden;
}

.sidebar.collapsed {
  width: 56px;
}

.sb-header {
  padding: 16px 16px 14px;
  background: #151922;
  border-bottom: 1px solid #2a3142;
  flex-shrink: 0;
}

.sb-title-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 28px;
}

.sb-title {
  font-size: 15px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sidebar.collapsed .sb-title {
  display: none;
}

.sb-toggle {
  width: 28px;
  height: 28px;
  border-radius: 4px;
  border: 1px solid #2a3142;
  background: #242a38;
  color: #b8c0cc;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  padding: 0;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s, transform 0.25s;
}
.sb-toggle:hover {
  background: #2f3749;
  color: #fff;
}
.sb-toggle-icon {
  display: inline-block;
  transition: transform 0.25s ease;
}
.sb-toggle-icon.flipped {
  transform: rotate(180deg);
}

.sb-nav {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 10px 0;
}
.sb-nav::-webkit-scrollbar {
  width: 6px;
}
.sb-nav::-webkit-scrollbar-thumb {
  background: #2a3142;
  border-radius: 3px;
}
.sb-nav ul {
  list-style: none;
  padding: 0;
  margin: 0;
}
.sb-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 18px;
  cursor: pointer;
  border-left: 3px solid transparent;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
  font-size: 13.5px;
  color: #b8c0cc;
  white-space: nowrap;
}
.sb-item:hover {
  background: #242a38;
  color: #fff;
}
.sb-item.active {
  background: #2a3142;
  color: #fff;
  border-left-color: #4c9aff;
  font-weight: 600;
}
.sb-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #4c9aff;
  flex-shrink: 0;
  opacity: 0.4;
}
.sb-item.active .sb-dot {
  opacity: 1;
}
.sb-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sidebar.collapsed .sb-label {
  display: none;
}

.sb-footer {
  padding: 12px 14px;
  background: #151922;
  border-top: 1px solid #2a3142;
  flex-shrink: 0;
}

.sb-settings-btn {
  width: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 12px;
  background: #242a38;
  border: 1px solid #2a3142;
  border-radius: 4px;
  color: #c9d1d9;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.sb-settings-btn:hover {
  background: #2f3749;
  color: #fff;
  border-color: #4c9aff;
}
.sb-settings-icon {
  font-size: 14px;
  line-height: 1;
}
.sidebar.collapsed .sb-settings-text {
  display: none;
}

/* ===== 自绘弹窗 ===== */
.sb-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(10, 15, 25, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.sb-modal {
  width: 600px;
  max-width: calc(100vw - 32px);
  background: #fff;
  color: #2c2c2c;
  border-radius: 8px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.25);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.sb-modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  background: #1f2430;
  color: #fff;
  font-size: 14px;
  font-weight: 600;
}

.sb-modal-close {
  width: 26px;
  height: 26px;
  border-radius: 4px;
  border: none;
  background: transparent;
  color: #b8c0cc;
  cursor: pointer;
  font-size: 20px;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.sb-modal-close:hover {
  background: #2f3749;
  color: #fff;
}

.sb-modal-body {
  padding: 18px 20px 12px;
  font-size: 13.5px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.sb-modal-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.sb-modal-label {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #2c2c2c;
  cursor: pointer;
}
.sb-modal-label input[type="checkbox"] {
  accent-color: #4c9aff;
  cursor: pointer;
}
.sb-modal-label label {
  cursor: pointer;
  user-select: none;
}

.sb-input-label {
  display: block;
  font-size: 13px;
  color: #444;
  font-weight: 600;
  margin-bottom: 6px;
}
.sb-input {
  width: 100%;
  padding: 8px 12px;
  font-size: 13.5px;
  border: 1px solid #dfe3e8;
  border-radius: 4px;
  background: #fff;
  color: #2c2c2c;
  outline: none;
  transition: border-color 0.15s, box-shadow 0.15s;
  box-sizing: border-box;
  font-family: inherit;
}
.sb-input::placeholder {
  color: #a9b0bd;
}
.sb-input:focus {
  border-color: #4c9aff;
  box-shadow: 0 0 0 3px rgba(76, 154, 255, 0.15);
}

.sb-modal-note {
  margin-top: 4px;
  padding: 10px 12px;
  background: #f1f4f8;
  color: #5a6478;
  font-size: 12.5px;
  border-radius: 4px;
  line-height: 1.6;
}
.sb-modal-note code {
  background: #fff;
  border: 1px solid #e6e8eb;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 12px;
}

.sb-modal-footer {
  padding: 12px 20px 16px;
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  background: #f8fafc;
  border-top: 1px solid #e6e8eb;
}

.sb-btn {
  min-width: 72px;
  padding: 7px 14px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  border: 1px solid transparent;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
}
.sb-btn-ghost {
  background: #fff;
  border-color: #dfe3e8;
  color: #2c2c2c;
}
.sb-btn-ghost:hover {
  border-color: #4c9aff;
  color: #4c9aff;
}
.sb-btn-primary {
  background: #4c9aff;
  border-color: #4c9aff;
  color: #fff;
}
.sb-btn-primary:hover {
  background: #3c8aef;
  border-color: #3c8aef;
}

/* 过渡动画 */
.modal-fade-enter-active,
.modal-fade-leave-active {
  transition: opacity 0.2s ease;
}
.modal-fade-enter-from,
.modal-fade-leave-to {
  opacity: 0;
}

@media (max-width: 900px) {
  .sidebar { display: none; }
}
</style>
