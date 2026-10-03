# Managed Llama

Windows에서 `llama-server` router를 로컬 자식 프로세스로 실행하고 GGUF 모델을 관리하는 Go 기반 대시보드입니다. Docker나 별도 데이터베이스 없이 하나의 실행 파일로 동작합니다.

소스 실행 시 `llama-server` 실행 파일과 GGUF 모델을 별도로 준비해야 합니다. 자동 설치 패키지는 빌드 시 지정한 llama.cpp 런타임을 포함하며, 모델은 설치 후 추가합니다. 현재 Windows 전용이며 macOS와 Linux는 지원하지 않습니다.

> **실행 권한:** 일반 사용자 모드는 개인용 로컬 실행을 위한 것으로 관리 API에 인증이 없습니다. 관리자 권한 실행과 Windows 서비스 모드는 관리 키 인증 및 설치 경로 권한 검사를 적용합니다. 관리 키 보유자는 높은 권한으로 프로그램 실행과 프로세스 종료를 요청할 수 있으므로 키를 공유하지 마세요. 모든 관리 포트는 로컬 전용으로 사용하세요.

게이트웨이는 일반 사용자 모드에서도 loopback 주소에만 바인드합니다.
HTTP Host는 `localhost` 또는 loopback IP와 포트만 허용합니다. 임의 도메인,
`0.0.0.0`, 외부 IP를 통한 관리 API 공개는 지원하지 않습니다.

## 기능

- `llama-server` router 시작, 중지, 재시작 및 PID/health 상태 확인
- Windows 실행 프로세스 목록과 실행 경로·부모 PID·시작 시각 확인
- `nvidia-smi`를 이용한 GPU별 VRAM 및 점유 프로세스 확인
- 프로세스 시작 시각을 재검증하는 안전한 단일/트리 수동 종료와 최근 종료 이력
- Windows 시스템 트레이에서 대시보드 열기, 상태 확인, 서버 시작·중지 및 앱 종료
- 설정 화면에서 Windows 서비스 자동 시작과 현재 사용자의 로그인 트레이 등록·해제
- router 모델별 load/unload 및 Windows 프로세스 트리 전체 unload
- OpenAI 호환 `/v1/*` 전체를 SSE 스트리밍을 유지하며 투명 프록시
- llama.cpp의 models, health, slots, metrics, props API를 `/llama/*`로 프록시
- stdout/stderr 실시간 로그 버퍼
- 실행 파일 경로와 시작 인수 템플릿 편집 및 명령 미리보기
- 로컬 GGUF 재검색, 헤더 검증, 업로드, 선택, 삭제
- Hugging Face의 GGUF 모델 검색과 저장소별 파일 탐색
- 진행률이 표시되는 비동기 GGUF 다운로드
- `config.json`에 저장한 Hugging Face 토큰을 이용한 gated/private 저장소 접근

## 요구 사항

- Windows
- Go 1.25.1 이상: 소스 실행 및 빌드에 필요
- `--models-dir`와 모델 load/unload API를 지원하는 `llama-server` 바이너리
- 실행할 GGUF 모델과 모델에 필요한 메모리
- NVIDIA GPU 정보를 표시하려면 `nvidia-smi`를 사용할 수 있는 드라이버 환경
- Hugging Face 연동을 사용하려면 접근 권한이 있는 토큰

