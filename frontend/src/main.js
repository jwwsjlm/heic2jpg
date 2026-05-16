import { createApp } from 'vue'
import './style.css'

const backend = window.go?.main?.App
const runtime = window.runtime

const fallback = {
  async SelectFile(){ alert('请在 Wails 桌面应用中使用文件选择。'); return '' },
  async SelectFolder(){ alert('请在 Wails 桌面应用中使用文件夹选择。'); return '' },
  async DefaultWorkers(){ return Math.min(4, navigator.hardwareConcurrency || 4) },
  async RuntimeInfo(){ return { os: 'browser-preview', arch: '' } },
  async StartConversion(){ throw new Error('浏览器预览不能直接转换，请运行 Wails 应用。') }
}
const api = backend || fallback

createApp({
  data(){
    return {
      form: { input: '', recursive: true, overwrite: false, deleteOriginal: false, level: 10, workers: 0 },
      running: false,
      progress: { found: 0, done: 0, success: 0, skipped: 0, failed: 0, percent: 0, message: '等待选择文件或文件夹' },
      result: null,
      error: '',
      info: {},
      log: []
    }
  },
  computed:{
    qualityText(){
      const map = {1:'文件最小',2:'较小文件',3:'轻量压缩',4:'均衡偏小',5:'均衡',6:'清晰',7:'高清',8:'高质量',9:'极高质量',10:'最高质量 / 保留元数据'}
      return map[this.form.level] || '最高质量'
    },
    canStart(){ return this.form.input && !this.running }
  },
  async mounted(){
    this.form.workers = await api.DefaultWorkers()
    this.info = await api.RuntimeInfo().catch(()=>({}))
    if(runtime?.EventsOn){
      runtime.EventsOn('conversion:progress', p => { this.progress = p; this.pushLog(p.message) })
      runtime.EventsOn('conversion:error', msg => { this.error = msg; this.running = false; this.pushLog('错误：' + msg) })
      runtime.EventsOn('conversion:done', res => { this.result = res; this.running = false; this.pushLog('转换完成') })
    }
  },
  methods:{
    async pickFile(){ const p = await api.SelectFile(); if(p) this.form.input = p },
    async pickFolder(){ const p = await api.SelectFolder(); if(p) this.form.input = p },
    pushLog(text){ if(!text) return; const line = new Date().toLocaleTimeString() + '  ' + text; if(this.log[0] !== line) this.log.unshift(line); this.log = this.log.slice(0, 80) },
    async start(){
      if(!this.canStart) return
      this.running = true; this.error = ''; this.result = null; this.log = []
      this.progress = { found: 0, done: 0, success: 0, skipped: 0, failed: 0, percent: 0, message: '准备转换' }
      try{
        const res = await api.StartConversion({...this.form, level: Number(this.form.level), workers: Number(this.form.workers || 0)})
        this.result = res; this.progress.percent = 100; this.pushLog('转换完成')
      }catch(e){
        this.error = e?.message || String(e)
        this.pushLog('错误：' + this.error)
      }finally{ this.running = false }
    }
  },
  template: `
  <main class="shell">
    <section class="hero card">
      <div>
        <p class="eyebrow">HEIC2JPG Desktop</p>
        <h1>把 iPhone HEIC 批量转成 JPG</h1>
        <p class="sub">选择文件或文件夹，设置画质，点击开始。默认保留原文件；如果要清理原图，会先移动到备份目录。</p>
      </div>
      <div class="hero-badge"><span>{{ info.os || 'desktop' }}</span><b>{{ info.arch }}</b></div>
    </section>

    <section class="grid">
      <div class="card controls">
        <h2>1. 选择来源</h2>
        <div class="path-box" :class="{empty: !form.input}">{{ form.input || '还没有选择文件或文件夹' }}</div>
        <div class="button-row">
          <button @click="pickFile" :disabled="running">选择 HEIC 文件</button>
          <button class="secondary" @click="pickFolder" :disabled="running">选择文件夹</button>
        </div>

        <h2>2. 转换设置</h2>
        <label>画质等级：{{ form.level }} / 10 <small>{{ qualityText }}</small></label>
        <input type="range" min="1" max="10" v-model.number="form.level" :disabled="running" />
        <div class="setting-row">
          <label class="toggle"><input type="checkbox" v-model="form.recursive" :disabled="running" /> 递归扫描子文件夹</label>
          <label class="toggle"><input type="checkbox" v-model="form.overwrite" :disabled="running" /> 覆盖已存在 JPG</label>
          <label class="toggle danger"><input type="checkbox" v-model="form.deleteOriginal" :disabled="running" /> 成功后移动原 HEIC 到备份目录</label>
        </div>
        <label>并发线程</label>
        <input class="number" type="number" min="1" max="16" v-model.number="form.workers" :disabled="running" />

        <button class="start" @click="start" :disabled="!canStart">{{ running ? '转换中...' : '开始转换' }}</button>
      </div>

      <div class="card progress-card">
        <div class="progress-head">
          <div><p class="eyebrow">Progress</p><h2>{{ progress.message }}</h2></div>
          <strong>{{ progress.percent }}%</strong>
        </div>
        <div class="bar"><i :style="{width: progress.percent + '%'}"></i></div>
        <div class="stats">
          <div><span>发现</span><b>{{ progress.found }}</b></div>
          <div><span>已处理</span><b>{{ progress.done }}</b></div>
          <div><span>成功</span><b>{{ progress.success }}</b></div>
          <div><span>跳过</span><b>{{ progress.skipped }}</b></div>
          <div><span>失败</span><b>{{ progress.failed }}</b></div>
        </div>

        <div v-if="result" class="result success">
          <h3>转换完成</h3>
          <p>总数 {{ result.found }}，成功 {{ result.success }}，跳过 {{ result.skipped }}，失败 {{ result.failed }}，耗时 {{ result.duration }}。</p>
          <p v-if="result.backupDir">原 HEIC 已移动到：{{ result.backupDir }}</p>
        </div>
        <div v-if="error" class="result error"><h3>需要处理</h3><p>{{ error }}</p></div>

        <div class="log">
          <h3>运行记录</h3>
          <p v-if="!log.length" class="muted">开始后会显示扫描和转换进度。</p>
          <p v-for="line in log" :key="line">{{ line }}</p>
        </div>
      </div>
    </section>
  </main>`
}).mount('#app')
