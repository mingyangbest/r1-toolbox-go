<template>
  <div class="app">
    <div class="status-bar">
      <div>CPU: {{ status.cpu.toFixed(2) }}%</div>
      <div>CPU温度: {{ status.cpu_temp.toFixed(1) }}°C</div>
      <div>机箱温度: {{ status.hdd_temp.toFixed(1) }}°C</div>
      <div>内存: {{ status.memory.toFixed(2) }}%</div>
    </div>

    <div class="card">
      <h2>屏幕外观</h2>
      <div style="margin-bottom:15px">
        <label style="color:#888">背景图片</label>
        <input type="file" @change="uploadBackground" accept="image/*" style="display:block;margin-top:8px;color:#888">
      </div>
      <div>
        <label style="color:#888">文字颜色</label>
        <div class="color-picker" style="margin-top:8px">
          <div v-for="c in textColors" :key="c.hex" @click="setTextColor(c.hex)" :class="{active:textColor===c.hex}" :style="{background:c.hex}" class="color-btn"></div>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>屏幕亮度</h2>
      <input type="range" v-model.number="brightness" @input="setBrightness" min="0" max="96000" class="slider">
      <div class="status">{{ Math.round(brightness/960) }}%</div>
      <div class="btn-group">
        <button @click="screenOff" class="btn">关闭屏幕</button>
        <button @click="screenOn" class="btn">开启屏幕</button>
      </div>
      <div class="temp-input" style="margin-top:15px">
        <label>自动关屏</label>
        <input type="number" v-model.number="screenTimeout" min="0" max="3600" style="width:80px">
        <label>秒 (0=禁用)</label>
        <button @click="setScreenTimeout" class="btn-sm">应用</button>
      </div>
    </div>

    <div class="card">
      <h2>硬件扫描 (IT8628)</h2>
      <button @click="scanHardware" class="btn">扫描硬件</button>
      <div v-if="hardware.fans.length" class="hw-list">
        <div><b>风扇:</b></div>
        <div v-for="f in hardware.fans" :key="f.Label">pwm{{ getPWMNum(f.Label) }}: [{{ f.Chip }}] {{ f.Label }} ({{ f.RPM }} RPM)</div>
        <div style="margin-top:10px"><b>温度:</b></div>
        <div v-for="t in hardware.temps" :key="t.Label">{{ t.Chip }}:{{ t.Label }} = {{ t.Temp.toFixed(1) }}°C</div>
      </div>
      
      <div class="mapping">
        <div class="map-item">
          <div class="map-title">CPU 风扇</div>
          <label>PWM:</label>
          <input type="number" v-model.number="cpuPWM" min="1" max="5">
          <label>温度:</label>
          <input type="text" v-model="cpuSensor" placeholder="芯片:标签">
          <button @click="setMapping('cpu')" class="btn-sm">应用</button>
        </div>
        <div class="map-item">
          <div class="map-title">机箱风扇</div>
          <label>PWM:</label>
          <input type="number" v-model.number="hddPWM" min="1" max="5">
          <label>温度:</label>
          <input type="text" v-model="hddSensor" placeholder="芯片:标签">
          <button @click="setMapping('hdd')" class="btn-sm">应用</button>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>RGB 灯效</h2>
      <div class="btn-group">
        <button v-for="m in rgbModes" :key="m" @click="setRGB(m)" :class="{btn:true,active:rgbMode===m}">{{ m }}</button>
      </div>
      <div class="color-picker">
        <div v-for="c in colors" :key="c" @click="setColor(c)" :class="{active:rgbColor===c}" :style="{background:colorMap[c]}" class="color-btn"></div>
      </div>
    </div>

    <div class="card">
      <h2>CPU 风扇</h2>
      <div class="btn-group">
        <button v-for="m in fanModes" :key="m" @click="setFan('cpu',m)" :class="{btn:true,active:cpuFanMode===m}">{{ m }}</button>
      </div>
      <div class="temp-input">
        <label>启动温度</label>
        <input type="number" v-model.number="cpuMinTemp" min="20" max="60">
        <label>°C</label>
        <label>全速温度</label>
        <input type="number" v-model.number="cpuMaxTemp" min="60" max="100">
        <label>°C</label>
        <button @click="setCurve('cpu')" class="btn">应用</button>
      </div>
    </div>

    <div class="card">
      <h2>机箱风扇</h2>
      <div class="btn-group">
        <button v-for="m in fanModes" :key="m" @click="setFan('hdd',m)" :class="{btn:true,active:hddFanMode===m}">{{ m }}</button>
      </div>
      <div class="temp-input">
        <label>启动温度</label>
        <input type="number" v-model.number="hddMinTemp" min="20" max="40">
        <label>°C</label>
        <label>全速温度</label>
        <input type="number" v-model.number="hddMaxTemp" min="40" max="60">
        <label>°C</label>
        <button @click="setCurve('hdd')" class="btn">应用</button>
      </div>
    </div>
  </div>
  <ImageCropper :show="showCropper" :image="cropperImage" @close="handleCropClose" @confirm="handleCropConfirm" />
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ImageCropper from './ImageCropper.vue'

