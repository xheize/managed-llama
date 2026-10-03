#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif
#ifndef PayloadDir
  #error PayloadDir is required. Use scripts/build-installer.ps1.
#endif

[Setup]
AppId={{D30667BE-F0E7-476A-A036-BA23DC32A70A}
AppName=Managed Llama
AppVersion={#AppVersion}
SetupMutex=ManagedLlamaSetup
DefaultDirName={autopf}\ManagedLlama
UsePreviousAppDir=no
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
DisableWelcomePage=yes
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableReadyPage=yes
DisableFinishedPage=yes
UninstallDisplayIcon={app}\managed-llama.exe
OutputDir=..\dist
OutputBaseFilename=ManagedLlamaSetup-{#AppVersion}-windows-amd64
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
WizardStyle=modern
SetupIconFile=..\web\favicon\managed-local-llm-tray-dark.ico

[Languages]
Name: "korean"; MessagesFile: "compiler:Languages\Korean.isl"

[Files]
Source: "{#PayloadDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PayloadDir}\managed-llama.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\managed-llama.exe"; DestName: "setup-helper.exe"; Flags: dontcopy
Source: "{#PayloadDir}\runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "installed-by-setup"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{app}\models"; Flags: uninsneveruninstall

[Icons]
Name: "{commonprograms}\Managed Llama"; Filename: "{app}\managed-llama.exe"; WorkingDir: "{app}"
Name: "{commonstartup}\Managed Llama"; Filename: "{app}\managed-llama.exe"; Parameters: "-installed-startup"; WorkingDir: "{app}"

[Run]
Filename: "{app}\managed-llama.exe"; WorkingDir: "{app}"; Flags: nowait runasoriginaluser skipifsilent; Check: InstallOK

[Code]
var
  ResumeService: Boolean;
  Completed: Boolean;
  Failed: Boolean;
  HelperError: String;
  IsNewInstallation: Boolean;

function InitializeSetup: Boolean;
var
  UninstallKey: String;
begin
  { Snapshot before this run creates/updates the uninstall registration.
    Match this package's machine-wide, 64-bit installation scope exactly. }
  UninstallKey := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\' +
    ExpandConstant('{#SetupSetting("AppId")}') + '_is1';
  IsNewInstallation := not RegKeyExists(HKLM64, UninstallKey);
  if IsNewInstallation then
    Log('Installation mode: new (no existing AppId registration)')
  else
    Log('Installation mode: existing (update or reinstall)');
  Result := True;
end;

procedure InitializeWizard;
begin
  if IsNewInstallation then
    WizardForm.Caption := 'Managed Llama — 신규 설치'
  else
    WizardForm.Caption := 'Managed Llama — 업데이트 / 재설치';
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = wpInstalling then begin
    if IsNewInstallation then
      WizardForm.PageDescriptionLabel.Caption := 'Managed Llama를 새로 설치하고 있습니다.'
    else
      WizardForm.PageDescriptionLabel.Caption := '기존 Managed Llama 설치를 업데이트하거나 재설치하고 있습니다.';
  end;
end;

function InstallOK: Boolean;
begin
  Result := Completed and not Failed;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := PageID = wpReady;
end;

function RunHelper(Executable, Action: String): Integer;
var
  Code, I: Integer;
  ErrorFile: String;
  ErrorLines: TArrayOfString;
begin
  HelperError := '';
  ErrorFile := ExpandConstant('{tmp}\managed-llama-setup-error.txt');
  DeleteFile(ErrorFile);
  if not Exec(Executable, '-setup ' + Action + ' -config "' +
    ExpandConstant('{app}\config.json') + '" -setup-error-file "' + ErrorFile + '"', '', SW_HIDE, ewWaitUntilTerminated, Code) then
    Code := -1;
  if LoadStringsFromFile(ErrorFile, ErrorLines) then
    for I := 0 to GetArrayLength(ErrorLines) - 1 do
      HelperError := HelperError + ErrorLines[I] + #13#10;
  if (Code <> 0) and (Code <> 10) and (HelperError = '') then
    HelperError := '오류 세부 정보를 읽지 못했습니다. 종료 코드: ' + IntToStr(Code) +
      '. Windows 이벤트 뷰어의 응용 프로그램 로그에서 ManagedLlama 오류를 확인하세요.';
  Log('Installer operation ' + Action + ': ' + IntToStr(Code));
  if HelperError <> '' then Log(HelperError);
  Result := Code;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Result := '';
  { Fix the location even when /DIR or an old installation requests another path. }
  WizardForm.DirEdit.Text := ExpandConstant('{autopf}\ManagedLlama');
  ExtractTemporaryFile('setup-helper.exe');
  Code := RunHelper(ExpandConstant('{tmp}\setup-helper.exe'), 'check');
  if Code <> 0 then begin
    Result := '설치를 준비하지 못했습니다.' + #13#10 + HelperError;
    exit;
  end;
  Code := RunHelper(ExpandConstant('{tmp}\setup-helper.exe'), 'stop');
  if Code = 10 then ResumeService := True
  else if Code <> 0 then Result := 'Managed Llama 서비스를 중지하지 못했습니다.' + #13#10 + HelperError;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    Failed := RunHelper(ExpandConstant('{app}\managed-llama.exe'), 'install') <> 0;
    Completed := not Failed;
    if Failed then
      MsgBox('파일 설치 후 서비스 구성에 실패했습니다.' + #13#10#13#10 + HelperError + #13#10 +
        '문제 해결 후 설치 파일을 다시 실행하세요. 기존 설정과 모델은 보존됩니다.', mbError, MB_OK);
  end;
end;

function GetCustomSetupExitCode: Integer;
begin
  Result := 0;
  if Failed then Result := 1;
end;

procedure DeinitializeSetup;
begin
  if ResumeService and not Completed then
    if RunHelper(ExpandConstant('{tmp}\setup-helper.exe'), 'resume') <> 0 then
      MsgBox('기존 서비스를 재시작하지 못했습니다.' + #13#10 + HelperError, mbError, MB_OK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    if RunHelper(ExpandConstant('{app}\managed-llama.exe'), 'remove') <> 0 then
      RaiseException('Managed Llama 서비스를 제거하지 못해 프로그램 제거를 중단했습니다.' + #13#10 + HelperError);
end;
