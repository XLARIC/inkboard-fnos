(function () {
 'use strict';
 var book=document.getElementById('book'),pages=[],index=0,timer=null,refreshTimer=null,request=null,resizeTimer=null,wakeLock=null;
 var basic=document.body.getAttribute('data-basic')==='true',history=document.body.getAttribute('data-history')==='true';
 var prefs={mode:basic?'ink':'mobile',font:'normal',rotate:false,interval:60,wake:false};
 var selectedDate=query('date'),selectedHour=query('hour'),pending=false,callbacks=[],noticeTimer=null,gesture=null,suppressClickUntil=0;
 function query(name){var m=new RegExp('(?:[?&])'+name+'=([^&]*)').exec(location.search);return m?decodeURIComponent(m[1]):'';}
 try{var saved=JSON.parse(localStorage.getItem('inkboard.display.v1')||'{}');for(var k in prefs){if(saved[k]!==undefined){prefs[k]=saved[k];}}}catch(e){}
 var mode=query('mode');if(mode==='mobile'||mode==='ink'){prefs.mode=mode;}if(basic){prefs.mode='ink';}
 if(prefs.mode!=='ink'&&prefs.mode!=='mobile'){prefs.mode='mobile';}
 if(prefs.font!=='large'&&prefs.font!=='xlarge'){prefs.font='normal';}
 prefs.interval=Math.max(15,Math.min(3600,Number(prefs.interval)||60));
 document.body.className='display board '+prefs.mode+(basic?' legacy':'')+' '+prefs.font;
 document.documentElement.style.overflow='hidden';document.documentElement.style.height='100%';
 function remember(){try{localStorage.setItem('inkboard.display.v1',JSON.stringify(prefs));}catch(e){}}
 if(mode){remember();}
 function el(id){return document.getElementById(id);}
 function hasClass(node,name){return node&&node.nodeType===1&&(' '+node.className+' ').indexOf(' '+name+' ')>=0;}
 function ancestor(node,attribute){while(node&&node!==book){if(node.nodeType===1&&node.hasAttribute(attribute)){return node;}node=node.parentNode;}return null;}
 function inRail(node){while(node&&node!==book){if(hasClass(node,'rail')){return true;}node=node.parentNode;}return false;}
 function interactive(node){while(node&&node!==book){if(node.nodeType===1&&/^(A|BUTTON|INPUT|SELECT|TEXTAREA)$/.test(node.tagName)){return true;}node=node.parentNode;}return false;}
 function notify(text){var n=el('board-notice');n.textContent=text;n.removeAttribute('hidden');clearTimeout(noticeTimer);noticeTimer=setTimeout(function(){n.setAttribute('hidden','');},8000);}
 function status(text){var n=el('sync-status');if(n&&n.textContent!==text){n.textContent=text;}}
 function currentMain(){return pages[index]?pages[index].getAttribute('data-main'):'';}
 function bindPages(main){pages=Array.prototype.slice.call(book.querySelectorAll('.sheet'));index=0;for(var i=0;i<pages.length;i++){if(pages[i].getAttribute('data-main')===main){index=i;break;}}show(index,false);}
 function show(i,restart){
  if(!pages.length){return;}index=(i+pages.length)%pages.length;
  for(var n=0;n<pages.length;n++){var value='sheet'+(n===index?' active':'');if(pages[n].className!==value){pages[n].className=value;}pages[n].setAttribute('aria-hidden',n===index?'false':'true');}
  var label=el('page-label');if(label){label.textContent=(index+1)+'/'+pages.length+' · '+pages[index].getAttribute('data-name');}
  var rotate=el('rotate');if(rotate){rotate.textContent=prefs.rotate?'轮播开':'轮播关';rotate.setAttribute('aria-pressed',prefs.rotate?'true':'false');}
  book.setAttribute('data-page-count',pages.length);book.setAttribute('data-current-main',currentMain());
  if(restart){startRotation();}
 }
 function startRotation(){clearTimeout(timer);if(prefs.rotate&&!document.hidden&&!selectedHour&&!selectedDate&&!history&&pages.length>1){timer=setTimeout(function(){show(index+1,true);},prefs.interval*1000);}}
 function turn(direction){show(index+(direction==='previous'?-1:1),true);}
 function key(node){if(node.nodeType!==1){return '';}return node.getAttribute('data-main')||node.getAttribute('data-block')||node.id||'';}
 function compatible(a,b){return a&&b&&a.nodeType===b.nodeType&&(a.nodeType!==1||a.tagName===b.tagName)&&key(a)===key(b);}
 function morph(oldNode,newNode){
  if(oldNode.nodeType===3||oldNode.nodeType===8){if(oldNode.nodeValue!==newNode.nodeValue){oldNode.nodeValue=newNode.nodeValue;}return;}
  var oldAttrs=oldNode===book?[]:Array.prototype.slice.call(oldNode.attributes),newAttrs=oldNode===book?[]:Array.prototype.slice.call(newNode.attributes);
  var sheet=oldNode.hasAttribute('data-main');
  for(var a=0;a<oldAttrs.length;a++){var name=oldAttrs[a].name;if(sheet&&(name==='class'||name==='aria-hidden')){continue;}if(!newNode.hasAttribute(name)){oldNode.removeAttribute(name);}}
  for(var b=0;b<newAttrs.length;b++){var attr=newAttrs[b];if(sheet&&(attr.name==='class'||attr.name==='aria-hidden')){continue;}if(oldNode.getAttribute(attr.name)!==attr.value){oldNode.setAttribute(attr.name,attr.value);}}
  var incoming=Array.prototype.slice.call(newNode.childNodes),cursor=oldNode.firstChild;
  for(var c=0;c<incoming.length;c++){
   var child=incoming[c],match=cursor;
   if(key(child)&&(!match||key(match)!==key(child))){match=null;for(var find=cursor;find;find=find.nextSibling){if(compatible(find,child)){match=find;break;}}}
   if(!compatible(match,child)){match=child.cloneNode(true);oldNode.insertBefore(match,cursor);}
   else{if(match!==cursor){oldNode.insertBefore(match,cursor);}morph(match,child);}
   cursor=match.nextSibling;
  }
  while(cursor){var next=cursor.nextSibling;oldNode.removeChild(cursor);cursor=next;}
 }
 function merge(html){
  var main=currentMain(),holder=document.createElement('div');holder.innerHTML=html;
  // Diff in place. Unchanged cards, horizontal offsets and focused controls survive.
  morph(book,holder);bindPages(main);markSelection();
  book.setAttribute('data-sync-count',Number(book.getAttribute('data-sync-count')||0)+1);
 }
 function markSelection(){
  var dates=book.querySelectorAll('[data-date]'),hours=book.querySelectorAll('[data-hour]');
  for(var i=0;i<dates.length;i++){dates[i].className='forecast forecast-tile'+(dates[i].getAttribute('data-date')===selectedDate?' selected':'');}
  for(var j=0;j<hours.length;j++){hours[j].className='hour forecast-tile'+(hours[j].getAttribute('data-hour')===selectedHour?' selected':'');}
 }
 function queryKey(){return 'date='+encodeURIComponent(selectedDate)+'&hour='+encodeURIComponent(selectedHour)+'&history='+(history?'1':'0');}
 function sync(done){
  if(done){callbacks.push(done);}if(request){pending=true;return;}
  var xhr=new XMLHttpRequest(),selection=queryKey();request=xhr;pending=false;
  xhr.open('GET','/fragments?'+selection,true);try{xhr.timeout=16000;}catch(e){}
  xhr.onload=function(){if(selection!==queryKey()){pending=true;return;}if(xhr.status===200){merge(xhr.responseText);status('已同步');}else{status('更新失败 · 保留数据');}};
  xhr.onerror=xhr.ontimeout=function(){status('离线 · 保留最后数据');};
  var finished=false;
  function finish(){if(finished){return;}finished=true;request=null;if(pending){sync();return;}var queue=callbacks;callbacks=[];for(var i=0;i<queue.length;i++){queue[i]();}scheduleRefresh();}
  xhr.onloadend=finish;
  // Some old WebKit builds omit loadend.
  xhr.onreadystatechange=function(){if(xhr.readyState===4){setTimeout(finish,0);}};
  xhr.send();
 }
 function scheduleRefresh(){clearTimeout(refreshTimer);if(!document.hidden){refreshTimer=setTimeout(function(){sync();},prefs.mode==='ink'?60000:15000);}}
 function requestWake(){if(prefs.wake&&window.isSecureContext&&navigator.wakeLock&&!wakeLock){navigator.wakeLock.request('screen').then(function(lock){wakeLock=lock;lock.addEventListener('release',function(){wakeLock=null;});}).catch(function(){status('常亮不可用');});}}
 function resume(){clearTimeout(timer);clearTimeout(refreshTimer);if(document.hidden){return;}sync(function(){startRotation();requestWake();});}
 function fullscreen(){var fn=document.documentElement.requestFullscreen||document.documentElement.webkitRequestFullscreen;if(fn){var result=fn.call(document.documentElement);if(result&&result.catch){result.catch(function(){notify('全屏不可用，可将看板添加到主屏幕。');});}}else{notify('此浏览器不支持全屏，可将看板添加到主屏幕或使用设备全屏工具。');}}
 book.addEventListener('click',function(e){
  if(Date.now()<suppressClickUntil){e.preventDefault();return;}
  var node=ancestor(e.target,'data-date');if(node){e.preventDefault();selectedDate=node.getAttribute('data-date');selectedHour='';history=false;startRotation();sync();return;}
  node=ancestor(e.target,'data-hour');if(node){selectedHour=node.getAttribute('data-hour');startRotation();sync();return;}
  node=ancestor(e.target,'data-current');if(node){selectedHour='';selectedDate='';history=false;sync(function(){startRotation();});return;}
  node=ancestor(e.target,'data-turn');if(node){turn(node.getAttribute('data-turn'));return;}
  node=ancestor(e.target,'data-history-main');if(node){history=true;startRotation();sync(function(){notify('正在查看历史快照，以下指标不是实时状态。');});return;}
  node=e.target;while(node&&node!==book&&!node.id){node=node.parentNode;}
  if(node&&node.id==='fullscreen'){fullscreen();}else if(node&&node.id==='rotate'){prefs.rotate=!prefs.rotate;remember();show(index,true);}
 });
 function begin(x,y,target){if(inRail(target)||interactive(target)){gesture=null;return;}gesture={x:x,y:y,time:Date.now()};}
 function end(x,y){if(!gesture){return;}var dx=x-gesture.x,dy=y-gesture.y,elapsed=Date.now()-gesture.time;gesture=null;if(Math.abs(dx)>=50&&Math.abs(dx)>Math.abs(dy)*1.5&&elapsed<1800){turn(dx>0?'previous':'next');suppressClickUntil=Date.now()+350;}}
 book.addEventListener('touchstart',function(e){if(e.touches.length===1){begin(e.touches[0].clientX,e.touches[0].clientY,e.target);}else{gesture=null;}},false);
 book.addEventListener('touchmove',function(e){if(gesture&&e.touches.length===1&&Math.abs(e.touches[0].clientX-gesture.x)>Math.abs(e.touches[0].clientY-gesture.y)*1.5){e.preventDefault();}},false);
 book.addEventListener('touchend',function(e){if(e.changedTouches.length){end(e.changedTouches[0].clientX,e.changedTouches[0].clientY);}},false);
 book.addEventListener('touchcancel',function(){gesture=null;},false);
 book.addEventListener('mousedown',function(e){if(e.button===0){begin(e.clientX,e.clientY,e.target);}},false);
 document.addEventListener('mouseup',function(e){end(e.clientX,e.clientY);},false);
 document.addEventListener('keydown',function(e){
  if(/^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)||e.target.isContentEditable||e.ctrlKey||e.metaKey||e.altKey){return;}
  var code=e.keyCode||e.which;
  if((code===37||code===39)&&inRail(e.target)){return;}
  if(code===37||code===33||e.key==='PageUp'){e.preventDefault();turn('previous');}
  else if(code===39||code===34||e.key==='PageDown'){e.preventDefault();turn('next');}
 },false);
 // Optional device tools may translate PagePress into this standard bridge.
 window.addEventListener('inkboard:turn',function(e){if(e.detail==='previous'||e.detail==='next'){turn(e.detail);}},false);
 function resize(){clearTimeout(resizeTimer);resizeTimer=setTimeout(function(){var height=window.visualViewport?window.visualViewport.height:window.innerHeight;book.setAttribute('data-viewport',window.innerWidth+'x'+Math.round(height));},100);}
 document.addEventListener('visibilitychange',resume,false);window.addEventListener('online',resume,false);window.addEventListener('pageshow',function(e){if(e.persisted){resume();}},false);
 window.addEventListener('resize',resize,false);if(window.visualViewport){window.visualViewport.addEventListener('resize',resize,false);}
 var initial=book.querySelector('.sheet.active');bindPages(initial?initial.getAttribute('data-main'):'');markSelection();resize();scheduleRefresh();startRotation();requestWake();
}());
