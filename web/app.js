const $=s=>document.querySelector(s), $$=s=>[...document.querySelectorAll(s)];
function setHFTokenVisible(visible){const input=$('#hfToken'),button=$('#toggleHFToken');input.type=visible?'text':'password';button.setAttribute('aria-pressed',String(visible));button.setAttribute('aria-label',visible?'토큰 숨기기':'토큰 표시');button.title=visible?'토큰 숨기기':'토큰 표시'}
$('#toggleHFToken').onclick=()=>setHFTokenVisible($('#hfToken').type==='password');
$('#hfSettingsForm').addEventListener('submit',()=>setHFTokenVisible(false));
let config={}, models=[], gateway={}, resourceSnapshot={processes:[],gpus:[]}, resourceBusy=false, toastTimer;const completedJobs=new Set();
let serviceKey='';
const api=async(url,options={})=>{const headers=new Headers(options.headers);if(serviceKey)headers.set('X-Managed-Llama-Key',serviceKey);const res=await fetch(url,{...options,headers});if(res.status===401){const dialog=$('#serviceLogin');if(!dialog.open)dialog.showModal()}if(res.status===204)return null;const data=await res.json().catch(()=>({}));if(!res.ok)throw new Error(data.error||`${res.status} ${res.statusText}`);return data};
$('#serviceLoginForm').onsubmit=async event=>{event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;serviceKey=$('#serviceKey').value.trim();$('#serviceKey').value='';try{await api('/api/state');$('#serviceLogin').close();$('#serviceLoginError').textContent='';await Promise.all([loadConfig(),loadState(),gatewayHealth(),status(),logs()])}catch(e){serviceKey='';$('#serviceLoginError').textContent=e.message}finally{button.disabled=false}};
const toast=(message,error=false)=>{const el=$('#toast');el.textContent=message;el.className=`show${error?' error':''}`;clearTimeout(toastTimer);toastTimer=setTimeout(()=>el.className='',3200)};
const bytes=n=>{if(!n)return '크기 미상';const u=['B','KB','MB','GB','TB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return `${n.toFixed(i>1?1:0)} ${u[i]}`};
const esc=s=>String(s).replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));
const processTime=value=>{const date=value?new Date(value):null;return date&&date.getFullYear()>1970?date.toLocaleString():'접근 불가'};

$$('.tab').forEach(btn=>btn.onclick=()=>{$$('.tab,.view').forEach(x=>x.classList.remove('active'));btn.classList.add('active');$(`#${btn.dataset.tab}`).classList.add('active');if(btn.dataset.tab==='explore'&&!$('#searchResults .card'))searchHF('');if(btn.dataset.tab==='resources')loadResources()});