const status = ref({ cpu: 0, cpu_temp: 0, hdd_temp: 0, memory: 0 })
const brightness = ref(36000)
const screenTimeout = ref(300)
const hardware = ref({ fans: [], temps: [] })
const cpuPWM = ref(3)
const hddPWM = ref(2)
const cpuSensor = ref('coretemp:Package id 0')
const hddSensor = ref('it8628:temp2')

const showCropper = ref(false)
const cropperImage = ref('')

const rgbMode = ref('solid')
const rgbColor = ref('green')
const rgbModes = ['off', 'rainbow', 'breathing', 'solid']
const colors = ['red', 'orange', 'yellow', 'green', 'cyan', 'blue', 'purple', 'white']
const colorMap = {
  red: '#ff0000', orange: '#ff8800', yellow: '#ffff00', green: '#00ff00',
  cyan: '#00ffff', blue: '#0000ff', purple: '#ff00ff', white: '#ffffff'
}

const cpuFanMode = ref('auto')
const hddFanMode = ref('auto')
const fanModes = ['auto', 'low', 'medium', 'high']
const cpuMinTemp = ref(35)
const cpuMaxTemp = ref(75)
const hddMinTemp = ref(30)
const hddMaxTemp = ref(45)

const textColor = ref('#ffffff')
const textColors = [
  {hex:'#ffffff'},{hex:'#00ff00'},{hex:'#00ffff'},{hex:'#ffff00'},
  {hex:'#ff8800'},{hex:'#ff0000'},{hex:'#ff00ff'},{hex:'#888888'}
]

const api = (url, data) => fetch(url, {
  method: data ? 'POST' : 'GET',
  headers: data ? { 'Content-Type': 'application/json' } : {},
  body: data ? JSON.stringify(data) : undefined
}).then(r => r.ok ? r.json() : Promise.reject(r))

const setBrightness = () => api('/api/brightness', { value: +brightness.value })
const setScreenTimeout = () => api('/api/screen/timeout', { timeout: +screenTimeout.value }).then(() => alert('自动关屏已更新'))
const screenOff = () => confirm('确定关闭屏幕？') && api('/api/screen/off', {})
const screenOn = () => api('/api/screen/on', { value: 36000 })
const scanHardware = () => api('/api/hwmon/scan').then(d => hardware.value = d)
const setMapping = (type) => {
  const pwm = type === 'cpu' ? cpuPWM.value : hddPWM.value
  const sensor = type === 'cpu' ? cpuSensor.value : hddSensor.value
  api('/api/fan/mapping', { type, pwm_index: pwm, temp_sensor: sensor }).then(() => alert('映射已更新'))
}
const setRGB = (mode) => {
  rgbMode.value = mode
  api('/api/rgb', { mode, color: rgbColor.value })
}
const setColor = (color) => {
  rgbColor.value = color
  api('/api/rgb', { mode: rgbMode.value, color })
}
const setFan = (type, mode) => {
  if (type === 'cpu') cpuFanMode.value = mode
  else hddFanMode.value = mode
  api('/api/fan', { type, mode })
}
const setCurve = (type) => {
  const min = type === 'cpu' ? cpuMinTemp.value : hddMinTemp.value
  const max = type === 'cpu' ? cpuMaxTemp.value : hddMaxTemp.value
  api('/api/fan/curve', { type, min_temp: min, max_temp: max }).then(() => alert('温度曲线已更新'))
}
const getPWMNum = (label) => label.match(/fan(\d+)/)?.[1] || '?'

