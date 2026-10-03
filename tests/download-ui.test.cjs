const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/app.js'),'utf8');
const loadFiles=source.slice(source.indexOf('async function loadFiles('),source.indexOf('async function startDownload('));
function fixture(api){
 const classes=new Set(['hidden']);
 const area={dataset:{},classList:{contains:x=>classes.has(x),add:x=>classes.add(x),remove:x=>classes.delete(x)},querySelectorAll:()=>[],textContent:'',innerHTML:''};
 const button={dataset:{repo:'test/model'},parentElement:{querySelector:()=>area},setAttribute(k,v){this[k]=v}};
 const context=vm.createContext({api,esc:x=>x,bytes:x=>x});
 vm.runInContext(loadFiles,context);
 return {area,button,toggle:()=>context.loadFiles(button)};
}
test('GGUF list opens, closes and reopens without another request',async()=>{
 let calls=0;
 const f=fixture(async()=>{calls++;return []});
 await f.toggle();assert.equal(f.button.textContent,'닫기');assert.equal(f.button['aria-expanded'],'true');
 await f.toggle();assert.equal(f.button.textContent,'GGUF 보기');assert.ok(f.area.classList.contains('hidden'));
 await f.toggle();assert.equal(calls,1);assert.equal(f.button.textContent,'닫기');
});
test('closing during a pending request stays closed when it completes',async()=>{
 let resolve;
 const f=fixture(()=>new Promise(r=>{resolve=r}));
 const pending=f.toggle();await f.toggle();resolve([]);await pending;
 assert.ok(f.area.classList.contains('hidden'));assert.equal(f.button.textContent,'GGUF 보기');
});
test('failed listing can be retried after closing',async()=>{
 let calls=0;
 const f=fixture(async()=>{if(++calls===1)throw Error('offline');return []});
 await f.toggle();assert.equal(f.area.textContent,'offline');
 await f.toggle();await f.toggle();assert.equal(calls,2);assert.equal(f.area.dataset.loaded,'true');
});
