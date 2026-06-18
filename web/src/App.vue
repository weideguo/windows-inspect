<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import Sidebar from './components/Sidebar.vue'
import ReportContent from './components/ReportContent.vue'
import { sections } from './data/reportData.js'

const activeId = ref(sections[0].id)
const contentRef = ref(null)
const collapsed = ref(false)
let scrollHandler = null

const toggleSidebar = () => {
  collapsed.value = !collapsed.value
}

// 平滑滚动到目标
const scrollToId = (id) => {
  const el = document.getElementById(id)
  if (el && contentRef.value) {
    const container = contentRef.value
    const top = el.offsetTop - 20
    container.scrollTo({ top, behavior: 'smooth' })
    history.replaceState(null, '', '#' + id)
  }
}

const navigate = (id) => {
  activeId.value = id
  scrollToId(id)
}

// 滚动时更新当前激活项
const onScroll = () => {
  if (!contentRef.value) return
  const scrollTop = contentRef.value.scrollTop + 80
  let current = sections[0].id
  for (const s of sections) {
    const el = document.getElementById(s.id)
    if (el && el.offsetTop <= scrollTop) {
      current = s.id
    }
  }
  if (activeId.value !== current) {
    activeId.value = current
  }
}

onMounted(() => {
  scrollHandler = onScroll
  contentRef.value && contentRef.value.addEventListener('scroll', scrollHandler)

  // 如果 URL 中带 hash，则滚动过去
  if (location.hash) {
    const id = location.hash.slice(1)
    if (sections.some(s => s.id === id)) {
      setTimeout(() => scrollToId(id), 50)
    }
  }
})

onBeforeUnmount(() => {
  if (contentRef.value && scrollHandler) {
    contentRef.value.removeEventListener('scroll', scrollHandler)
  }
})
</script>

<template>
  <div class="app-layout" :class="{ 'is-collapsed': collapsed }">
    <Sidebar :active-id="activeId" :collapsed="collapsed" @navigate="navigate" @toggle="toggleSidebar" />
    <main class="main-content" ref="contentRef">
      <ReportContent />
    </main>
  </div>
</template>

<style>
/* 全局样式已通过 style.css 注入 */
</style>
