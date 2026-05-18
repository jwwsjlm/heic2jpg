<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import * as AppApi from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

const fallback = {
  async SelectFile(){ alert('请在 Wails 桌面应用中使用文件选择。'); return '' },
  async SelectFolder(){ alert('请在 Wails 桌面应用中使用文件夹选择。'); return '' },
  async DefaultWorkers(){ return Math.min(4, navigator.hardwareConcurrency || 4) },
  async RuntimeInfo(){ return { os: 'browser-preview', arch: '', version: 'dev', title: 'HEIC2JPG' } },
  async ConverterStatus(){ return { available: false, message: '浏览器预览无法检测本机转换器', help: '请运行 Wails 桌面应用检测 ImageMagick / libheif-tools / sips。' } },
  async StartConversion(){ throw new Error('浏览器预览不能直接转换，请运行 Wails 应用。') }
}

const api = window.go?.main?.App ? AppApi : fallback
const form = reactive({ input: '', recursive: true, overwrite: false, deleteOriginal: false, level: 10, workers: 0 })
const running = ref(false)
const progress = ref({ found: 0, done: 0, success: 0, skipped: 0, failed: 0, percent: 0, message: '等待选择文件或文件夹' })
const result = ref(null)
const error = ref('')
const info = ref({})
const converter = ref({ available: false, message: '正在检测转换器...' })
const log = ref([])
const dragging = ref(false)

const qualityText = computed(() => ({
  1:'文件最小', 2:'较小文件', 3:'轻量压缩', 4:'均衡偏小', 5:'均衡',
  6:'清晰', 7:'高清', 8:'高质量', 9:'极高质量', 10:'最高质量 / 保留元数据'
}[form.level] || '最高质量'))
const canStart = computed(() => Boolean(form.input && !running.value))
const versionLabel = computed(() => info.value.version ? `v${String(info.value.version).replace(/^v/, '')}` : 'dev')
const progressPercent = computed(() => Math.max(0, Math.min(100, Number(progress.value.percent || 0))))
const sourceName = computed(() => {
  if(!form.input) return '尚未选择来源'
  const normalized = String(form.input).replaceAll('\\', '/')
  return normalized.split('/').filter(Boolean).pop() || form.input
})
const taskState = computed(() => {
  if(running.value) return '正在转换'
  if(result.value) return '已完成'
  if(error.value) return '需要处理'
  if(form.input) return '已就绪'
  return '等待来源'
})
const metrics = computed(() => [
  { label: '发现', value: progress.value.found, tone: 'neutral' },
  { label: '已处理', value: progress.value.done, tone: 'blue' },
  { label: '成功', value: progress.value.success, tone: 'green' },
  { label: '跳过', value: progress.value.skipped, tone: 'amber' },
  { label: '失败', value: progress.value.failed, tone: 'red' },
])

function pushLog(text){
  if(!text) return
  const line = `${new Date().toLocaleTimeString()}  ${text}`
  if(log.value[0] !== line) log.value.unshift(line)
  log.value = log.value.slice(0, 80)
}

async function pickFile(){ const p = await api.SelectFile(); if(p) setInput(p, '已选择文件') }
async function pickFolder(){ const p = await api.SelectFolder(); if(p) setInput(p, '已选择文件夹') }
function setInput(path, source = '已选择来源'){
  if(!path) return
  form.input = path
  pushLog(`${source}：${path}`)
}
function onBrowserDrop(event){
  dragging.value = false
  event.preventDefault()
  const item = event.dataTransfer?.files?.[0]
  const path = item?.path || item?.webkitRelativePath || item?.name
  if(path) setInput(path, '已拖入来源')
}
function clearInput(){
  if(running.value) return
  form.input = ''
  pushLog('已清除来源')
}
function adjustWorkers(delta){
  const next = Number(form.workers || 1) + delta
  form.workers = Math.max(1, Math.min(16, next))
}

async function start(){
  if(!canStart.value) return
  running.value = true
  error.value = ''
  result.value = null
  log.value = []
  progress.value = { found: 0, done: 0, success: 0, skipped: 0, failed: 0, percent: 0, message: '准备转换' }
  try{
    const res = await api.StartConversion({...form, level: Number(form.level), workers: Number(form.workers || 0)})
    result.value = res
    progress.value = {...progress.value, percent: 100, message: '转换完成'}
    pushLog('转换完成')
  }catch(e){
    error.value = e?.message || String(e)
    pushLog('错误：' + error.value)
  }finally{
    running.value = false
  }
}

onMounted(async () => {
  form.workers = await api.DefaultWorkers()
  info.value = await api.RuntimeInfo().catch(()=>({}))
  converter.value = await api.ConverterStatus().catch(e => ({ available: false, message: e?.message || String(e) }))
  if(converter.value.available) pushLog(`检测到转换器：${converter.value.name} ${converter.value.path || ''}`)
  else pushLog(`未检测到转换器：${converter.value.message}`)
  if(window.runtime?.EventsOn){
    EventsOn('source:dropped', path => setInput(path, '已拖入来源'))
    EventsOn('conversion:progress', p => { progress.value = p; pushLog(p.message) })
    EventsOn('conversion:error', msg => { error.value = msg; running.value = false; pushLog('错误：' + msg) })
    EventsOn('conversion:done', res => { result.value = res; running.value = false; pushLog('转换完成') })
  }
})
</script>