async function loadConfig(){config=await api('/api/config');let startup={enabled:false,available:true};try{startup=await api('/api/autostart')}catch{startup.available=false}const f=$('#configForm');Object.entries(config).forEach(([k,v])=>{if(f.elements[k])f.elements[k].value=v});$('#hfSettingsForm').elements.hugging_face_base.value=config.hugging_face_base;f.elements.start_with_windows.checked=startup.enabled;f.elements.start_with_windows.disabled=!startup.available;await loadModels();await preview()}
async function saveConfig(){const f=$('#configForm'),fd=new FormData(f);const ints=['port','context_size','gpu_layers','threads','parallel','models_max'];const next=await api('/api/config');for(const [k,v]of fd){if(k==='start_with_windows')continue;next[k]=ints.includes(k)?Number(v):v}await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)});if(!f.elements.start_with_windows.disabled)await api('/api/autostart',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:f.elements.start_with_windows.checked})});config={...next,hugging_face_token:''};toast('설정을 저장했습니다');await Promise.all([preview(),loadState()])}
async function preview(){try{$('#commandPreview').textContent=(await api('/api/command')).command}catch(e){$('#commandPreview').textContent=e.message}}
$('#saveBtn').onclick=()=>saveConfig().catch(e=>toast(e.message,true));
$('#configForm').elements.start_with_windows.onchange=async function(){const requested=this.checked;this.disabled=true;toast('Windows 자동 실행 설정을 변경 중입니다. 관리자 승인 창을 확인하세요.');try{const result=await api('/api/autostart',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:requested})});this.checked=result.enabled;toast('Windows 자동 실행 설정을 변경했습니다')}catch(e){this.checked=!requested;toast(e.message,true)}finally{this.disabled=false}};
$('#hfSettingsForm').onsubmit=async function(event){event.preventDefault();const button=this.querySelector('[type=submit]');button.disabled=true;try{const next=await api('/api/config');next.hugging_face_base=this.elements.hugging_face_base.value;next.hugging_face_token=this.elements.hugging_face_token.value;await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)});config={...next,hugging_face_token:''};this.elements.hugging_face_token.value='';toast('연동 설정을 저장했습니다');await loadState()}catch(e){toast(e.message,true)}finally{button.disabled=false}};
$('#clearHFToken').onclick=async()=>{if(!confirm('저장된 Hugging Face 토큰을 제거할까요?'))return;try{await api('/api/config/hugging-face-token',{method:'DELETE'});$('#hfSettingsForm').elements.hugging_face_token.value='';toast('HF 토큰을 제거했습니다');await loadState()}catch(e){toast(e.message,true)}};

async function loadState(){try{gateway=await api('/api/state');const banner=$('#gatewayBanner');banner.classList.remove('hidden');banner.innerHTML=`<span class="kicker">${esc(gateway.phase)}</span><h2>${esc(gateway.message)}</h2>${!gateway.hf_enabled?`<p class="hint">${esc(gateway.hf_disabled_reason)} 연동 설정에서 토큰을 등록하세요.</p>`:''}`;$('#hfTab').disabled=!gateway.hf_enabled;$('#startBtn').disabled=!gateway.can_start;$('#hfSettingsForm').elements.hugging_face_token.placeholder=gateway.hf_enabled?'토큰 저장됨 · 변경할 때만 입력':'hf_...';$('#hfConnectionStatus').textContent=gateway.hf_enabled?'토큰 설정됨 · 검색 및 다운로드 사용 가능': '토큰 미설정 · 토큰을 저장하면 검색 및 다운로드를 사용할 수 있습니다.';return gateway}catch(e){toast(e.message,true)}}
async function gatewayHealth(){try{const h=await api('/health/gateway');$('#gatewayStatusDot').className=h.healthy?'ready':'';$('#gatewayStatusText').textContent=h.healthy?'Gateway 정상':'Gateway 오류';$('#gatewayStatusDetail').textContent=h.status}catch(e){$('#gatewayStatusDot').className='';$('#gatewayStatusText').textContent='Gateway 연결 오류';$('#gatewayStatusDetail').textContent=e.message}}
async function status(){try{const s=await api('/api/server/status');$('#llamaStatusDot').className=s.ready?'ready':s.running?'running':'';$('#llamaStatusText').textContent=s.ready?'Llama 준비됨':s.running?'Llama 로딩 중':'Llama 중지됨';$('#llamaStatusDetail').textContent=s.last_error|| (s.running?`PID ${s.pid}`:'llama.cpp 미실행');$('#pid').textContent=s.pid||'—';$('#endpoint').textContent=s.endpoint||'—';$('#proxyEndpoint').textContent=`${location.origin}/v1`;$('#startedAt').textContent=s.running&&s.started_at?new Date(s.started_at).toLocaleString():'—';$('#startBtn').disabled=s.running||!gateway.can_start;$('#stopBtn').disabled=!s.running;$('#restartBtn').disabled=!s.running}catch(e){$('#llamaStatusText').textContent='Llama 연결 오류'}}
async function action(name){try{await api(`/api/server/${name}`,{method:'POST'});toast(`서버 ${name} 요청을 처리했습니다`);setTimeout(()=>Promise.all([status(),loadState()]),250)}catch(e){toast(e.message,true);loadState()}}
$('#startBtn').onclick=()=>action('start');$('#stopBtn').onclick=()=>action('stop');$('#restartBtn').onclick=()=>action('restart');
async function logs(){try{const data=await api('/api/server/logs?tail=400');const el=$('#logs');const bottom=el.scrollHeight-el.scrollTop-el.clientHeight<40;el.textContent=data.lines.join('\n')||'아직 로그가 없습니다.';if(bottom)el.scrollTop=el.scrollHeight}catch{}}
$('#clearLogs').onclick=async()=>{try{await api('/api/server/logs',{method:'DELETE'});$('#logs').textContent='로그를 지웠습니다.'}catch(e){toast(e.message,true)}};

