<template>
  <div v-if="show" class="modal" @click.self="close">
    <div class="cropper-container">
      <h3>裁剪背景图片</h3>
      <canvas ref="canvas" @mousedown="startDrag" @mousemove="drag" @mouseup="endDrag" @touchstart="startDrag" @touchmove="drag" @touchend="endDrag"></canvas>
      <div class="controls">
        <button @click="confirm" class="btn">确认</button>
        <button @click="close" class="btn">取消</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'

const props = defineProps(['show', 'image'])
const emit = defineEmits(['close', 'confirm'])

const canvas = ref(null)
const ctx = ref(null)
const img = ref(null)
const scale = ref(1)
const offsetX = ref(0)
const offsetY = ref(0)
const dragging = ref(false)
const dragStart = ref({ x: 0, y: 0 })

const targetW = 376
const targetH = 960

watch(() => props.show, async (val) => {
  if (val && props.image) {
    await nextTick()
    initCanvas()
  }
})

const initCanvas = () => {
  const c = canvas.value
  if (!c) return
  
  c.width = targetW
  c.height = targetH
  ctx.value = c.getContext('2d')
  
  img.value = new Image()
  img.value.onload = () => {
    const imgW = img.value.width
    const imgH = img.value.height
    scale.value = Math.max(targetW / imgW, targetH / imgH)
    offsetX.value = (targetW - imgW * scale.value) / 2
    offsetY.value = (targetH - imgH * scale.value) / 2
    draw()
  }
  img.value.src = props.image
}

const draw = () => {
  if (!ctx.value || !img.value) return
  ctx.value.clearRect(0, 0, targetW, targetH)
  ctx.value.drawImage(img.value, offsetX.value, offsetY.value, img.value.width * scale.value, img.value.height * scale.value)
}

const startDrag = (e) => {
  dragging.value = true
  const pos = getPos(e)
  dragStart.value = { x: pos.x - offsetX.value, y: pos.y - offsetY.value }
}

const drag = (e) => {
  if (!dragging.value) return
  const pos = getPos(e)
  offsetX.value = pos.x - dragStart.value.x
  offsetY.value = pos.y - dragStart.value.y
  draw()
}

const endDrag = () => {
  dragging.value = false
}

const getPos = (e) => {
  const rect = canvas.value.getBoundingClientRect()
  const x = (e.touches ? e.touches[0].clientX : e.clientX) - rect.left
  const y = (e.touches ? e.touches[0].clientY : e.clientY) - rect.top
  return { x, y }
}

const confirm = () => {
  canvas.value.toBlob((blob) => {
    emit('confirm', blob)
  }, 'image/jpeg', 0.9)
}

const close = () => emit('close')
</script>

<style scoped>
.modal { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.8); display: flex; align-items: center; justify-content: center; z-index: 1000; overflow: auto; }
.cropper-container { background: #1e2028; border-radius: 12px; padding: 20px; max-width: 90vw; max-height: 90vh; overflow: auto; }
h3 { color: #00c882; margin-bottom: 15px; }
canvas { border: 2px solid #00c882; cursor: move; display: block; margin-bottom: 15px; max-width: 100%; height: auto; }
.controls { display: flex; gap: 10px; }
.btn { flex: 1; padding: 12px; border: none; border-radius: 8px; background: #00c882; color: #fff; cursor: pointer; }
</style>