<template>
  <main class="app-shell">
    <header class="topbar">
      <div class="brand">
        <div class="brand-mark" aria-hidden="true">HJ</div>
        <div>
          <p class="kicker">HEIC2JPG Desktop</p>
          <h1>HEIC 转 JPG</h1>
        </div>
      </div>
      <div class="top-meta" aria-label="应用状态">
        <span class="status-chip" :class="{ ready: converter.available }">
          <i aria-hidden="true"></i>{{ converter.available ? '转换器已就绪' : '等待转换器' }}
        </span>
        <span>{{ info.os || 'desktop' }} {{ info.arch }}</span>
        <span>{{ versionLabel }}</span>
      </div>
    </header>

    <section class="workspace">
      <aside class="panel control-panel">
        <div class="panel-heading">
          <p class="kicker">Input</p>
          <h2>选择来源</h2>
        </div>

        <div class="drop-target" :class="{ populated: form.input, dragging }" @dragenter.prevent="dragging = true" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop="onBrowserDrop">
          <div class="drop-glyph" aria-hidden="true"></div>
          <p class="drop-title">{{ form.input ? sourceName : '拖入 HEIC 文件或文件夹' }}</p>
          <p class="drop-copy">{{ form.input || '支持 HEIC / HEIF 文件和文件夹，也可以用下方按钮选择。' }}</p>
        </div>

        <div class="button-grid">
          <button class="button primary" @click="pickFile" :disabled="running">选择文件</button>
          <button class="button secondary" @click="pickFolder" :disabled="running">选择文件夹</button>
          <button class="button ghost" @click="clearInput" :disabled="running || !form.input">清除来源</button>
        </div>

        <div class="section-divider"></div>

        <div class="panel-heading compact">
          <p class="kicker">Settings</p>
          <h2>转换设置</h2>
        </div>

        <div class="quality-control">
          <div class="field-top">
            <label for="quality">画质等级</label>
            <output for="quality">{{ form.level }} / 10</output>
          </div>
          <input id="quality" type="range" min="1" max="10" v-model.number="form.level" :disabled="running" />
          <div class="range-legend">
            <span>小体积</span>
            <strong>{{ qualityText }}</strong>
            <span>高画质</span>
          </div>
        </div>

        <div class="option-list">
          <label class="switch-row">
            <input type="checkbox" v-model="form.recursive" :disabled="running" />
            <span class="switch" aria-hidden="true"></span>
            <span><strong>递归扫描</strong><small>包含子文件夹中的 HEIC / HEIF</small></span>
          </label>
          <label class="switch-row">
            <input type="checkbox" v-model="form.overwrite" :disabled="running" />
            <span class="switch" aria-hidden="true"></span>
            <span><strong>覆盖 JPG</strong><small>同名 JPG 存在时重新生成</small></span>
          </label>
          <label class="switch-row caution">
            <input type="checkbox" v-model="form.deleteOriginal" :disabled="running" />
            <span class="switch" aria-hidden="true"></span>
            <span><strong>备份原图</strong><small>成功后移动原 HEIC 到备份目录</small></span>
          </label>
        </div>

        <div class="worker-field">
          <div>
            <label for="workers">并发线程</label>
            <small>默认按 CPU 自动建议，可手动调整</small>
          </div>
          <div class="stepper">
            <button type="button" @click="adjustWorkers(-1)" :disabled="running" title="减少线程">-</button>
            <input id="workers" type="number" min="1" max="16" v-model.number="form.workers" :disabled="running" />
            <button type="button" @click="adjustWorkers(1)" :disabled="running" title="增加线程">+</button>
          </div>
        </div>

        <div class="converter-panel" :class="{ ok: converter.available, warn: !converter.available }">
          <strong>{{ converter.available ? '转换器可用' : '未检测到转换器' }}</strong>
          <span>{{ converter.available ? `${converter.name} ${converter.path || ''}` : converter.message }}</span>
          <pre v-if="!converter.available && converter.help">{{ converter.help }}</pre>
        </div>

        <button class="start-button" @click="start" :disabled="!canStart">{{ running ? '转换中...' : '开始转换' }}</button>
      </aside>

      <section class="main-stack">
        <section class="panel progress-panel">
          <div class="progress-overview">
            <div>
              <p class="kicker">Progress</p>
              <h2>{{ progress.message }}</h2>
              <p>{{ taskState }}，{{ form.input ? `来源：${sourceName}` : '请选择文件或文件夹开始。' }}</p>
            </div>
            <div class="progress-dial" :style="{ '--value': progressPercent + '%' }" aria-label="转换进度">
              <strong>{{ progressPercent }}%</strong>
            </div>
          </div>
          <div class="progress-track"><i :style="{ width: progressPercent + '%' }"></i></div>
          <div class="metric-grid">
            <article v-for="item in metrics" :key="item.label" class="metric" :class="item.tone">
              <span>{{ item.label }}</span>
              <strong>{{ item.value }}</strong>
            </article>
          </div>
        </section>

        <section v-if="result || error" class="panel result-panel" :class="{ failed: error, done: result && !error }">
          <div v-if="result">
            <p class="kicker">Result</p>
            <h3>转换完成</h3>
            <p>总数 {{ result.found }}，成功 {{ result.success }}，跳过 {{ result.skipped }}，失败 {{ result.failed }}，耗时 {{ result.duration }}。</p>
            <p v-if="result.backupDir">原 HEIC 已移动到：{{ result.backupDir }}</p>
          </div>
          <div v-if="error">
            <p class="kicker">Attention</p>
            <h3>需要处理</h3>
            <p>{{ error }}</p>
          </div>
        </section>

        <section class="panel log-panel">
          <div class="log-heading">
            <div>
              <p class="kicker">Activity</p>
              <h2>运行记录</h2>
            </div>
            <span>{{ log.length }} 条</span>
          </div>
          <div class="log-list" aria-live="polite">
            <p v-if="!log.length" class="muted">开始后会显示扫描、转换和错误信息。</p>
            <p v-for="line in log" :key="line">{{ line }}</p>
          </div>
        </section>
      </section>
    </section>
  </main>
</template>
