(function () {
 'use strict';
 var root=document.createElement('div'),items=[];
 root.className='toast-region';root.setAttribute('aria-label','操作提示');
 document.body.appendChild(root);
 function remove(item){clearTimeout(item.timer);var i=items.indexOf(item);if(i!==-1){items.splice(i,1);}if(item.node.parentNode){item.node.parentNode.removeChild(item.node);}}
 function clear(key){items.slice().forEach(function(item){if(item.key===key){remove(item);}});}
 function show(text,type,key){
  key=key||'main';clear(key);if(!text){return;}
  type=type==='error'||type==='success'?type:'info';
  var node=document.createElement('div'),icon=document.createElement('span'),content=document.createElement('div'),title=document.createElement('strong'),body=document.createElement('p'),close=document.createElement('button');
  node.className='toast toast-'+type;node.setAttribute('role',type==='error'?'alert':'status');node.setAttribute('aria-atomic','true');
  icon.className='toast-icon';icon.textContent=type==='error'?'!':type==='success'?'✓':'i';icon.setAttribute('aria-hidden','true');
  content.className='toast-content';title.textContent=type==='error'?'操作未完成':type==='success'?'操作成功':'提示';body.textContent=String(text);content.appendChild(title);content.appendChild(body);
  close.type='button';close.className='toast-close';close.textContent='×';close.setAttribute('aria-label','关闭提示');
  node.appendChild(icon);node.appendChild(content);node.appendChild(close);
  var item={node:node,key:key,timer:null};items.push(item);root.appendChild(node);close.onclick=function(){remove(item);};
  function schedule(){clearTimeout(item.timer);if(type!=='error'){item.timer=setTimeout(function(){remove(item);},type==='success'?5000:8000);}}
  node.onmouseenter=function(){clearTimeout(item.timer);};node.onmouseleave=schedule;node.onfocusin=function(){clearTimeout(item.timer);};node.onfocusout=schedule;
  schedule();while(items.length>3){remove(items[0]);}
 }
 window.InkBoardToast={show:show,clear:clear,mount:function(parent){parent.appendChild(root);}};
 document.addEventListener('submit',function(){clear('validation');},true);
 document.addEventListener('invalid',function(e){var input=e.target,label=input.id&&document.querySelector('label[for="'+input.id+'"]');e.preventDefault();show((label?label.textContent+'：':'')+input.validationMessage,'error','validation');},true);
}());