[llama.cpp 릴리스](https://github.com/ggml-org/llama.cpp/releases)에서 Windows와 사용할 CPU/GPU 환경에 맞는 바이너리를 준비하세요. Managed Llama는 기본적으로 router에 `127.0.0.1:8080`을 사용하도록 설정합니다. 지원 API와 인수는 준비한 llama.cpp 버전에 따라 달라질 수 있습니다.

## 원클릭 자동 설치

`ManagedLlamaSetup-<버전>-windows-amd64.exe`를 일반 사용자 세션에서 실행합니다.
Windows 관리자 권한 확인(UAC)을 승인하면 경로 선택이나 다음 버튼 없이 설치 진행
화면이 표시되고, 완료 후 원래 사용자 권한으로 트레이가 실행됩니다.

Setup은 시작 시 고정 AppId에 해당하는 Windows 설치 등록 정보
(`HKLM`의 64비트 Uninstall 키)를 확인합니다. 등록이 없으면 **신규 설치**,
있으면 **업데이트 / 재설치**로 구분하여 진행 화면과 설치 로그에 표시합니다.
`config.json`이나 모델 폴더의 존재 여부는 이 구분에 사용하지 않습니다.
제거 후 등록 정보가 삭제되었다면 다음 실행은 신규 설치로 분류되며,
보존된 설정·모델은 그대로 유지합니다. 과거 설치 이력을 영구 추적하는 방식은 아닙니다.

| 항목 | 위치 / 동작 |
|---|---|
| 프로그램 | `C:\Program Files\ManagedLlama\managed-llama.exe` |
| 설정 | 설치 폴더의 `config.json`; 없을 때만 생성 |
| llama.cpp | 설치 폴더의 `runtime\llama-server.exe` 및 DLL |
| 모델 | 설치 폴더의 `models\`; 기본적으로 모델을 포함하지 않음 |
| 관리 키 | 설치 폴더의 `config.json.service-key`; Administrators/SYSTEM만 접근 |
| 부팅 시 | `ManagedLlama` 서비스 자동 시작; 로그인 전에도 관리 게이트웨이 실행 |
| 로그인 시 | 모든 사용자 시작프로그램 바로가기로 트레이 실행 |
| 제거 | Windows 설치된 앱에서 제거; 설정·키·모델은 보존 |

트레이에서 대시보드를 열고 기존 서비스 관리자 인증 절차로 관리 키를 입력합니다.
설치 프로그램은 키를 브라우저나 실행 인수로 전달하지 않습니다. 모델을 추가하고
명시적으로 서버를 시작해야 추론이 실행됩니다.

일반 권한 로그인 트레이는 보호된 관리 키를 읽지 않습니다. 서비스가 설치되어 있으면
`대시보드에서 인증 필요`를 표시하고 트레이의 시작·중지 메뉴를 비활성화합니다.
`대시보드 열기`에서 인증한 뒤 상태 조회와 서버 제어를 수행하세요. 이 상태는
서비스 연결 장애와 구분됩니다.

설치판에서 자동 실행을 해제하면 서비스는 제거되지 않고 **수동 시작**으로 변경됩니다.
현재 실행 중인 서비스는 계속 설정 저장·모델 관리를 담당하며, 다음 부팅에는
서비스와 로그인 트레이가 자동으로 시작되지 않습니다. 이후 시작 메뉴에서 앱을
열 때 서비스가 중지되어 있으면 UAC 승인 후 서비스를 시작합니다. 다시 자동 실행을
켜면 지연된 자동 시작으로 복원합니다. 설치판은 `installed-by-setup` 표시 파일을
사용해 중복된 사용자별 Run 레지스트리 등록을 만들지 않습니다.

트레이는 Windows 서비스에 등록된 설정 경로와 관리 API 주소를 읽습니다. 기존 서비스가
`-listen 127.0.0.1:4040`처럼 다른 포트를 사용해도 재설치 후 같은 주소로 연결하며,
서비스가 등록된 동안에는 트레이 실행 인수보다 등록된 경로·주소가 우선합니다.
다른 실행 파일의 서비스이거나 외부 공개 주소이면 연결을 거부합니다.

새 버전 Setup을 실행하면 기존 설정을 덮어쓰지 않고 프로그램과 포함 런타임을
교체합니다. 실행 중인 앱을 닫으라는 안내가 나올 수 있습니다. 기존 서비스가 다른
경로를 사용하거나 설치 폴더 권한이 안전하지 않으면 파일 복사 전에 중단합니다.
서비스 시작 실패 시 오류와 비정상 종료 코드를 반환하며 트레이는 실행하지 않습니다.
파일 설치 이후의 서비스 구성 실패는 전체 파일 롤백이 아닌 Setup 재실행으로
복구하는 방식입니다. 기존 실행 중 서비스는 취소·실패 시 재시작을 시도합니다.

### 설치 파일 빌드

Go와 [Inno Setup 6.3 이상](https://jrsoftware.org/isinfo.php)이 필요합니다.
배포할 x64 llama.cpp 런타임과 라이선스를 명시적으로 지정합니다.

```powershell
.\scripts\build-installer.ps1 `
  -RuntimeDir C:\release\llama-cpp-win-x64 `
  -RuntimeLicense C:\release\llama-cpp-win-x64\LICENSE `
  -Version 0.1.0 `
  -ISCC "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
```

결과는 `dist\ManagedLlamaSetup-0.1.0-windows-amd64.exe`입니다. 빌드는 앱과
지정 폴더의 `llama-server.exe`, DLL, 라이선스만 새 스테이징 폴더에 복사합니다.
저장소의 개인 설정·토큰·키·모델은 포함하지 않습니다. `licenses` 폴더가 있으면 함께
포함합니다. 런타임에 필요한 VC++/CUDA 재배포 DLL과 라이선스는 배포자가 준비해야
하며, 빌드 스크립트가 드라이버나 런타임을 인터넷에서 자동으로 받지는 않습니다.
서명 인증서는 포함되어 있지 않으므로 생성된 설치 파일은 서명되지 않은 빌드입니다.

실제 배포 전 별도의 Windows 테스트 환경에서 최초 설치, 재설치, 업그레이드,
서비스 등록/로그인 시작, 취소·실패 복구, 제거 후 데이터 보존을 검증하세요.
컴파일과 단위 테스트만으로 실제 관리자 설치 검증이 완료되는 것은 아닙니다.

## 소스 빠른 시작

저장소를 내려받은 뒤 프로젝트 루트에서 PowerShell을 엽니다.

```powershell
$env:LLAMA_SERVER_PATH = "C:\tools\llama.cpp\llama-server.exe"
go run . -config .\config.json
```

1. 시스템 트레이의 Managed Llama 아이콘에서 **대시보드 열기**를 선택합니다. 관리 API에는 <http://127.0.0.1:3030>으로 직접 접속할 수도 있습니다.
2. 로컬 GGUF 파일을 업로드하거나 기본 `models/` 폴더에 넣고 재검색합니다. Hugging Face에서 받으려면 먼저 설정에 토큰을 저장합니다.
3. 사용할 기본 모델을 선택합니다.
4. 서버를 시작하고 상태가 `running`으로 바뀌는지 확인합니다.

첫 실행 시 `config.json`이 생성됩니다. `-config`를 생략하면 현재 작업 폴더가 아닌 실행 파일 옆을 사용합니다. `go run`은 임시 실행 파일을 만들므로 위처럼 설정 경로를 명시하세요. 실행 파일 경로는 대시보드 설정에서도 변경할 수 있지만, `LLAMA_SERVER_PATH`를 지정한 경우 환경변수 값이 우선합니다. 앱을 실행하는 것만으로 llama-server가 시작되지는 않습니다.

일반 실행에는 관리자 권한이 필요하지 않습니다. 설정 예시는 [`config.example.json`](config.example.json)을 참고하세요. 관리자 권한으로 실행하려면 아래 서비스 설치 조건을 먼저 충족해야 합니다.

### 실행 파일 빌드

```powershell
go build -o managed-llama.exe .
.\managed-llama.exe -listen 127.0.0.1:3030 -config config.json
```

콘솔 창 없이 시스템 트레이 앱으로 빌드하려면 다음 명령을 사용합니다.

```powershell
go build -ldflags "-H=windowsgui" -o managed-llama.exe .
```

실행 파일에는 대시보드 웹 리소스가 포함됩니다. 실행할 때 Go나 별도의 웹 서버는 필요하지 않지만, `llama-server`와 모델 파일은 계속 필요합니다.

## 게이트웨이 상태

관리 API 기본 주소는 `127.0.0.1:3030`, llama-server 기본 주소는
`127.0.0.1:8080`입니다. 관리 API 또는 트레이 대시보드 바인드가 실패하면
일반 앱 실행에서는 Windows 오류 대화상자로 주소와 원인을 안내합니다.
포트 사용 중 오류는 기존 앱/점유 프로그램과 `-listen` 설정을, 접근 거부는
Windows 포트 예약·보안 정책을 확인하도록 안내하며 다른 포트로 자동 변경하지 않습니다.
서비스는 대화상자 대신 Windows 응용 프로그램 이벤트 로그에 오류를 남깁니다.
Setup은 서비스 시작 전 포트 점유를 확인하고 내부 작업 오류를 설치 화면과
Setup 로그에 표시합니다. 사전 확인 이후에도 포트가 점유될 수 있으므로 실제
서비스 시작 오류의 세부 정보는 이벤트 로그 확인이 필요할 수 있습니다.
llama-server 자체의 오류는 기존 대시보드 상태와 서버 로그에서 확인합니다.

관리 게이트웨이는 항상 먼저 실행되며 아래 조건이 충족되기 전에는 `llama-server`를 시작하지 않습니다.

| 상태 | 동작 |
|---|---|
| `setup_required` | `LLAMA_SERVER_PATH` 실행 파일을 찾을 수 없음 |
| `empty_library` | 유효한 GGUF가 없어 게이트웨이만 실행 |
| `default_model_required` | GGUF는 있지만 기본 모델이 선택되지 않음 |
| `invalid_default_model` | 기본 모델이 삭제됐거나 GGUF 검증 실패 |
| `ready` | 사용자가 llama router를 시작할 수 있음 |
| `starting` | router 시작 또는 모델 로딩 중 |
| `running` | OpenAI 프록시 요청 처리 가능 |
| `error` | 마지막 llama-server 실행 실패. 조건을 수정한 후 재시도 가능 |

`GET /api/state`에서 현재 상태, 시작 가능 여부, GGUF 수와 Hugging Face 활성화 여부를 확인할 수 있습니다. router 시작 시 선택한 기본 모델을 자동으로 Load합니다. 실행 조건이 충족되지 않으면 `/v1/*`는 상태 원인이 포함된 OpenAI 형식의 `503`을 반환합니다.

게이트웨이와 llama-server health는 서로 독립적으로 확인할 수 있습니다.

| Endpoint | 의미 |
|---|---|
| `GET /health/gateway` | 관리 게이트웨이가 응답하면 `200`과 `healthy: true` 반환 |
| `GET /health/llama` | llama-server가 `/health`에서 `200`을 반환할 때만 `200`; 중지·로딩·오류는 `503` |
| `GET /llama/health` | llama.cpp의 원본 `/health` 응답을 그대로 프록시 |

## 트레이와 Windows 서비스

실행 후 알림 영역의 Managed Llama 아이콘을 우클릭하면 대시보드 열기,
`llama-server` 시작·중지, 앱 종료 메뉴를 사용할 수 있습니다. 앱 종료는 실행 중인
router와 모델 worker 프로세스 트리까지 정리합니다.

### Windows 서비스

서비스는 로그인 전에도 게이트웨이를 실행합니다. `LocalSystem` 권한을 보호하기 위해 다음 설치 조건을 적용합니다.

- 앱 실행 파일과 설정 파일은 같은 관리자 소유의 전용 설치 폴더에 둡니다. 예: `C:\Program Files\ManagedLlama`.
- `server_path`는 해당 폴더 안의 `llama-server.exe`를 가리키는 **절대 경로**여야 합니다. 함께 배포된 DLL도 설치 폴더 안에 둡니다.
- 모델 폴더도 설치 폴더 안에 둡니다. 설치 폴더 전체의 파일과 상위 경로에 일반 사용자의 수정·교체 권한이 있으면 거부합니다.
- 네트워크 경로, 심볼릭 링크와 junction 등 reparse point는 허용하지 않습니다. 파일 소유자는 Administrators, SYSTEM 또는 TrustedInstaller여야 합니다.
- 설치 시 `config.json.service-key`를 생성하며, 파일 생성 시점부터 Administrators와 SYSTEM만 접근할 수 있는 ACL을 적용합니다. 기존 키 파일의 권한이 안전하지 않아도 실행을 거부합니다.

저장소 작업 폴더나 사용자 다운로드 폴더에서 직접 서비스를 등록하지 마세요. 기존 폴더의 ACL을 자동 변경하지 않으므로, 보호된 전용 설치 폴더를 준비해야 합니다.

관리자 PowerShell에서 설치 폴더를 만들고 빌드한 앱, llama.cpp 바이너리와 DLL, 모델, 설정 예시를 복사합니다. 경로는 실제 준비한 파일에 맞게 변경하세요.

```powershell
$installDir = Join-Path $env:ProgramFiles 'ManagedLlama'
New-Item -ItemType Directory -Path $installDir -ErrorAction Stop
Copy-Item .\managed-llama.exe -Destination $installDir
Copy-Item .\config.example.json -Destination (Join-Path $installDir 'config.json')
# llama-server.exe와 함께 제공된 DLL을 $installDir 아래에 복사하세요.
New-Item -ItemType Directory -Path (Join-Path $installDir 'models')
# GGUF 파일을 models 폴더에 복사하세요.

$configFile = Join-Path $installDir 'config.json'
$settings = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json
$settings.server_path = Join-Path $installDir 'llama-server.exe'
[System.IO.File]::WriteAllText($configFile, ($settings | ConvertTo-Json), [System.Text.UTF8Encoding]::new($false))
```

Setup을 사용하지 않은 일반 실행은 사용자 모드에서 관리 API와 트레이를 시작합니다. 트레이의 **대시보드
열기**로 접속한 뒤 **Windows 시작 시 자동 실행**을 체크하면 UAC 승인 후 서비스를
등록하고 사용자 게이트웨이에서 서비스로 전환합니다. 체크를 해제하면 서비스를
중지·제거하고 사용자 게이트웨이로 돌아옵니다. 전환 중 실행 중인 llama는 중지됩니다.
로그인 시작 항목은 승인한 관리자 계정이 아닌 현재 트레이 사용자의 계정에 등록합니다.
Setup 설치판은 위의 원클릭 설치 절에 설명한 대로 서비스를 유지하고 부팅 시작 유형만 변경합니다.
트레이 대시보드는 임시 loopback 포트에서 서비스 API를 중계하므로 전환 중에도 같은
페이지를 사용할 수 있습니다. `3030` 관리 API를 직접 연 경우에는 자동 실행 설정이
비활성화되므로 트레이에서 대시보드를 열어 변경하세요.

WOL 부팅 후 사용자 로그인 전에도 llama-server를 시작하려면 관리자 PowerShell에서
빌드된 실행 파일로 서비스를 등록합니다. `go run`으로 만든 임시 실행 파일은 등록하지
마세요. `-config`에는 일반 실행으로 미리 생성하고 설정한 파일의 절대 경로를 지정합니다.

```powershell
& "$installDir\managed-llama.exe" -service install -config "$installDir\config.json"
& "$installDir\managed-llama.exe" -service start
& "$installDir\managed-llama.exe" -service status
```

서비스는 `LocalSystem` 권한의 지연된 자동 시작으로 등록되며 관리 API 게이트웨이만
실행합니다. llama-server는 자동으로 시작하지 않으며 대시보드 또는
`POST /api/server/start` 요청으로 명시적으로 시작합니다. 서비스 자체가 실패하면
Windows 서비스 관리자가 1분, 2분, 5분 간격으로 재시작합니다. 서비스 로그는
Windows 이벤트 뷰어의 `ManagedLlama` 소스에서 확인할 수 있습니다.

서비스를 설치하면 현재 사용자의 로그인 자동 시작 항목도 함께 등록됩니다. 로그인
후 같은 실행 파일이 서비스 등록을 감지하면 두 번째 게이트웨이를 만들지 않고 트레이를
표시합니다. 서비스 모드의 대시보드에는 관리 키를 입력해야 합니다. 관리 키는 브라우저
페이지의 메모리에서만 유지하며 새로고침 후 다시 입력합니다. 트레이의 시작·중지와 상태
조회는 관리자 권한으로 실행한 트레이에서 사용할 수 있습니다. 이때 `트레이 아이콘 종료`는
아이콘만 닫으며 백그라운드 서비스와 llama-server는 계속 실행됩니다.

서비스 모드의 프로세스 종료 API는 높은 권한으로 동작하므로 대시보드는 반드시
loopback 주소에만 바인딩됩니다. 원격 데스크톱으로 접속한 뒤 로컬 대시보드를
사용하는 방식은 지원하지만 LAN 전체 주소로 서비스 등록하는 것은 거부합니다.

서비스를 제거할 때는 먼저 중지합니다. 제거 시 로그인 트레이 시작 항목도 정리됩니다.

```powershell
.\managed-llama.exe -service stop
.\managed-llama.exe -service uninstall
```

관리 키는 관리자 PowerShell에서 확인합니다. 명령 결과를 로그, 스크린샷이나 이슈에 첨부하지 마세요.

```powershell
Get-Content -LiteralPath "$installDir\config.json.service-key"
```

서비스의 모든 API(`/api/*`, `/health/*`, `/v1*`, `/llama/*`)는 `X-Managed-Llama-Key` 헤더를 요구합니다. 대시보드 정적 파일만 인증 없이 제공됩니다. 인증 헤더는 llama-server에 전달하기 전에 제거하며, 별도의 `Authorization` 헤더는 유지합니다.

서비스 제거 후 키 파일은 자동으로 삭제하지 않습니다. 키를 교체하려면 서비스와 관련 앱을 중지한 뒤 관리자 권한으로 키 파일을 삭제하고 서비스를 다시 시작하세요. 새 키가 생성되며 이전 키는 사용할 수 없게 됩니다.

### Managed Llama 업데이트

#### 웹 대시보드

**연동 설정 → Managed Llama 업데이트**에서 현재 버전과 새 버전을 확인할 수 있습니다.
새 릴리스가 있으면 화면 상단에 알림이 표시되며 **업데이트 및 재시작**을 누르면
서버가 실행 파일을 다운로드하고 검증한 뒤 교체합니다. 다운로드 진행률과 오류도
같은 화면에 표시됩니다. 다운로드만으로 자동 설치하지 않으며 버튼에서 확인해야 합니다.

기본 배포 주소는 `https://github.com/OWNER/managed-llama`라는 **예시 값**입니다.
실제 주소를 등록하기 전에는 GitHub 조회나 다운로드를 하지 않습니다. 준비되면
화면의 배포 주소 또는 `config.json`의 `update_repository`를 공개 GitHub 저장소 URL로
변경하세요. 접속 시와 페이지가 열려 있는 동안 30분 간격으로 새 버전을 확인합니다.
비공개 저장소, 임의 다운로드 URL, 사전 릴리스는 지원하지 않습니다.

배포 릴리스에는 `v1.2.3` 형식의 정식 태그와 다음 파일이 필요합니다.

- x64: `managed-llama-windows-amd64.exe`
- ARM64: `managed-llama-windows-arm64.exe` (해당 아키텍처를 배포할 때)
- GitHub 릴리스 asset API의 `sha256:` digest. 파일 크기는 256 MiB 이하입니다.

예를 들어 x64 빌드에서 버전을 포함하는 명령은 다음과 같습니다. 출력 파일을 해당
버전의 GitHub Release에 업로드해야 새 버전으로 표시됩니다. 버전을 지정하지 않은
빌드는 `개발 빌드`로 표시하며, 사용자가 정식 릴리스로 전환할 수 있습니다.

```powershell
go build -ldflags "-H=windowsgui -X main.version=v1.2.3" -o managed-llama-windows-amd64.exe .
```

웹 업데이트는 로컬 대시보드에서만 가능하고, 서비스 모드에서는 기존 관리 키 인증이
필요합니다. 설치 폴더에 파일을 쓸 수 있어야 하며 `go run` 임시 실행 파일은 지원하지
않습니다. 다운로드의 SHA-256, 크기, 앱 종류와 아키텍처를 확인합니다. 실행 중인
llama-server와 트레이는 교체 전에 종료됩니다. 사용자 앱은 다시 실행하고, 서비스는
재시작합니다. 서비스 트레이는 업데이트 후 실행 파일을 다시 열어 시작하세요.
기존 트레이의 임시 대시보드 주소는 만료되므로 트레이에서 대시보드를 다시 엽니다.
llama-server는 업데이트 후 자동 시작하지 않습니다.

실패 시 화면 또는 실행 파일 옆의 `*.update-result.json`에서 결과를 확인할 수 있습니다.
기존 실행 파일은 백업하며, 서비스 시작 실패 시 이전 파일 복구를 시도합니다. 다운로드한
업데이트 실행 파일(`.managed-llama-update-*.exe`)은 작업이 끝나면 수동으로 정리할 수 있습니다.

#### CLI

새 버전 실행 파일에서 `-update`에 **기존 실행 파일 경로**를 지정합니다. 먼저 기존
Managed Llama 트레이를 모두 종료하세요. 사용자 모드는 앱 종료로 llama-server도 중지되며,
서비스 모드는 업데이트 명령이 서비스를 중지하면서 llama-server를 정리합니다.
새 파일은 기존 설치 경로와 다른 위치에 준비합니다. 소스에서 업데이트할 때의 예시입니다.

```powershell
go build -o managed-llama-new.exe .
# 서비스 또는 Program Files 설치를 업데이트할 때는 관리자 PowerShell에서 실행
.\managed-llama-new.exe -update "C:\Program Files\ManagedLlama\managed-llama.exe"
```

일반 사용자 설치도 같은 명령에 실제 기존 exe 경로를 지정하면 됩니다. `-config`와
`-listen`은 업데이트에 사용하지 않으며, 기존 서비스에 등록된 경로와 인수를 유지합니다.
서비스를 제거하거나 재등록할 필요가 없습니다. 실행 중이던 서비스는 교체 후 다시
시작하고, 중지 상태였던 서비스는 그대로 둡니다. 트레이는 업데이트된 실행 파일로
다시 실행하세요. llama-server는 대시보드에서 별도로 시작합니다.

설정, 서비스 관리 키, 로그인 자동 시작 등록, 모델과 llama.cpp 바이너리는 유지합니다.
이전 exe는 설치 폴더의 `managed-llama-backup-*.exe`에 보관하며 성공 메시지에서 경로를
확인할 수 있습니다. 새 서비스 시작 실패 시 이전 exe로 복구하고 서비스 재시작을 시도합니다.
복구까지 실패하면 오류에 표시된 백업 경로에서 수동 복구해야 합니다.

이 명령은 로컬에 준비한 새 실행 파일을 적용하며 인터넷 버전 확인이나 다운로드는
수행하지 않습니다. 앱 종류와 Windows 아키텍처는 검사하지만 배포자 서명을 검증하지는
않으므로 직접 빌드했거나 신뢰하는 경로에서 받은 파일을 사용하세요. 업데이트 도중
강제 종료된 경우에는 실행 중인 업데이트가 없는지 확인한 뒤 기존 exe 옆의
`.update-lock` 파일을 제거하고, 실행 파일이 없다면 백업으로 먼저 복구하세요.

## 환경변수

환경변수는 `config.json`과 대시보드에서 입력한 값보다 우선합니다.

| 변수 | 용도 |
|---|---|
| `LLAMA_SERVER_PATH` | `llama-server` 또는 `llama-server.exe` 파일 경로 |
| `GGUF_MODELS_DIR` | GGUF 모델을 검색하고 다운로드할 디렉터리 |
| `HF_ENDPOINT` | Hugging Face API 주소. 기본값은 `https://huggingface.co` |

Hugging Face endpoint와 token은 `config.json`에 저장합니다. `hugging_face_token`은
`base64:` 접두사와 Base64 인코딩 값으로 저장하고 실행 시 복원합니다. Base64는 암호화가
아니며 파일 접근자가 복원할 수 있습니다. 기존 평문 토큰은 그대로 읽을 수 있고 다음
설정 저장 시 Base64 형식으로 전환됩니다. 토큰은 설정 조회 API로 반환하지 않으며,
UI에서 빈 값으로 저장하면 기존 토큰을 유지하고 제거 버튼으로 삭제합니다.
토큰이 없으면 Hugging Face 검색·파일 조회·다운로드 API가 비활성화됩니다.
로컬 GGUF 업로드와 관리는 계속 사용할 수 있습니다.

`.env`와 `.env.*`는 Git에서 제외하며 `.env.example`, `.env.*.example`은 추적할 수 있습니다.
이 제외 규칙은 `.env` 자동 로딩이나 `HF_TOKEN` 환경변수 지원을 추가하지 않습니다.

PowerShell 예시:

```powershell
$env:LLAMA_SERVER_PATH = "C:\tools\llama.cpp\llama-server.exe"
$env:GGUF_MODELS_DIR = "D:\models\gguf"
$env:HF_ENDPOINT = "https://huggingface.co"
.\managed-llama.exe
```

## 시작 인수 템플릿

기본값은 다음과 같습니다.

```text
--models-dir {models_dir} --models-max {models_max} --host {host} --port {port} --ctx-size {ctx} --n-gpu-layers {gpu_layers} --parallel {parallel}
```

지원하는 치환자는 `{models_dir}`, `{models_max}`, `{model}`, `{host}`, `{port}`, `{ctx}`, `{gpu_layers}`, `{threads}`, `{parallel}`입니다. `{model}`을 넣으면 기존 단일 모델 모드도 사용할 수 있습니다. 템플릿은 셸로 실행하지 않고 인수 목록으로 파싱하므로 공백이 포함된 Windows 경로도 처리합니다. 실행 파일 자체는 `server_path`에서 변경할 수 있습니다.

## 프록시 주소

관리 도구가 `127.0.0.1:3030`에서 실행 중일 때 외부 OpenAI SDK에는 다음 base URL을 지정합니다.

```text
http://127.0.0.1:3030/v1
```

`/v1/chat/completions`, `/v1/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/models`를 포함해 llama.cpp가 현재 또는 향후 제공하는 모든 `/v1/*` 경로를 그대로 전달합니다. `Authorization` 등의 요청 헤더, 상태 코드, 응답 헤더와 SSE 스트림도 유지합니다.

llama.cpp 고유 API는 `/llama` prefix를 사용합니다.

```text
GET  /llama/models?reload=1
POST /llama/models/load
POST /llama/models/unload
GET  /llama/health
GET  /llama/slots
GET  /llama/metrics
GET  /llama/props
```

대시보드의 `전체 해제 · 종료`는 `taskkill /T /F`로 router와 모델 worker 프로세스 트리를 모두 종료합니다.

`프로세스 · GPU` 화면에서는 현재 권한으로 조회 가능한 Windows 프로세스와
NVIDIA VRAM 점유 현황을 함께 확인할 수 있습니다. 일반 프로세스는 단일 종료 또는
자식 트리 종료를 직접 요청할 수 있습니다. PID 재사용 사고를 막기 위해 종료 직전에
프로세스 시작 시각을 다시 확인하며, Managed Llama 자신과 관리 중인 llama-server
프로세스 트리 및 Windows 핵심 프로세스는 이 화면에서 종료할 수 없습니다. 자동 종료
규칙은 적용하지 않습니다.

게이트웨이가 Ctrl+C 또는 SIGTERM을 받는 경우에도 같은 방식으로 llama-server 프로세스 트리를 먼저 정리한 뒤 HTTP 서버를 종료합니다. llama.cpp의 stdout/stderr는 대시보드 로그 화면과 `GET /api/server/logs`에서 확인할 수 있으며 `DELETE /api/server/logs`로 메모리 버퍼를 비울 수 있습니다.

## 데이터 위치

- `config.json`: 로컬 실행 설정
- `models/`: 기본 GGUF 저장소
- `*.part`: 다운로드 또는 업로드가 완료되기 전의 임시 파일

기본 `config.json`, 기본 `models/` 폴더와 `*.part`는 Git에서 제외됩니다. 사용자 지정 설정 경로나 모델 경로를 저장소 안에 두면 별도로 제외 여부를 확인하세요.

설정은 같은 폴더의 고유한 임시 파일(`config.json.tmp-*`)에 쓴 뒤 교체합니다. 정상 완료와 저장 오류 시 임시 파일을 정리합니다. 강제 종료로 남은 임시 파일, 이전 버전의 `config.json.tmp`, 서비스 관리 키(`*.service-key`), 환경 파일과 빌드 산출물도 Git에서 제외합니다. 설정에는 복원 가능한 HF 토큰이 포함될 수 있으므로 로컬 작업 폴더 전체를 배포용 압축 파일로 사용하지 마세요.

## 개발 검증

```powershell
go test ./...
go vet ./...
go build .
```

UI 테스트에는 `node:test`를 지원하는 Node.js가 필요합니다. 별도 npm 의존성 설치 없이 실행할 수 있습니다.

```powershell
node --test tests/*.test.cjs
```

## 프로젝트 구조

```text
main.go                  실행 진입점과 HTTP 게이트웨이
service_windows.go       Windows 서비스 관리
tray_windows.go          시스템 트레이
user_session_windows.go  사용자 세션과 서비스 전환
internal/api/            관리 API와 llama-server 프록시
internal/config/         설정 읽기·저장과 환경변수
internal/hf/             Hugging Face 검색·다운로드
internal/models/         GGUF 파일 관리
internal/runner/         llama-server 프로세스 관리
internal/resources/      Windows 프로세스·GPU 정보와 종료
internal/autostart/      로그인 트레이 자동 실행
internal/process/        백그라운드 명령 실행 지원
internal/serviceauth/    서비스 인증과 Windows 설치 경로 권한 검사
web/                     내장 대시보드
tests/                   JavaScript UI 테스트
```

## 문제 해결

| 증상 | 확인할 사항 |
|---|---|
| `setup_required` | `server_path` 또는 `LLAMA_SERVER_PATH`가 실제 실행 파일을 가리키는지 확인 |
| `empty_library` | GGUF를 추가하고 재검색. 파일 헤더 검증 실패 여부 확인 |
| `default_model_required` | 모델 목록에서 기본 모델 선택 |
| 서버 시작 실패 | 로그에서 실행 인수, 포트 충돌, 메모리 부족, llama.cpp 버전 확인 |
| Hugging Face 기능 비활성화 | 설정에 토큰을 저장했는지 확인 |
| gated/private 모델 접근 실패 | 토큰 권한과 해당 저장소 접근 권한 확인 |
| GPU 정보가 없음 | PowerShell에서 `nvidia-smi` 실행 여부 확인 |
| 자동 실행 설정 비활성화 | 트레이의 대시보드에서만 변경 가능. 서비스 설치 경로 조건도 확인 |
| 서비스 API가 `401` 반환 | 대시보드에 관리 키를 입력하거나 클라이언트에 `X-Managed-Llama-Key` 헤더 설정 |
| `unsafe service path` | 관리자 소유의 전용 폴더, 파일 소유자, ACL과 reparse point 여부 확인 |

## 라이선스

프로젝트는 [MIT License](LICENSE)로 공개합니다. 외부 의존성, llama.cpp,
다운로드한 모델에는 각자의 라이선스가 적용됩니다.
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)와 배포물의 `licenses/`를 확인하세요.

## 공개 및 릴리스 준비

Windows CI는 Go 테스트·vet, UI 테스트, 포맷 검사와 릴리스 빌드를 수행합니다.
보안 범위와 제보 방법은 [SECURITY.md](SECURITY.md)를 참고하세요.

버전을 포함한 업데이트용 실행 파일과 의존성 라이선스는 다음 명령으로 생성합니다.

```powershell
.\scripts\build-release.ps1 -Version 0.1.0
```

결과는 `dist/release-v0.1.0/`입니다. GitHub의 `v0.1.0` Release에
`managed-llama-windows-amd64.exe`와 체크섬을 올리고, 함께 생성된 라이선스·고지
파일도 배포하세요. 원클릭 설치판은 앞의 `build-installer.ps1`로 별도 생성합니다.
두 빌드 모두 같은 버전을 앱에 주입합니다. GitHub asset API의 SHA-256 digest도
웹 업데이트 검증에 필요하며, 별도 `.sha256` 파일만으로 대체되지 않습니다.

공개 저장소 주소가 정해지기 전에는 `OWNER` 예시 값을 유지하여 자동 조회를
비활성화합니다. 실제 공개 주소와 Release가 준비된 후 설정에서 변경하세요.
저장소 공개 및 push와 설치판 배포는 별도 작업입니다. 첫 배포 전 확인 항목:

- 깨끗한 Windows 환경에서 신규 설치·재설치·업그레이드 및 제거
- 로그인 전 서비스 시작, 로그인 트레이 인증 안내, 자동 실행 해제·복원
- 포트 충돌, UAC 취소, 업데이트 실패·롤백과 설정·모델 보존
- 실제 GitHub Release의 버전·digest 확인과 웹 업데이트 전체 흐름
- 번들 런타임 DLL의 재배포 조건·고지, 필요 시 배포자 코드 서명

이 항목은 CI 통과만으로 검증 완료 처리하지 않습니다.
