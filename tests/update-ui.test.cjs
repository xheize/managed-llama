const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../web/app.js'),'utf8');
const block=source.slice(source.indexOf('let appUpdate='),source.indexOf('loadAppUpdate();setInterval'));
function fixture(state){
 const nodes=new Map(),calls=[];
 function node(selector){if(!nodes.has(selector))nodes.set(selector,{textContent:'',value:'',disabled:false,classes:new Set(),classList:{toggle(name,enabled){const target=nodes.get(selector);enabled?target.classes.add(name):target.classes.delete(name)}},querySelector(){return node(selector+' button')}});return nodes.get(selector)}
 const context=vm.createContext({$:node,document:{activeElement:null,querySelector:node},bytes:n=>`${n} bytes`,confirm:()=>true,api:async(url,options)=>{calls.push({url,options});return state}});
 vm.runInContext(block,context);
 return {node,calls,run:code=>vm.runInContext(code,context)};
}
test('example URL disables checking and never requests a release',async()=>{
 const f=fixture({current:'dev',repository:'https://github.com/OWNER/managed-llama',configured:false,phase:'idle',available:false});
 await f.run('loadAppUpdate()');
 assert.deepEqual(f.calls.map(c=>c.url),['/api/updates']);
 assert.equal(f.node('#checkAppUpdate').disabled,true);
 assert.equal(f.node('#installAppUpdate').classes.has('hidden'),true);
 assert.match(f.node('#appUpdateStatus').textContent,/예시 URL/);
});
test('new release shows notification and explicit restart action',async()=>{
 const f=fixture({current:'v1.0.0',latest:'v1.1.0',repository:'acme/repo',configured:true,phase:'idle',available:true});
 await f.run('loadAppUpdate()');
 assert.equal(f.node('#appUpdateNotice').classes.has('hidden'),false);
 assert.equal(f.node('#installAppUpdate').disabled,false);
 await f.node('#installAppUpdate').onclick();
 const install=f.calls.find(c=>c.url==='/api/updates/install');
 assert.equal(install.options.method,'POST');
 assert.deepEqual(JSON.parse(install.options.body),{version:'v1.1.0'});
});
test('download progress prevents repeated installation',async()=>{
 const f=fixture({current:'v1.0.0',latest:'v1.1.0',configured:true,phase:'downloading',available:true,downloaded:25,total:100});
 await f.run('loadAppUpdate()');
 assert.equal(f.node('#appUpdateProgress').value,25);
 assert.equal(f.node('#installAppUpdate').disabled,true);
 assert.deepEqual(f.calls.map(c=>c.url),['/api/updates']);
});
