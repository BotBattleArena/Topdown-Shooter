package main

var viewerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Top-Down Shooter</title>
<style>
@import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&family=Bebas+Neue&display=swap');
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0a0a0f;color:#e0e0e0;font-family:'JetBrains Mono',monospace;overflow:hidden;height:100vh;width:100vw}
#wrap{display:flex;height:100vh;width:100vw}
#game{flex:1;position:relative;overflow:hidden;background:#0d0d14}
canvas{display:block;width:100%;height:100%}
#side{width:280px;background:linear-gradient(180deg,#111118 0%,#0a0a10 100%);border-left:1px solid #1a1a2e;display:flex;flex-direction:column;padding:14px;gap:10px;overflow-y:auto}
#side::-webkit-scrollbar{width:4px}
#side::-webkit-scrollbar-thumb{background:#2a2a3e;border-radius:2px}
.panel{background:rgba(255,255,255,0.03);border:1px solid rgba(255,255,255,0.06);border-radius:8px;padding:12px}
.panel h3{font-family:'Bebas Neue',sans-serif;font-size:18px;letter-spacing:2px;color:#8a8aff;margin-bottom:8px;text-transform:uppercase}
#timer{font-family:'Bebas Neue',sans-serif;font-size:42px;text-align:center;letter-spacing:4px;background:linear-gradient(135deg,#8a8aff,#ff6b8a);-webkit-background-clip:text;-webkit-text-fill-color:transparent;line-height:1}
#timer-sub{text-align:center;font-size:10px;color:#555;margin-top:2px}
.lb-row{display:flex;align-items:center;gap:8px;padding:4px 0;border-bottom:1px solid rgba(255,255,255,0.04)}
.lb-row:last-child{border:none}
.lb-rank{width:20px;font-size:11px;color:#555;text-align:right}
.lb-dot{width:10px;height:10px;border-radius:50%;flex-shrink:0}
.lb-name{flex:1;font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.lb-kills{font-size:12px;font-weight:700;color:#8a8aff}
.kill-row{font-size:10px;padding:2px 0;color:#777;display:flex;gap:4px;align-items:center}
.kill-row .k{color:#ff6b8a}
.kill-row .v{color:#8a8aff}
#overlay{position:absolute;top:0;left:0;right:0;bottom:0;display:none;align-items:center;justify-content:center;background:rgba(0,0,0,0.7);z-index:10;flex-direction:column;gap:16px}
#overlay.show{display:flex}
#overlay h1{font-family:'Bebas Neue',sans-serif;font-size:64px;letter-spacing:6px;background:linear-gradient(135deg,#ffd700,#ff6b8a);-webkit-background-clip:text;-webkit-text-fill-color:transparent}
#overlay p{font-size:14px;color:#aaa}
#minimap{width:100%;aspect-ratio:1;background:#0a0a12;border-radius:6px;border:1px solid rgba(255,255,255,0.08);position:relative;overflow:hidden}
#stats{font-size:10px;color:#555;text-align:center}
#fps{position:absolute;top:8px;left:8px;font-size:10px;color:#444;z-index:5}
</style>
</head>
<body>
<div id="wrap">
<div id="game">
<canvas id="cv"></canvas>
<div id="fps"></div>
<div id="overlay"><h1>WAITING</h1><p>Connecting to game...</p></div>
</div>
<div id="side">
<div class="panel" style="text-align:center">
<div id="timer">0:00</div>
<div id="timer-sub">TIME REMAINING</div>
</div>
<div class="panel">
<h3>Leaderboard</h3>
<div id="lb"></div>
</div>
<div class="panel">
<h3>Minimap</h3>
<div id="minimap"><canvas id="mm"></canvas></div>
</div>
<div class="panel">
<h3>Kill Feed</h3>
<div id="kf"></div>
</div>
<div id="stats"></div>
</div>
</div>
<script>
(function(){
const cv=document.getElementById('cv');
const ctx=cv.getContext('2d',{alpha:false});
const mm=document.getElementById('mm');
const mctx=mm.getContext('2d');
const lbEl=document.getElementById('lb');
const kfEl=document.getElementById('kf');
const timerEl=document.getElementById('timer');
const statsEl=document.getElementById('stats');
const fpsEl=document.getElementById('fps');
const overlay=document.getElementById('overlay');
overlay.classList.add('show');

var state=null,prevState=null,connected=false;
var cam={x:1000,y:1000,zoom:1},targetZoom=0.45;
var autoFollow=true,dragStart=null,dragging=false;
var interpT=0,stateTime=0;
var colorMap={};
var prevPosMap={};
var uiDirty=true,lastUI=0;
var frameCount=0,fpsTime=performance.now(),curFPS=0;

function resize(){
  var r=devicePixelRatio||1;
  cv.width=cv.clientWidth*r;
  cv.height=cv.clientHeight*r;
  ctx.setTransform(r,0,0,r,0,0);
  var mc=document.getElementById('minimap');
  mm.width=mc.clientWidth;
  mm.height=mc.clientHeight;
}
window.addEventListener('resize',resize);
resize();

cv.addEventListener('wheel',function(e){
  e.preventDefault();
  targetZoom*=e.deltaY>0?0.9:1.1;
  if(targetZoom<0.1)targetZoom=0.1;
  if(targetZoom>2.5)targetZoom=2.5;
  autoFollow=false;
},{passive:false});

cv.addEventListener('mousedown',function(e){
  dragStart={x:e.clientX,y:e.clientY,cx:cam.x,cy:cam.y};
  dragging=false;
});
cv.addEventListener('mousemove',function(e){
  if(!dragStart)return;
  var dx=e.clientX-dragStart.x,dy=e.clientY-dragStart.y;
  if(!dragging&&Math.abs(dx)+Math.abs(dy)>5)dragging=true;
  if(dragging){
    cam.x=dragStart.cx-dx/cam.zoom;
    cam.y=dragStart.cy-dy/cam.zoom;
    autoFollow=false;
  }
});
cv.addEventListener('mouseup',function(){dragStart=null});
cv.addEventListener('dblclick',function(){autoFollow=true;targetZoom=0.45});

var es=new EventSource('/events');
es.onmessage=function(e){
  prevState=state;
  state=JSON.parse(e.data);
  stateTime=performance.now();
  interpT=0;
  uiDirty=true;

  colorMap={};
  for(var i=0;i<state.players.length;i++){
    var p=state.players[i];
    colorMap[p.id]=p.color;
  }

  prevPosMap={};
  if(prevState){
    for(var j=0;j<prevState.players.length;j++){
      var pp=prevState.players[j];
      prevPosMap[pp.id]=pp;
    }
  }

  if(!connected){connected=true;overlay.classList.remove('show')}
};
es.onerror=function(){
  if(!connected){
    overlay.querySelector('h1').textContent='DISCONNECTED';
    overlay.querySelector('p').textContent='Reconnecting...';
    overlay.classList.add('show');
  }
};

var PI2=Math.PI*2;

function render(now){
  requestAnimationFrame(render);
  if(!state)return;

  frameCount++;
  if(now-fpsTime>=1000){
    curFPS=frameCount;
    frameCount=0;
    fpsTime=now;
    fpsEl.textContent=curFPS+' fps';
  }

  var dt=(now-stateTime)/1000;
  interpT=dt*30;
  if(interpT>1)interpT=1;

  var cw=cv.clientWidth,ch=cv.clientHeight;

  cam.zoom+=(targetZoom-cam.zoom)*0.08;

  if(autoFollow&&state.players.length>0){
    var cx=0,cy=0,n=0;
    for(var i=0;i<state.players.length;i++){
      var p=state.players[i];
      if(p.alive){cx+=p.x;cy+=p.y;n++}
    }
    if(n>0){
      cam.x+=(cx/n-cam.x)*0.05;
      cam.y+=(cy/n-cam.y)*0.05;
    }
  }

  ctx.fillStyle='#0d0d14';
  ctx.fillRect(0,0,cw,ch);

  ctx.save();
  ctx.translate(cw*0.5,ch*0.5);
  ctx.scale(cam.zoom,cam.zoom);
  ctx.translate(-cam.x,-cam.y);

  var mw=state.map_w,mh=state.map_h;

  ctx.strokeStyle='rgba(255,255,255,0.03)';
  ctx.lineWidth=1;
  ctx.beginPath();
  for(var gx=0;gx<=mw;gx+=100){ctx.moveTo(gx,0);ctx.lineTo(gx,mh)}
  for(var gy=0;gy<=mh;gy+=100){ctx.moveTo(0,gy);ctx.lineTo(mw,gy)}
  ctx.stroke();

  ctx.strokeStyle='#8a8aff';
  ctx.lineWidth=3;
  ctx.strokeRect(0,0,mw,mh);

  var bulls=state.bullets;
  if(bulls&&bulls.length>0){
    ctx.globalAlpha=0.3;
    ctx.lineWidth=1;
    for(var bi=0;bi<bulls.length;bi++){
      var b=bulls[bi];
      ctx.strokeStyle=colorMap[b.owner]||'#fff';
      ctx.beginPath();
      ctx.moveTo(b.x,b.y);
      ctx.lineTo(b.x-b.dx*14,b.y-b.dy*14);
      ctx.stroke();
    }

    ctx.globalAlpha=0.95;
    for(var bi2=0;bi2<bulls.length;bi2++){
      var b2=bulls[bi2];
      ctx.fillStyle=colorMap[b2.owner]||'#fff';
      ctx.beginPath();
      ctx.arc(b2.x,b2.y,4,0,PI2);
      ctx.fill();
    }
    ctx.globalAlpha=1;
  }

  var pls=state.players;
  ctx.font='bold 9px "JetBrains Mono",monospace';
  ctx.textAlign='center';

  for(var pi=0;pi<pls.length;pi++){
    var pl=pls[pi];
    if(!pl.alive)continue;

    var px=pl.x,py=pl.y;
    var prev=prevPosMap[pl.id];
    if(prev){
      px=prev.x+(pl.x-prev.x)*interpT;
      py=prev.y+(pl.y-prev.y)*interpT;
    }

    ctx.fillStyle=pl.color;
    ctx.globalAlpha=1;
    ctx.beginPath();
    ctx.arc(px,py,15,0,PI2);
    ctx.fill();

    ctx.strokeStyle='rgba(255,255,255,0.25)';
    ctx.lineWidth=1.5;
    ctx.stroke();

    var ax=pl.aim_x||0,ay=pl.aim_y||1;
    ctx.strokeStyle=pl.color;
    ctx.lineWidth=2.5;
    ctx.globalAlpha=0.65;
    ctx.beginPath();
    ctx.moveTo(px+ax*15,py+ay*15);
    ctx.lineTo(px+ax*40,py+ay*40);
    ctx.stroke();

    if(pl.hp<100){
      ctx.globalAlpha=0.8;
      var bx=px-15,by=py-23;
      ctx.fillStyle='#1a1a2e';
      ctx.fillRect(bx,by,30,4);
      var pct=pl.hp*0.01;
      if(pct<0)pct=0;
      ctx.fillStyle=pct>0.5?'#2ecc71':pct>0.25?'#f39c12':'#e74c3c';
      ctx.fillRect(bx,by,30*pct,4);
    }

    ctx.globalAlpha=0.55;
    ctx.fillStyle='#fff';
    ctx.fillText(pl.id,px,py+29);
  }

  for(var di=0;di<pls.length;di++){
    var dp=pls[di];
    if(dp.alive)continue;
    ctx.globalAlpha=0.15;
    ctx.fillStyle=dp.color;
    ctx.beginPath();
    ctx.arc(dp.x,dp.y,15,0,PI2);
    ctx.fill();
    ctx.fillStyle='#fff';
    ctx.globalAlpha=0.1;
    ctx.fillText(dp.id,dp.x,dp.y+29);
  }

  ctx.globalAlpha=1;
  ctx.restore();

  if(uiDirty&&now-lastUI>200){
    updateUI();
    lastUI=now;
    uiDirty=false;
  }

  if(frameCount%3===0)drawMinimap();
}

function updateUI(){
  var left=Math.max(0,((state.max_tick-state.tick)/60)|0);
  var m=(left/60)|0,s=left%60;
  timerEl.textContent=m+':'+(s<10?'0':'')+s;

  var sorted=state.players.slice().sort(function(a,b){return b.kills-a.kills});
  var parts=[];
  for(var i=0;i<sorted.length;i++){
    var p=sorted[i];
    parts.push('<div class="lb-row"><span class="lb-rank">');
    parts.push(i+1);
    parts.push('</span><span class="lb-dot" style="background:');
    parts.push(p.color);
    parts.push('"></span><span class="lb-name">');
    parts.push(esc(p.id));
    parts.push('</span><span class="lb-kills">');
    parts.push(p.kills);
    parts.push('</span></div>');
  }
  lbEl.innerHTML=parts.join('');

  var kills=state.kills;
  var kparts=[];
  if(kills){
    var start=kills.length-6;
    if(start<0)start=0;
    for(var j=kills.length-1;j>=start;j--){
      var k=kills[j];
      kparts.push('<div class="kill-row"><span class="k">');
      kparts.push(esc(k.killer));
      kparts.push('</span><span>\u2192</span><span class="v">');
      kparts.push(esc(k.victim));
      kparts.push('</span></div>');
    }
  }
  kfEl.innerHTML=kparts.join('');

  statsEl.textContent=state.players.length+' players | '+(state.bullets?state.bullets.length:0)+' bullets | '+curFPS+' fps';

  if(state.over&&!overlay.classList.contains('show')){
    overlay.querySelector('h1').textContent='WINNER: '+state.winner;
    var w=null;
    for(var wi=0;wi<state.players.length;wi++){
      if(state.players[wi].id===state.winner){w=state.players[wi];break}
    }
    overlay.querySelector('p').textContent=w?(w.kills+' kills / '+w.deaths+' deaths'):'';
    overlay.classList.add('show');
  }
}

function drawMinimap(){
  var w=mm.width,h=mm.height;
  if(w===0||h===0)return;
  mctx.fillStyle='#0a0a12';
  mctx.fillRect(0,0,w,h);
  var sx=w/state.map_w,sy=h/state.map_h;
  var pls=state.players;
  for(var i=0;i<pls.length;i++){
    var p=pls[i];
    if(!p.alive)continue;
    mctx.fillStyle=p.color;
    mctx.beginPath();
    mctx.arc(p.x*sx,p.y*sy,3,0,PI2);
    mctx.fill();
  }
  mctx.strokeStyle='rgba(138,138,255,0.3)';
  mctx.lineWidth=1;
  var vw=cv.clientWidth/cam.zoom,vh=cv.clientHeight/cam.zoom;
  mctx.strokeRect((cam.x-vw*0.5)*sx,(cam.y-vh*0.5)*sy,vw*sx,vh*sy);
}

function esc(s){
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

requestAnimationFrame(render);
})();
</script>
</body>
</html>`
