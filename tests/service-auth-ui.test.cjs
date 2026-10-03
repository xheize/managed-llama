const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/app.js'),'utf8');
const auth=source.slice(source.indexOf("let serviceKey="),source.indexOf("$('#serviceLoginForm').onsubmit="));
function fixture(fetch){
  const dialog={open:false,showModal(){this.open=true}};
  const context=vm.createContext({Headers,fetch,$:()=>dialog});
  vm.runInContext(auth,context);
  return {dialog,run:code=>vm.runInContext(code,context)};
}
test('privileged API asks for authentication on 401',async()=>{
  const f=fixture(async()=>({status:401,ok:false,json:async()=>({error:'authentication required'})}));
  await assert.rejects(f.run("api('/api/config')"),/authentication required/);
  assert.equal(f.dialog.open,true);
});
test('key header preserves request content type and body',async()=>{
  const f=fixture(async(url,options)=>{
    assert.equal(url,'/api/config');
    assert.equal(options.headers.get('X-Managed-Llama-Key'),'test-service-key');
    assert.equal(options.headers.get('Content-Type'),'application/json');
    assert.equal(options.body,'{}');
    return {status:204,ok:true};
  });
  await f.run("serviceKey='test-service-key'; api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:'{}'})");
});
test('ordinary user mode does not invent a service key',async()=>{
  const f=fixture(async(url,options)=>{
    assert.equal(options.headers.has('X-Managed-Llama-Key'),false);
    return {status:204,ok:true};
  });
  await f.run("api('/api/state')");
  assert.equal(f.dialog.open,false);
});