async function runtimeModels(reload=false){const root=$('#runtimeModels');try{const response=await api(`/llama/models${reload?'?reload=1':''}`);const list=response.data||[];if(!list.length){root.className='cards empty';root.textContent='router가 발견한 GGUF 모델이 없습니다.';return}root.className='cards';root.innerHTML=list.map(m=>{const state=m.status?.value||'unknown';const loaded=['loaded','loading','sleeping'].includes(state);return `<div class="card"><div><h3>${esc(m.id)}</h3><p><span class="tag">${esc(state)}</span>${m.path?esc(m.path):''}</p></div><button class="${loaded?'danger':'primary'}" data-runtime-model="${esc(m.id)}" data-runtime-action="${loaded?'unload':'load'}">${loaded?'Unload':'Load'}</button></div>`}).join('');root.querySelectorAll('[data-runtime-model]').forEach(b=>b.onclick=()=>runtimeAction(b.dataset.runtimeAction,b.dataset.runtimeModel))}catch(e){root.className='cards empty';root.textContent=e.message}}
async function runtimeAction(action,model){try{await api(`/llama/models/${action}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({model})});toast(`${model}: ${action} 요청 완료`);setTimeout(runtimeModels,300)}catch(e){toast(e.message,true)}}
$('#refreshRuntime').onclick=()=>runtimeModels(true);

async function loadModels(){models=await api('/api/models');const select=$('#configForm [name=selected_model]');const chosen=select.value||config.selected_model;select.innerHTML='<option value="">모델을 선택하세요</option>'+models.map(m=>`<option value="${esc(m.path)}">${esc(m.path)}</option>`).join('');select.value=chosen;renderModels()}
function renderModels(){const root=$('#modelList');if(!models.length){root.className='cards empty';root.textContent='모델 디렉터리가 비어 있습니다.';return}root.className='cards';root.innerHTML=models.map(m=>`<div class="card"><div><h3>${esc(m.path)}</h3><p><span class="tag">${m.valid?`GGUF v${m.gguf_version}`:'검증 실패'}</span>${bytes(m.size)} · ${new Date(m.modified).toLocaleString()}</p></div><div class="actions"><button data-select="${esc(m.path)}">기본 모델</button><button class="danger" data-delete="${esc(m.path)}">삭제</button></div></div>`).join('');root.querySelectorAll('[data-select]').forEach(b=>b.onclick=async()=>{$('#configForm [name=selected_model]').value=b.dataset.select;await saveConfig().catch(e=>toast(e.message,true));$$('.tab')[0].click()});root.querySelectorAll('[data-delete]').forEach(b=>b.onclick=async()=>{if(!confirm(`${b.dataset.delete} 파일을 삭제할까요?`))return;try{await api(`/api/models/${b.dataset.delete.split('/').map(encodeURIComponent).join('/')}`,{method:'DELETE'});toast('모델을 삭제했습니다');await Promise.all([loadModels(),loadState()])}catch(e){toast(e.message,true)}})}
$('#refreshModels').onclick=()=>loadModels().catch(e=>toast(e.message,true));
$('#uploadInput').onchange=()=>{const file=$('#uploadInput').files[0];if(!file)return;const xhr=new XMLHttpRequest(),fd=new FormData();fd.append('file',file);const p=$('#uploadProgress');p.classList.remove('hidden');xhr.upload.onprogress=e=>{if(e.lengthComputable){p.querySelector('span').style.width=`${e.loaded/e.total*100}%`;p.querySelector('small').textContent=`업로드 중 ${Math.round(e.loaded/e.total*100)}%`}};xhr.onload=async()=>{p.classList.add('hidden');if(xhr.status>=200&&xhr.status<300){toast('GGUF를 추가했습니다');await loadModels()}else{try{toast(JSON.parse(xhr.responseText).error,true)}catch{toast('업로드 실패',true)}}};xhr.onerror=()=>toast('업로드 실패',true);xhr.open('POST','/api/models/upload');xhr.send(fd)};

async function loadResources(){
  if(resourceBusy)return;resourceBusy=true;
  try{resourceSnapshot=await api('/api/resources/snapshot');renderGPU();renderProcesses();await loadResourceEvents();$('#resourceCapturedAt').textContent=`${new Date(resourceSnapshot.captured_at).toLocaleString()} 기준`;if(resourceSnapshot.process_error)toast(resourceSnapshot.process_error,true)}
  catch(e){toast(e.message,true)}finally{resourceBusy=false}
}
function renderGPU(){
  const root=$('#gpuSummary'),notice=$('#nvidiaNotice');notice.textContent=resourceSnapshot.nvidia_error||'';
  if(!resourceSnapshot.nvidia_available||!resourceSnapshot.gpus?.length){root.className='gpu-grid empty';root.textContent=resourceSnapshot.nvidia_error||'NVIDIA GPU를 찾지 못했습니다.';return}
  root.className='gpu-grid';root.innerHTML=resourceSnapshot.gpus.map(g=>{const pct=g.memory_total_mib?Math.min(100,g.memory_used_mib/g.memory_total_mib*100):0;return `<div class="gpu-card"><h3>GPU ${g.index} · ${esc(g.name)}</h3><div class="meter"><span style="width:${pct.toFixed(1)}%"></span></div><p><span>VRAM</span><b>${Number(g.memory_used_mib).toLocaleString()} / ${Number(g.memory_total_mib).toLocaleString()} MiB</b></p></div>`}).join('')
}
function renderProcesses(){
  const query=$('#processSearch').value.trim().toLowerCase(),gpuOnly=$('#gpuOnly').checked,killableOnly=$('#killableOnly').checked;
  const list=(resourceSnapshot.processes||[]).filter(p=>{const matches=!query||`${p.pid} ${p.name} ${p.executable||''}`.toLowerCase().includes(query);return matches&&(!gpuOnly||(p.gpu&&p.gpu.length))&&(!killableOnly||p.can_terminate)});
  const root=$('#processRows');if(!list.length){root.innerHTML='<tr><td colspan="5" class="empty-cell">조건에 맞는 프로세스가 없습니다.</td></tr>';return}
  root.innerHTML=list.map(p=>{const label=p.managed_by_llama?'<span class="managed">Managed Llama</span>':p.protected?'<span class="protected">보호됨</span>':'';const gpu=(p.gpu||[]).map(g=>`GPU ${g.gpu_index<0?'?':g.gpu_index} · ${g.used_vram_mib==null?'N/A':`${Number(g.used_vram_mib).toLocaleString()} MiB`}`).join('<br>')||'—';const actions=p.can_terminate?`<div class="actions"><button class="danger" data-kill="${p.pid}">종료</button><button class="danger" data-kill-tree="${p.pid}">트리 종료</button></div>`:label||'—';return `<tr><td><strong>${esc(p.name)}</strong><code title="${esc(p.executable||'')}">${esc(p.executable||'경로 접근 불가')}</code></td><td>${p.pid}<br><span class="hint">부모 ${p.parent_pid||'—'}</span></td><td>${processTime(p.started_at)}</td><td>${gpu}</td><td>${actions}</td></tr>`}).join('');
  root.querySelectorAll('[data-kill]').forEach(b=>b.onclick=()=>terminateResource(Number(b.dataset.kill),false));root.querySelectorAll('[data-kill-tree]').forEach(b=>b.onclick=()=>terminateResource(Number(b.dataset.killTree),true))
}
async function terminateResource(pid,tree){
  const p=(resourceSnapshot.processes||[]).find(x=>x.pid===pid);if(!p)return toast('프로세스 정보를 새로고침하세요.',true);
  const gpu=p.total_vram_mib?`\nVRAM: ${Number(p.total_vram_mib).toLocaleString()} MiB`:'';const warning=tree?'프로세스와 모든 자식 프로세스를 강제로 종료합니다.':'해당 프로세스를 강제로 종료합니다.';
  if(!confirm(`${p.name}을(를) 종료하시겠습니까?\n\nPID: ${p.pid}\n경로: ${p.executable||'접근 불가'}${gpu}\n\n${warning}`))return;
  try{await api(`/api/resources/processes/${pid}/terminate`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({started_at:p.started_at,tree})});toast(tree?'프로세스 트리를 종료했습니다.':'프로세스를 종료했습니다.');setTimeout(loadResources,350)}catch(e){toast(e.message,true);loadResources()}
}
async function loadResourceEvents(){
  const data=await api('/api/resources/events'),root=$('#resourceEvents'),events=data.events||[];if(!events.length){root.className='cards empty';root.textContent='아직 종료 이력이 없습니다.';return}root.className='cards';root.innerHTML=events.map(e=>`<div class="card"><div><h3>${esc(e.name)} · PID ${e.pid}</h3><p><span class="tag">${e.success?'성공':'실패'}</span>${new Date(e.at).toLocaleString()} · ${esc(e.message)}${e.tree?' · 트리 종료':''}</p></div></div>`).join('')
}
$('#refreshResources').onclick=loadResources;$('#processSearch').oninput=renderProcesses;$('#gpuOnly').onchange=renderProcesses;$('#killableOnly').onchange=renderProcesses;

async function searchHF(q){const root=$('#searchResults');if(!gateway.hf_enabled){root.className='cards empty';root.textContent=gateway.hf_disabled_reason||'Hugging Face 기능이 비활성화되었습니다.';return}root.className='cards empty';root.textContent='Hugging Face에서 검색 중…';try{const list=await api(`/api/hf/search?q=${encodeURIComponent(q)}&limit=20`);if(!list.length){root.textContent='검색 결과가 없습니다.';return}root.className='cards';root.innerHTML=list.map(m=>`<div class="card"><div><h3>${esc(m.id)}</h3><p>↓ ${Number(m.downloads||0).toLocaleString()} · ♥ ${Number(m.likes||0).toLocaleString()}</p></div><button data-repo="${esc(m.id)}">GGUF 보기</button><div class="files hidden"></div></div>`).join('');root.querySelectorAll('[data-repo]').forEach(b=>b.onclick=()=>loadFiles(b))}catch(e){root.textContent=e.message;toast(e.message,true)}}
async function loadFiles(btn){
 const area=btn.parentElement.querySelector('.files');
 if(!area.classList.contains('hidden')){area.classList.add('hidden');btn.textContent='GGUF 보기';btn.setAttribute('aria-expanded','false');return}
 area.classList.remove('hidden');btn.textContent='닫기';btn.setAttribute('aria-expanded','true');
 if(area.dataset.loaded==='true'||area.dataset.loading==='true')return;
 area.dataset.loading='true';area.textContent='파일 확인 중…';
 try{const files=await api(`/api/hf/files?repo=${encodeURIComponent(btn.dataset.repo)}`);
 area.innerHTML=files.length?files.map(f=>`<div class="file"><span>${esc(f.name)} · ${bytes(f.size)}</span><button data-file="${esc(f.name)}">다운로드</button></div>`).join(''):'GGUF 파일이 없습니다.';
 area.dataset.loaded='true';
 area.querySelectorAll('[data-file]').forEach(b=>b.onclick=async()=>{b.disabled=true;try{await startDownload(btn.dataset.repo,b.dataset.file)}finally{b.disabled=false}});
 }catch(e){area.textContent=e.message}finally{delete area.dataset.loading}
}
async function startDownload(repo,file){try{await api('/api/hf/downloads',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({repo,file})});toast('다운로드를 시작했습니다 · 다운로드 탭에서 확인하세요');await downloads()}catch(e){toast(e.message,true)}}
let downloadJobs=[],downloadsBusy=false;
function renderDownloads(){
 const active=downloadJobs.filter(j=>j.state==='queued'||j.state==='downloading').length;
 const completed=downloadJobs.filter(j=>j.state==='completed').length,failed=downloadJobs.filter(j=>j.state==='failed').length;
 $('#downloadsTab').textContent=active?`다운로드 (${active})`:'다운로드';
 $('#downloadSummary').textContent=`진행 중 ${active} · 완료 ${completed} · 실패 ${failed}`;
 const filter=$('#downloadFilter').value,jobs=downloadJobs.filter(j=>filter==='all'||(filter==='active'?(j.state==='queued'||j.state==='downloading'):j.state===filter));
 const root=$('#downloadJobs');
 root.className=jobs.length?'cards':'cards empty';
 if(!jobs.length){root.textContent=downloadJobs.length?'해당 상태의 다운로드가 없습니다.':'아직 다운로드 내역이 없습니다.';return}
 root.innerHTML=jobs.map(j=>{
 const pct=j.total>0?Math.max(0,Math.min(100,Math.floor(j.downloaded/j.total*100))):null;
 const label={queued:'대기 중',downloading:'다운로드 중',completed:'완료',failed:'실패'}[j.state]||j.state;
 return `<div class="job"><h3>${esc(j.file)}</h3><p>${esc(j.repo)} · ${esc(label)}</p><p>${j.downloaded?bytes(j.downloaded):'0 B'} / ${bytes(j.total)}${pct===null?'':` · ${pct}%`}</p><progress max="100"${pct===null?'':` value="${pct}"`} aria-label="${esc(j.file)} 진행률"></progress><p>시작: ${esc(new Date(j.started_at).toLocaleString())}</p><p>저장 위치: ${esc(j.destination)}</p>${j.error?`<p class="danger">${esc(j.error)}</p>`:''}</div>`;
 }).join('');
}
async function downloads(){if(downloadsBusy)return;downloadsBusy=true;try{downloadJobs=(await api('/api/hf/downloads'))||[];$('#downloadError').textContent='';renderDownloads();const fresh=downloadJobs.filter(j=>j.state==='completed'&&!completedJobs.has(j.id));fresh.forEach(j=>completedJobs.add(j.id));if(fresh.length){await loadModels();await runtimeModels(true)}}catch(e){$('#downloadError').textContent='다운로드 상태를 확인하지 못했습니다: '+e.message}finally{downloadsBusy=false}}
$('#refreshDownloads').onclick=downloads;
$('#downloadFilter').onchange=renderDownloads;
$('#downloadsTab').addEventListener('click',downloads);
$('#searchForm').onsubmit=e=>{e.preventDefault();searchHF($('#searchQuery').value.trim())};

let appUpdate={},appUpdateBusy=false,appUpdateChecked=false;
function renderAppUpdate(state){
 appUpdate=state;
 const busy=['checking','downloading','restarting'].includes(state.phase);
 $('#appVersion').textContent=state.current==='dev'?'개발 빌드':state.current;
 $('#appLatestVersion').textContent=state.latest?` · 최신 버전 ${state.latest}`:'';
 if(document.activeElement!==$('#appUpdateSource'))$('#appUpdateSource').value=state.repository||'https://github.com/OWNER/managed-llama';
 $('#checkAppUpdate').disabled=busy||!state.configured;
 $('#appUpdateSourceForm').querySelector('button').disabled=busy;
 $('#installAppUpdate').classList.toggle('hidden',!state.available);
 $('#installAppUpdate').disabled=busy;
 $('#appUpdateNotice').classList.toggle('hidden',!state.available||busy);
 $('#appUpdateNotice').textContent=`새 버전 ${state.latest||''} · 업데이트`;
 const progress=$('#appUpdateProgress');progress.classList.toggle('hidden',state.phase!=='downloading');progress.value=state.total?Math.min(100,100*state.downloaded/state.total):0;
 let message=state.message||'';
 if(!state.configured)message='예시 URL이 설정되어 있습니다. 실제 배포 주소를 등록하면 새 버전을 확인할 수 있습니다.';
 else if(state.phase==='downloading')message=`새 버전 다운로드 중 · ${bytes(state.downloaded)} / ${bytes(state.total)}`;
 else if(state.phase==='checking')message='새 버전을 확인하고 있습니다.';
 else if(state.phase==='restarting')message='업데이트 및 재시작 중입니다. 잠시 후 트레이에서 대시보드를 다시 열어주세요.';
 else if(!message)message=state.available?'새 버전이 있습니다. 업데이트 및 재시작을 눌러 적용하세요.':state.latest?'현재 최신 버전입니다.':'새 버전 확인을 눌러주세요.';
 $('#appUpdateStatus').textContent=message;
}
async function loadAppUpdate(check=false){
 if(appUpdateBusy)return;appUpdateBusy=true;
 try{
  let state=await api('/api/updates');
  if(check||(!appUpdateChecked&&state.configured&&!['checking','downloading','restarting'].includes(state.phase))){appUpdateChecked=true;state=await api('/api/updates/check',{method:'POST'})}
  renderAppUpdate(state);
 }catch(e){if(appUpdate.phase!=='restarting')$('#appUpdateStatus').textContent=e.message}finally{appUpdateBusy=false}
}
$('#checkAppUpdate').onclick=()=>loadAppUpdate(true);
$('#appUpdateNotice').onclick=()=>document.querySelector('[data-tab="settings"]').click();
$('#appUpdateSourceForm').onsubmit=async event=>{
 event.preventDefault();const button=event.currentTarget.querySelector('button');button.disabled=true;
 try{const next=await api('/api/config');next.update_repository=$('#appUpdateSource').value.trim();await api('/api/config',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(next)});appUpdateChecked=false;await loadAppUpdate()}catch(e){$('#appUpdateStatus').textContent=e.message}finally{button.disabled=false}
};
$('#installAppUpdate').onclick=async()=>{
 if(!appUpdate.available||!confirm(`${appUpdate.latest}로 업데이트하고 다시 시작할까요?\n실행 중인 llama-server는 중지되며 설정과 모델은 유지됩니다.`))return;
 const button=$('#installAppUpdate');button.disabled=true;
 try{renderAppUpdate(await api('/api/updates/install',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:appUpdate.latest})}))}catch(e){$('#appUpdateStatus').textContent=e.message;button.disabled=false}
};
loadAppUpdate();setInterval(()=>loadAppUpdate(),2000);setInterval(()=>{appUpdateChecked=false},30*60*1000);

Promise.all([loadConfig(),loadState(),gatewayHealth(),status(),logs()]).catch(e=>toast(e.message,true));setInterval(()=>Promise.all([gatewayHealth(),status(),loadState()]),2000);setInterval(logs,1500);setInterval(downloads,1500);setInterval(runtimeModels,3000);setInterval(()=>{if($('#resources').classList.contains('active'))loadResources()},5000);