const uploadBackground = (e) => {
  const file = e.target.files[0]
  if (!file) return
  
  const reader = new FileReader()
  reader.onload = (ev) => {
    cropperImage.value = ev.target.result
    showCropper.value = true
  }
  reader.readAsDataURL(file)
}

const handleCropConfirm = (blob) => {
  const formData = new FormData()
  formData.append('image', blob, 'background.jpg')
  fetch('/api/background', { method: 'POST', body: formData })
    .then(r => r.json())
    .then(() => {
      alert('背景已更新')
      showCropper.value = false
    })
}

const handleCropClose = () => {
  showCropper.value = false
}

const setTextColor = (color) => {
  textColor.value = color
  api('/api/textcolor', { color }).then(() => alert('文字颜色已更新'))
}

onMounted(() => {
  setInterval(() => api('/api/status').then(d => status.value = d), 2000)
})
</script>

<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: sans-serif; background: #1a1c24; color: #fff; }
.app { padding: 20px; max-width: 800px; margin: 0 auto; }
.status-bar { display: flex; gap: 20px; margin-bottom: 20px; padding: 15px; background: #1e2028; border-radius: 12px; }
.card { background: #1e2028; border-radius: 12px; padding: 20px; margin-bottom: 20px; }
h2 { margin-bottom: 15px; color: #00c882; font-size: 18px; }
.slider { width: 100%; height: 40px; -webkit-appearance: none; background: #2a2c34; border-radius: 20px; outline: none; }
.slider::-webkit-slider-thumb { -webkit-appearance: none; width: 50px; height: 50px; background: #00c882; border-radius: 50%; cursor: pointer; }
.btn-group { display: flex; gap: 10px; margin-top: 10px; }
.btn { flex: 1; padding: 12px; border: none; border-radius: 8px; background: #2a2c34; color: #fff; cursor: pointer; transition: all 0.3s; }
.btn.active { background: #00c882; }
.btn:hover { background: #3a3c44; }
.btn-sm { padding: 6px 12px; border: none; border-radius: 6px; background: #00c882; color: #fff; cursor: pointer; }
.color-picker { display: flex; gap: 8px; margin-top: 10px; }
.color-btn { width: 40px; height: 40px; border-radius: 50%; border: 2px solid #2a2c34; cursor: pointer; transition: border-color 0.3s; }
.color-btn.active { border-color: #00c882; border-width: 3px; }
.status { margin-top: 10px; color: #888; }
.temp-input { display: flex; gap: 10px; align-items: center; margin-top: 10px; flex-wrap: wrap; }
.temp-input input { width: 80px; padding: 8px; background: #2a2c34; border: 1px solid #3a3c44; border-radius: 6px; color: #fff; text-align: center; }
.temp-input label { color: #888; }
.hw-list { margin-top: 15px; padding: 10px; background: #2a2c34; border-radius: 6px; font-size: 13px; line-height: 1.6; color: #aaa; }
.mapping { margin-top: 15px; }
.map-item { padding: 12px; background: #2a2c34; border-radius: 6px; margin-bottom: 10px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.map-title { width: 100%; color: #00c882; margin-bottom: 5px; font-weight: bold; }
.map-item input[type="number"] { width: 60px; padding: 6px; background: #1a1c24; border: 1px solid #3a3c44; border-radius: 4px; color: #fff; }
.map-item input[type="text"] { width: 180px; padding: 6px; background: #1a1c24; border: 1px solid #3a3c44; border-radius: 4px; color: #fff; }
.map-item label { color: #888; font-size: 13px; }
</style>
