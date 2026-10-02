(function () {
 'use strict';
 var book=document.getElementById('book'),pages=[],index=0,timer=null,refreshTimer=null,request=null,resizeTimer=null,wakeLock=null;
 var basic=document.body.getAttribute('data-basic')==='true',history=document.body.getAttribute('data-history')==='true';
 var prefs={mode:basic?'ink':'mobile',font:'normal',rotate:false,interval:60,wake:false,kindleBottom:64};
 var selectedDate=query('date'),selectedHour=query('hour'),pending=false,callbacks=[],noticeTimer=null,gesture=null,suppressClickUntil=0,railOffsets={};
 function query(name){var m=new RegExp('(?:[?&])'+name+'=([^&]*)').exec(location.search);return m?decodeURIComponent(m[1]):'';}
 try{var saved=JSON.parse(localStorage.getItem('inkboard.display.v1')||'{}');for(var k in prefs){if(saved[k]!==undefined){prefs[k]=saved[k];}}}catch(e){}
 var mode=query('mode');if(mode==='mobile'||mode==='ink'){prefs.mode=mode;}if(basic){prefs.mode='ink';}
 if(prefs.mode!=='ink'&&prefs.mode!=='mobile'){prefs.mode='mobile';}
 if(prefs.font!=='large'&&prefs.font!=='xlarge'){prefs.font='normal';}
 prefs.interval=Math.max(15,Math.min(3600,Number(prefs.interval)||60));
 var bottom=query('bottom');if(bottom!==''&&isFinite(Number(bottom))){prefs.kindleBottom=Number(bottom);}
 prefs.kindleBottom=Math.max(0,Math.min(200,Number(prefs.kindleBottom)||0));
 document.body.className='display board '+prefs.mode+(basic?' legacy':'')+' '+prefs.font;
 document.documentElement.style.overflow='hidden';document.documentElement.style.height='100%';
 function remember(){try{localStorage.setItem('inkboard.display.v1',JSON.stringify(prefs));}catch(e){}}
 if(mode||bottom!==''){remember();}
 function el(id){return document.getElementById(id);}
 function hasClass(node,name){return node&&node.nodeType===1&&(' '+node.className+' ').indexOf(' '+name+' ')>=0;}
 function ancestor(node,attribute){while(node&&node!==book){if(node.nodeType===1&&node.hasAttribute(attribute)){return node;}node=node.parentNode;}return null;}
 function inRail(node){while(node&&node!==book){if(hasClass(node,'scroll-rail')){return true;}node=node.parentNode;}return false;}
 function interactive(node){while(node&&node!==book){if(node.nodeType===1&&/^(A|BUTTON|INPUT|SELECT|TEXTAREA)$/.test(node.tagName)){return true;}node=node.parentNode;}return false;}
 function notify(text){var n=el('board-notice');n.textContent=text;n.removeAttribute('hidden');clearTimeout(noticeTimer);noticeTimer=setTimeout(function(){n.setAttribute('hidden','');},8000);}
 function status(text){var n=el('sync-status');if(n&&n.textContent!==text){n.textContent=text;}}
 function currentMain(){return pages[index]?pages[index].getAttribute('data-main'):'';}
 function listKey(node){return node.parentNode.getAttribute('data-block')||node.getAttribute('data-list');}
 function captureOffsets(){var lists=book.querySelectorAll('.sheet.active .scroll-rail');for(var i=0;i<lists.length;i++){railOffsets[listKey(lists[i])]=lists[i].scrollLeft;}}
 function geometry(){
  var height=window.innerHeight||document.documentElement.clientHeight;
  if(window.visualViewport){height=Math.min(height,window.visualViewport.height);}
  var inset=basic?prefs.kindleBottom:0;
  document.body.style.height=Math.max(1,Math.floor(height-inset))+'px';document.body.style.bottom='auto';
  book.setAttribute('data-bottom-inset',inset);book.setAttribute('data-viewport',window.innerWidth+'x'+Math.floor(height));
  var control=el('bottom-space');if(control){control.textContent='底部 '+inset+'px';}
 }
 function listColumns(kind,width,count){
  var minimum=kind==='disks'?250:kind==='gpu'?240:kind==='sensors'?150:kind==='traffic'?170:kind==='volumes'?120:175;
  return kind==='clocks'?Math.max(1,count):Math.max(1,Math.min(count,Math.floor(width/minimum)));
 }
 // Kindle's old WebKit can use a different minimum font size. Measure content,
 // rather than cutting it into fixed percentages of the reported screen height.
 function layoutLegacy(page){
  var width=page.clientWidth,height=page.clientHeight,landscape=width>height,weather=page.getAttribute('data-main')==='weather';
  var rows=weather?(landscape?[[['clocks-0',.43],['current',.56]],[['today',.43],['forecast',.56]],[['controls',1]]]:[[['clocks-0',1]],[['current',1]],[['today',1]],[['forecast',1]],[['controls',1]]]):[[['-state',1]],[['-system',1]],[['-gpu',.49],['-volumes',.50]],[['-disks',1]],[['-network',.32],['-io',.32],['-sensors',.34]]];
  if(weather&&width<=400){rows=[[['clocks-0',1]],[['current',1]],[['today',.49],['forecast',.50]],[['controls',1]]];}
  if(!weather&&landscape){rows=[[['-state',.32],['-system',.67]],[['-gpu',.32],['-volumes',.32],['-disks',.34]],[['-network',.32],['-io',.32],['-sensors',.34]]];}
  var offline=page.querySelector('[data-block$="-offline"]');if(offline){rows=[[['-state',1]],[['-offline',1]]];}
  function find(key){return page.querySelector(key.charAt(0)==='-'?'[data-block$="'+key+'"]':'[data-block="'+key+'"]')||(key==='current'?page.querySelector('[data-block="welcome"]'):null);}
  function measure(){
   var result=[],total=0;
   for(var r=0;r<rows.length;r++){
    var row={cells:[],height:0},left=0;
    for(var c=0;c<rows[r].length;c++){
     var cell=rows[r][c],block=find(cell[0]);if(!block){continue;}
     block.style.left=Math.round(left*width)+'px';block.style.width=Math.floor(cell[1]*width)+'px';block.style.top='0px';block.style.height='auto';left+=cell[1]+.01;
     var lists=block.querySelectorAll('[data-list]');
     for(var l=0;l<lists.length;l++){
      var list=lists[l],kind=list.getAttribute('data-list'),children=list.children,listWidth=list.clientWidth;
      list.style.height='auto';list.className=(kind==='clocks'?'clocks':'rows')+' item-list scroll-rail';
      var target=kind==='hours'||kind==='days'?Math.min(listWidth,174):kind==='clocks'?(listWidth/Math.max(1,children.length)<90?140:Math.floor(listWidth/Math.max(1,children.length))):kind==='disks'?300:kind==='gpu'?Math.max(160,listWidth):kind==='traffic'?170:kind==='sensors'?150:Math.max(kind==='volumes'?120:175,Math.floor(listWidth/listColumns(kind,listWidth,children.length)));
      for(var i=0;i<children.length;i++){children[i].style.width=target+'px';}
     }
     var required=Math.ceil(block.getBoundingClientRect().height);block.setAttribute('data-required-height',required);
     row.cells.push(block);row.height=Math.max(row.height,required);
    }
    if(row.cells.length){result.push(row);total+=row.height;}
   }
   return {rows:result,total:total+Math.max(0,result.length-1)*6};
  }
  document.body.className=document.body.className.replace(/ legacy-compact/g,'');
  var layout=measure();if(layout.total>height){document.body.className+=' legacy-compact';layout=measure();}
  if(weather&&layout.total>height){
   rows=landscape?[[['clocks-0',.43],['current',.56]],[['today',.29],['forecast',.29],['controls',.40]]]:[[['clocks-0',1]],[['current',1]],[['today',.49],['forecast',.50]],[['controls',1]]];
   layout=measure();
  }
  if(!weather&&!offline&&landscape&&layout.total>height){
   rows=[[['-state',.32],['-system',.67]],[['-gpu',.156],['-volumes',.156],['-disks',.156],['-network',.156],['-io',.156],['-sensors',.17]]];layout=measure();
  }
  var spare=Math.max(0,height-layout.total),top=0;
  for(var r=0;r<layout.rows.length;r++){
   var row=layout.rows[r],extra=Math.floor(spare/(layout.rows.length-r));spare-=extra;
   for(var c=0;c<row.cells.length;c++){row.cells[c].style.top=top+'px';row.cells[c].style.height=(row.height+extra)+'px';}
   top+=row.height+extra+6;
  }
  book.setAttribute('data-content-height',top-6);
 }
 function layoutLists(){
  var page=pages[index];if(!page){return;}var lists=page.querySelectorAll('[data-list]');
  geometry();if(basic){layoutLegacy(page);}
  for(var i=0;i<lists.length;i++){
   var list=lists[i],kind=list.getAttribute('data-list'),block=list.parentNode;
   var style=window.getComputedStyle?window.getComputedStyle(block):block.currentStyle;
   var bottomPadding=parseFloat(style&&style.paddingBottom)||0;
   var available=Math.max(0,block.getBoundingClientRect().bottom-list.getBoundingClientRect().top-bottomPadding-1);
   list.style.height=Math.floor(available)+'px';list.className=(kind==='clocks'?'clocks':'rows')+' item-list';
   var children=list.children,width=list.clientWidth,columns=1,minimum=kind==='disks'?250:kind==='gpu'?240:kind==='sensors'?150:kind==='traffic'?170:kind==='volumes'?120:175;
   if(kind==='clocks'){columns=children.length;minimum=basic?90:80;}
   else if(kind!=='hours'&&kind!=='days'){columns=Math.max(1,Math.min(children.length,Math.floor(width/minimum)));}
   var alwaysRail=kind==='hours'||kind==='days',cellWidth=Math.floor(width/Math.max(1,columns));
   for(var c=0;c<children.length;c++){children[c].style.width=cellWidth+'px';}
   var tooWide=(kind==='clocks'&&cellWidth<minimum)||(kind==='gpu'&&width<160),tooTall=list.scrollHeight>available+2;
   if(alwaysRail||tooWide||tooTall){
    list.className+=( ' scroll-rail');list.setAttribute('tabindex','0');
    var target=kind==='hours'?174:kind==='days'?174:kind==='clocks'?minimum:minimum;
    if(kind==='clocks'){target=Math.max(target,140);}
    if(kind==='hours'||kind==='days'||kind==='gpu'){target=Math.min(width,target);}
    if(kind==='disks'){target=Math.max(300,target);}
    if(kind==='gpu'){target=Math.max(160,width);}
    for(var r=0;r<children.length;r++){children[r].style.width=Math.floor(target)+'px';}
    list.scrollLeft=railOffsets[listKey(list)]||0;
   }else{list.removeAttribute('tabindex');}
  }
 }
 function bindPages(main){pages=Array.prototype.slice.call(book.querySelectorAll('.sheet'));index=0;for(var i=0;i<pages.length;i++){if(pages[i].getAttribute('data-main')===main){index=i;break;}}show(index,false);}
 function show(i,restart){
  if(!pages.length){return;}index=(i+pages.length)%pages.length;
  for(var n=0;n<pages.length;n++){var value='sheet'+(n===index?' active':'');if(pages[n].className!==value){pages[n].className=value;}pages[n].setAttribute('aria-hidden',n===index?'false':'true');}
  var label=el('page-label');if(label){label.textContent=(index+1)+'/'+pages.length+' · '+pages[index].getAttribute('data-name');}
  var rotate=el('rotate');if(rotate){rotate.textContent=prefs.rotate?'轮播开':'轮播关';rotate.setAttribute('aria-pressed',prefs.rotate?'true':'false');}
  book.setAttribute('data-page-count',pages.length);book.setAttribute('data-current-main',currentMain());
  layoutLists();
  if(restart){startRotation();}
 }
 function startRotation(){clearTimeout(timer);if(prefs.rotate&&!document.hidden&&!selectedHour&&!selectedDate&&!history&&pages.length>1){timer=setTimeout(function(){show(index+1,true);},prefs.interval*1000);}}
 function turn(direction){captureOffsets();show(index+(direction==='previous'?-1:1),true);}
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
  captureOffsets();
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
  var node=ancestor(e.target,'data-reason');if(node){notify(node.getAttribute('data-reason'));return;}
  node=ancestor(e.target,'data-date');if(node){e.preventDefault();selectedDate=node.getAttribute('data-date');selectedHour='';history=false;startRotation();sync();return;}
  node=ancestor(e.target,'data-hour');if(node){selectedHour=node.getAttribute('data-hour');startRotation();sync();return;}
  node=ancestor(e.target,'data-current');if(node){selectedHour='';selectedDate='';history=false;sync(function(){startRotation();});return;}
  node=ancestor(e.target,'data-turn');if(node){turn(node.getAttribute('data-turn'));return;}
  node=ancestor(e.target,'data-bottom-space');if(node){var steps=[0,48,64,80,96,120],next=0;for(var s=0;s<steps.length;s++){if(steps[s]>prefs.kindleBottom){next=steps[s];break;}}captureOffsets();prefs.kindleBottom=next;remember();layoutLists();notify('底部已留出 '+next+' 像素。继续点击可调整；只影响这个浏览器的 Kindle 页面。');return;}
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
 function resize(){captureOffsets();clearTimeout(resizeTimer);resizeTimer=setTimeout(layoutLists,100);}
 document.addEventListener('visibilitychange',resume,false);window.addEventListener('online',resume,false);window.addEventListener('pageshow',function(e){if(e.persisted){resume();}},false);
 window.addEventListener('resize',resize,false);if(window.visualViewport){window.visualViewport.addEventListener('resize',resize,false);}
 var initial=book.querySelector('.sheet.active');bindPages(initial?initial.getAttribute('data-main'):'');markSelection();resize();scheduleRefresh();startRotation();requestWake();
}());
