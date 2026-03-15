package web

const htmlTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>R1 控制面板</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:sans-serif;background:#1a1c24;color:#fff;padding:20px}
.card{background:#1e2028;border-radius:12px;padding:20px;margin-bottom:20px}
h2{margin-bottom:15px;color:#00c882}
.slider{width:100%;height:40px;-webkit-appearance:none;background:#2a2c34;border-radius:20px;outline:none}
.slider::-webkit-slider-thumb{-webkit-appearance:none;width:50px;height:50px;background:#00c882;border-radius:50%;cursor:pointer}
.btn-group{display:flex;gap:10px;margin-top:10px}
.btn{flex:1;padding:12px;border:none;border-radius:8px;background:#2a2c34;color:#fff;cursor:pointer;transition:background 0.3s}
.btn.active{background:#00c882}
.btn:hover{background:#3a3c44}
.color-picker{display:flex;gap:8px;margin-top:10px}
.color-btn{width:40px;height:40px;border-radius:50%;border:2px solid #2a2c34;cursor:pointer;transition:border-color 0.3s}
.color-btn.active{border-color:#00c882}
.status{display:flex;justify-content:space-between;margin-top:10px;color:#888}
.temp-input{display:flex;gap:10px;align-items:center;margin-top:10px}
.temp-input input{width:80px;padding:8px;background:#2a2c34;border:1px solid #3a3c44;border-radius:6px;color:#fff;text-align:center}
.temp-input label{color:#888}
</style>
</head>
<body>
<div class="card">
<h2>系统状态</h2>
<div class="status">
<span>CPU: <b id="cpu">--</b>%</span>
<span>温度: <b id="temp">--</b>°C</span>
<span>内存: <b id="mem">--</b>%</span>
</div>
</div>

<div class="card">
<h2>亮度调节</h2>
<input type="range" class="slider" id="brightness" min="0" max="96000" value="36000">
<div class="status"><span id="bright-val">37%</span></div>
</div>

<div class="card">
<h2>屏幕控制</h2>
<div class="btn-group">
<button class="btn" onclick="screenOff()">关闭屏幕</button>
</div>
</div>

<div class="card">
<h2>硬件配置 (IT8628)</h2>
<button class="btn" onclick="scanHardware()" style="width:100%;margin-bottom:15px">扫描硬件</button>
<div id="hardware-list" style="color:#888;font-size:14px"></div>
<div style="margin-top:15px">
<label style="color:#888">CPU风扇PWM:</label>
<input type="number" id="cpu-pwm" value="3" min="1" max="5" style="width:60px;padding:5px;background:#2a2c34;border:1px solid #3a3c44;border-radius:4px;color:#fff;margin:0 10px">
<label style="color:#888">温度:</label>
<input type="text" id="cpu-sensor" value="it8628:temp1" style="width:150px;padding:5px;background:#2a2c34;border:1px solid #3a3c44;border-radius:4px;color:#fff;margin:0 10px">
<button class="btn" onclick="setMapping('cpu')" style="width:80px">应用</button>
</div>
<div style="margin-top:10px">
<label style="color:#888">机箱风扇PWM:</label>
<input type="number" id="hdd-pwm" value="2" min="1" max="5" style="width:60px;padding:5px;background:#2a2c34;border:1px solid #3a3c44;border-radius:4px;color:#fff;margin:0 10px">
<label style="color:#888">温度:</label>
<input type="text" id="hdd-sensor" value="it8628:temp2" style="width:150px;padding:5px;background:#2a2c34;border:1px solid #3a3c44;border-radius:4px;color:#fff;margin:0 10px">
<button class="btn" onclick="setMapping('hdd')" style="width:80px">应用</button>
</div>
</div>

<div class="card">
<h2>RGB 灯效</h2>
<div class="btn-group">
<button class="btn" onclick="setRGB('rainbow','')">彩虹</button>
<button class="btn" onclick="setRGB('breathing','green')">呼吸</button>
<button class="btn" onclick="setRGB('solid','green')">单色</button>
<button class="btn" onclick="setRGB('off','')">关闭</button>
</div>
<div class="color-picker">
<div class="color-btn" style="background:#ff5050" onclick="setColor('red')"></div>
<div class="color-btn" style="background:#ffa500" onclick="setColor('orange')"></div>
<div class="color-btn" style="background:#ffff00" onclick="setColor('yellow')"></div>
<div class="color-btn" style="background:#00c882" onclick="setColor('green')"></div>
<div class="color-btn" style="background:#00ffff" onclick="setColor('cyan')"></div>
<div class="color-btn" style="background:#5096ff" onclick="setColor('blue')"></div>
<div class="color-btn" style="background:#b450ff" onclick="setColor('purple')"></div>
<div class="color-btn" style="background:#fff" onclick="setColor('white')"></div>
</div>
</div>

<div class="card">
<h2>CPU 风扇</h2>
<div class="btn-group" id="cpu-fan-group">
<button class="btn active" data-mode="auto" onclick="setFan('cpu','auto',this)">自动</button>
<button class="btn" data-mode="low" onclick="setFan('cpu','low',this)">低速</button>
<button class="btn" data-mode="medium" onclick="setFan('cpu','medium',this)">中速</button>
<button class="btn" data-mode="high" onclick="setFan('cpu','high',this)">高速</button>
</div>
<div class="temp-input">
<label>启动温度</label>
<input type="number" id="cpu-min" value="35" min="20" max="60">
<label>°C</label>
<label>全速温度</label>
<input type="number" id="cpu-max" value="75" min="60" max="100">
<label>°C</label>
<button class="btn" onclick="setCurve('cpu')" style="width:80px">应用</button>
</div>
</div>

<div class="card">
<h2>HDD 风扇</h2>
<div class="btn-group" id="hdd-fan-group">
<button class="btn active" data-mode="auto" onclick="setFan('hdd','auto',this)">自动</button>
<button class="btn" data-mode="low" onclick="setFan('hdd','low',this)">低速</button>
<button class="btn" data-mode="medium" onclick="setFan('hdd','medium',this)">中速</button>
<button class="btn" data-mode="high" onclick="setFan('hdd','high',this)">高速</button>
</div>
<div class="temp-input">
<label>启动温度</label>
<input type="number" id="hdd-min" value="30" min="20" max="40">
<label>°C</label>
<label>全速温度</label>
<input type="number" id="hdd-max" value="45" min="40" max="60">
<label>°C</label>
<button class="btn" onclick="setCurve('hdd')" style="width:80px">应用</button>
</div>
</div>

<script>
let currentMode='solid',currentColor='green';
document.getElementById('brightness').oninput=function(){
fetch('/api/brightness',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({value:parseInt(this.value)})});
document.getElementById('bright-val').textContent=Math.round(this.value/960)+'%';
};
function screenOff(){
if(confirm('确定关闭屏幕？')){
fetch('/api/screen/off',{method:'POST'});
}}
function scanHardware(){
fetch('/api/hwmon/scan').then(r=>r.json()).then(d=>{
let html='<b>风扇列表 (PWM编号):</b><br>';
d.fans.forEach(f=>{
const pwmNum=f.Label.match(/fan(\d+)/)?.[1]||'?';
html+='pwm'+pwmNum+': ['+f.Chip+'] '+f.Label+' ('+f.RPM+' RPM)<br>';
});
html+='<br><b>温度传感器 (格式: 芯片名:标签):</b><br>';
d.temps.forEach(t=>html+=t.Chip+':'+t.Label+' = '+t.Temp.toFixed(1)+'°C<br>');
document.getElementById('hardware-list').innerHTML=html;
});}
function setMapping(type){
const pwmIndex=parseInt(document.getElementById(type+'-pwm').value);
const tempSensor=document.getElementById(type+'-sensor').value;
fetch('/api/fan/mapping',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({type,pwm_index:pwmIndex,temp_sensor:tempSensor})})
.then(()=>alert('映射已更新'));}
function setRGB(mode,color){currentMode=mode;if(color)currentColor=color;
fetch('/api/rgb',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode,color:currentColor})});}
function setColor(color){currentColor=color;
fetch('/api/rgb',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:currentMode,color})});}
function setFan(type,mode,btn){
const group=btn.parentElement;
group.querySelectorAll('.btn').forEach(b=>b.classList.remove('active'));
btn.classList.add('active');
fetch('/api/fan',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({type,mode})});}
function setCurve(type){
const minTemp=parseInt(document.getElementById(type+'-min').value);
const maxTemp=parseInt(document.getElementById(type+'-max').value);
fetch('/api/fan/curve',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({type,min_temp:minTemp,max_temp:maxTemp})})
.then(()=>alert('温度曲线已更新'));}
setInterval(()=>{
fetch('/api/status').then(r=>r.json()).then(d=>{
document.getElementById('cpu').textContent=d.cpu.toFixed(1);
document.getElementById('temp').textContent=d.cpu_temp.toFixed(1);
document.getElementById('mem').textContent=d.memory.toFixed(1);
});
},2000);
</script>
</body>
</html>`
