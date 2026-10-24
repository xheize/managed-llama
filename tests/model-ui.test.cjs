const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'../web/app.js'),'utf8');
const section=(start,end)=>source.slice(source.indexOf(start),source.indexOf(end));

function fixture(modelResponse){
  const elements=new Map();
  const $=selector=>{
    if(!elements.has(selector))elements.set(selector,{
      value:'',textContent:'',innerHTML:'',open:false,
      classList:{add(){},remove(){}},
      showModal(){this.open=true},close(){this.open=false},
    });
    return elements.get(selector);
  };
  $('#configForm').elements={start_with_windows:{},selected_model:{value:''}};
  $('#hfSettingsForm').elements={hugging_face_base:{}};
  $('#serviceKey').value='test-service-key';
  $('#serviceLogin').open=true;
  $('#uploadInput').files=[new Blob(['GGUF'])];
  const requests=[],uploads=[];
  class XMLHttpRequest{
    constructor(){this.upload={};this.headers={}}
    open(method,url){this.method=method;this.url=url}
    setRequestHeader(name,value){this.headers[name]=value}
    send(body){this.body=body;uploads.push(this)}
  }
  const fetch=async(url,options)=>{
    requests.push({url,options});
    const payload={
      '/api/state':{},'/api/config':{selected_model:'',hugging_face_base:'https://huggingface.co'},
      '/api/autostart':{enabled:false},'/api/models':modelResponse,'/api/command':{command:'preview'},
    };
    assert.ok(Object.hasOwn(payload,url),`unexpected request: ${url}`);
    return {status:200,ok:true,json:async()=>payload[url]};
  };
  const context=vm.createContext({$,Headers,fetch,FormData,XMLHttpRequest,
    loadState:async()=>{},gatewayHealth:async()=>{},status:async()=>{},logs:async()=>{},toast(){},
  });
  vm.runInContext('let config={},models=[];'+
    section("let serviceKey=",'const toast=')+
    section('async function loadConfig()','async function saveConfig()')+
    section('async function preview()',"$('#saveBtn').onclick=")+
    section('async function loadModels()',"$('#refreshModels').onclick=")+
    section("$('#uploadInput').onchange=",'async function loadResources()'),context);
  return {$,requests,uploads,run:code=>vm.runInContext(code,context)};
}

for(const response of [[],null]){
  test(`login with ${JSON.stringify(response)} model list keeps authentication and renders empty library`,async()=>{
    const f=fixture(response),button={disabled:false};
    await f.$('#serviceLoginForm').onsubmit({preventDefault(){},currentTarget:{querySelector:()=>button}});
    assert.equal(f.run('serviceKey'),'test-service-key');
    assert.equal(f.$('#serviceLogin').open,false);
    assert.equal(f.$('#serviceLoginError').textContent,'');
    assert.equal(f.$('#modelList').textContent,'모델 디렉터리가 비어 있습니다.');
    assert.equal(f.$('#commandPreview').textContent,'preview');
    assert.equal(button.disabled,false);
    await f.run("api('/api/state')");
    assert.equal(f.requests.at(-1).options.headers.get('Authorization'),'Bearer test-service-key');
    assert.equal(f.requests.at(-1).options.headers.has('X-Managed-Llama-Key'),false);
  });
}

for(const key of ['test-service-key','']){
  test(`model upload ${key?'includes service authentication':'works without a service key in user mode'}`,()=>{
    const f=fixture([]);
    f.run(`serviceKey=${JSON.stringify(key)}`);
    f.$('#uploadInput').onchange();
    assert.equal(f.uploads.length,1);
    const request=f.uploads[0];
    assert.equal(request.method,'POST');
    assert.equal(request.url,'/api/models/upload');
    assert.equal(request.headers['Authorization'],key?'Bearer '+key:undefined);
    assert.equal(request.headers['X-Managed-Llama-Key'],undefined);
    assert.equal(request.headers['Content-Type'],undefined,'browser must generate the multipart boundary');
    assert.equal(request.body.get('file').size,4);
    assert.equal(typeof request.upload.onprogress,'function');
  });
}
