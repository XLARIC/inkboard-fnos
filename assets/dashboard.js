(function () {
 'use strict';
 var source=document.getElementById('source'),book=document.getElementById('book'),pages=[],index=0,timer=null,refreshTimer=null,resizing=null,request=null;
 var prefs={mode:'mobile',font:'normal',rotate:false,interval:60,wake:false};
 try {var saved=JSON.parse(localStorage.getItem('inkboard.display.v1')||'{}');for(var k in prefs){if(saved[k]!==undefined){prefs[k]=saved[k];}}}catch(e){}
 var requested=/(?:[?&])mode=(mobile|ink)(?:&|$)/.exec(location.search);if(requested){prefs.mode=requested[1];}
 if(['ink','mobile'].indexOf(prefs.mode)<0){prefs.mode='mobile';}if(['normal','large','xlarge'].indexOf(prefs.font)<0){prefs.font='normal';}
 prefs.interval=Math.max(15,Math.min(3600,Number(prefs.interval)||60));
 document.body.className='display '+prefs.mode+' '+prefs.font;
 document.documentElement.style.fontSize=(prefs.font==='xlarge'?24:prefs.font==='large'?21:18)+'px';
 function remember(){try{localStorage.setItem('inkboard.display.v1',JSON.stringify(prefs));}catch(e){}}
 if(requested){remember();}
 function anchor(){if(!pages[index]){return '';}var b=pages[index].querySelector('[data-anchor]');return b?b.getAttribute('data-anchor'):'';}
 function show(i,restart){
  if(!pages.length){return;}index=(i+pages.length)%pages.length;
  for(var n=0;n<pages.length;n++){pages[n].className='sheet'+(n===index?' active':'');}
  var current=pages[index],main=current.getAttribute('data-main');
  var same=pages.filter(function(p){return p.getAttribute('data-main')===main;});
  document.getElementById('page-label').textContent=(same.indexOf(current)+1)+'/'+same.length;
  document.getElementById('main-menu').textContent=current.getAttribute('data-name');
  document.getElementById('rotate').textContent=prefs.rotate?'轮播开':'轮播关';
  if(restart){startRotation();}
 }
 function startRotation(){clearTimeout(timer);if(prefs.rotate&&!document.hidden){timer=setTimeout(function(){show(index+1,true);},prefs.interval*1000);}}
 function newPage(group){
  var p=document.createElement('section');p.className='sheet active';p.setAttribute('data-main',group.getAttribute('data-main'));p.setAttribute('data-name',group.getAttribute('data-name'));book.appendChild(p);pages.push(p);return p;
 }
 function paginate(target){
  var saved=target||anchor(),currentMain=pages[index]?pages[index].getAttribute('data-main'):'';
  book.textContent='';pages=[];document.body.classList.add('ready');
  var height=window.visualViewport?window.visualViewport.height:window.innerHeight;
  var available=Math.max(90,height-document.querySelector('.topbar').getBoundingClientRect().height-document.getElementById('pager').getBoundingClientRect().height-8);
  book.style.height=available+'px';
  var groups=source.querySelectorAll('[data-main]'),options=document.getElementById('main-options');options.textContent='';
  for(var g=0;g<groups.length;g++){
   var group=groups[g],p=newPage(group),blocks=group.querySelectorAll('[data-block]');
   var mainButton=document.createElement('button');mainButton.textContent=group.getAttribute('data-name');mainButton.setAttribute('data-target',group.getAttribute('data-main'));mainButton.onclick=function(){for(var j=0;j<pages.length;j++){if(pages[j].getAttribute('data-main')===this.getAttribute('data-target')){show(j,true);break;}}document.getElementById('main-picker').hidden=true;};options.appendChild(mainButton);
   for(var b=0;b<blocks.length;b++){
    var block=blocks[b],rows=block.querySelectorAll('.rows>.row'),copy=block.cloneNode(true),id=block.getAttribute('data-block');
    if(!rows.length){
     copy.setAttribute('data-anchor',id);p.appendChild(copy);
     if(p.getBoundingClientRect().height>available&&p.children.length>1){p.removeChild(copy);p=newPage(group);p.appendChild(copy);}
     if(copy.getBoundingClientRect().height>available){p=splitContent(copy,p,group,available,id);}
    }else{
     copy.querySelector('.rows').textContent='';copy.setAttribute('data-anchor',id+':0');p.appendChild(copy);
     if(p.getBoundingClientRect().height+rows[0].getBoundingClientRect().height>available&&p.children.length>1){p.removeChild(copy);p=newPage(group);p.appendChild(copy);}
     for(var r=0;r<rows.length;r++){
      var row=rows[r].cloneNode(true);row.setAttribute('data-anchor',id+':'+r);copy.querySelector('.rows').appendChild(row);
      if(p.getBoundingClientRect().height>available&&(copy.querySelector('.rows').children.length>1||p.children.length>1)){
       copy.querySelector('.rows').removeChild(row);p=newPage(group);copy=block.cloneNode(true);copy.querySelector('.rows').textContent='';copy.setAttribute('data-anchor',id+':'+r);copy.querySelector('.rows').appendChild(row);p.appendChild(copy);
      }
      if(p.getBoundingClientRect().height>available&&copy.querySelector('.rows')&&copy.querySelector('.rows').children.length===1){p=splitContent(copy,p,group,available,id+':'+r);if(r<rows.length-1){p=newPage(group);copy=block.cloneNode(true);copy.querySelector('.rows').textContent='';p.appendChild(copy);}}
     }
    }
   }
  }
  index=0;
  var found=false;for(var i=0;i<pages.length;i++){var nodes=pages[i].querySelectorAll('[data-anchor]');for(var j=0;j<nodes.length;j++){if(nodes[j].getAttribute('data-anchor')===saved){index=i;found=true;break;}}if(found){break;}}
  if(!found&&saved){var stem=saved.split(':part')[0],blockId=saved.split(':')[0];for(var s=0;s<pages.length;s++){var anchors=pages[s].querySelectorAll('[data-anchor]');for(var a=0;a<anchors.length;a++){var value=anchors[a].getAttribute('data-anchor');if(value===stem||value.split(':')[0]===blockId){index=s;found=true;break;}}if(found){break;}}}
  if(!found&&currentMain){for(var z=0;z<pages.length;z++){if(pages[z].getAttribute('data-main')===currentMain){index=z;break;}}}
  show(index,false);
  book.setAttribute('data-viewport',window.innerWidth+'x'+Math.round(height));
 }
 function splitContent(copy,p,group,available,id){
  // Oversized cards become ordinary pages of semantic child content.
  // The hero and metric grid are decomposed before measuring, without reducing type.
  var rowContainer=copy.querySelector('.rows');if(rowContainer&&rowContainer.children.length===1){var single=rowContainer.firstElementChild.querySelector('.detail-row')||rowContainer.firstElementChild;while(single.firstChild){copy.insertBefore(single.firstChild,rowContainer);}copy.removeChild(rowContainer);}
  var hero=copy.querySelector('.hero'),grid=copy.querySelector('.metric-grid');
  if(hero){while(hero.firstChild){copy.insertBefore(hero.firstChild,hero);}copy.removeChild(hero);}
  if(grid){while(grid.firstChild){copy.insertBefore(grid.firstChild,grid);}copy.removeChild(grid);}
  var facts=copy.querySelector('.weather-facts');if(facts){while(facts.firstChild){copy.insertBefore(facts.firstChild,facts);}copy.removeChild(facts);}
  var clocks=copy.querySelector('.clocks');if(clocks){while(clocks.firstChild){copy.insertBefore(clocks.firstChild,clocks);}copy.removeChild(clocks);}
  var blank=copy.querySelector('.blank-metrics');if(blank){while(blank.firstChild){copy.insertBefore(blank.firstChild,blank);}copy.removeChild(blank);}
  var children=Array.prototype.slice.call(copy.children,1),heading=copy.querySelector('h2').cloneNode(true);copy.textContent='';copy.appendChild(heading);
  for(var i=0;i<children.length;i++){
   var child=children[i];child.setAttribute('data-anchor',id+':part'+i);copy.appendChild(child);
   if(p.getBoundingClientRect().height>available&&copy.children.length>2){
    copy.removeChild(child);p=newPage(group);var next=document.createElement('section');next.className='block';next.appendChild(heading.cloneNode(true));next.appendChild(child);p.appendChild(next);copy=next;
   }
  }
  return p;
 }
 function sync(done){
  if(request){if(done){request.addEventListener('loadend',done,{once:true});}return;}
  var xhr=new XMLHttpRequest();request=xhr;xhr.open('GET','/fragments');xhr.timeout=16000;
  xhr.onload=function(){if(xhr.status===200){var saved=anchor();source.innerHTML=xhr.responseText;paginate(saved);document.getElementById('sync-status').textContent='已同步 '+new Date().toLocaleTimeString([],{hour:'2-digit',minute:'2-digit',hour12:false});}else{document.getElementById('sync-status').textContent='更新失败 · 保留页面';}};
  xhr.onerror=xhr.ontimeout=function(){document.getElementById('sync-status').textContent='网络中断 · 保留页面';};
  xhr.onloadend=function(){request=null;if(done){done();}scheduleRefresh();};xhr.send();
 }
 function scheduleRefresh(){clearTimeout(refreshTimer);if(!document.hidden){refreshTimer=setTimeout(function(){sync();},prefs.mode==='ink'?60000:15000);}}
 function resume(){if(document.hidden){clearTimeout(timer);clearTimeout(refreshTimer);return;}clearTimeout(timer);sync(function(){startRotation();requestWake();});}
 function requestWake(){if(prefs.wake&&window.isSecureContext&&navigator.wakeLock){navigator.wakeLock.request('screen').then(function(lock){lock.addEventListener('release',function(){document.getElementById('sync-status').textContent='防休眠已释放';});}).catch(function(){document.getElementById('sync-status').textContent='防休眠不可用';});}}
 document.getElementById('fullscreen').onclick=function(){var fn=document.documentElement.requestFullscreen||document.documentElement.webkitRequestFullscreen;if(fn){var result=fn.call(document.documentElement);if(result&&result.catch){result.catch(function(){document.getElementById('sync-status').textContent='全屏不可用，请用主屏幕入口';});}}else{location.href='/device';}};
 document.getElementById('prev').onclick=function(){show(index-1,true);};
 document.getElementById('next').onclick=function(){show(index+1,true);};
 document.getElementById('rotate').onclick=function(){prefs.rotate=!prefs.rotate;remember();show(index,true);};
 document.getElementById('main-menu').onclick=function(){document.getElementById('main-picker').hidden=false;};
 document.getElementById('close-picker').onclick=function(){document.getElementById('main-picker').hidden=true;};
 document.addEventListener('visibilitychange',resume);window.addEventListener('online',resume);
 window.addEventListener('pageshow',function(e){if(e.persisted){resume();}});
 function resized(){clearTimeout(resizing);resizing=setTimeout(function(){paginate();},150);}
 window.addEventListener('resize',resized);if(window.visualViewport){window.visualViewport.addEventListener('resize',resized);}
 document.addEventListener('keydown',function(e){if(e.key==='ArrowRight'){show(index+1,true);}else if(e.key==='ArrowLeft'){show(index-1,true);}});
 paginate();scheduleRefresh();startRotation();requestWake();
}());
