package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/url"
	"os/exec"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows/registry"

	"managed-llama/internal/api"
	"managed-llama/internal/process"
	"managed-llama/internal/runner"
)

type trayController interface {
	StartServer() error
	StopServer() error
	RuntimeStatus() runner.Status
	CurrentState() api.GatewayState
}

type trayApp struct {
	controller   trayController
	dashboardURL string
	serviceTray  bool
	operationMu  sync.Mutex
}

func runTray(controller trayController, listen string, onExit func(), serviceTray bool) {
	app := &trayApp{controller: controller, dashboardURL: localDashboardURL(listen), serviceTray: serviceTray}
	systray.Run(app.ready, onExit)
}

func (a *trayApp) ready() {
	systray.SetIcon(systemTrayIcon())
	systray.SetTooltip("Managed Llama")

	statusItem := systray.AddMenuItem("상태 확인 중...", "llama-server 상태")
	statusItem.Disable()
	systray.AddSeparator()
	openItem := systray.AddMenuItem("대시보드 열기", a.dashboardURL)
	startItem := systray.AddMenuItem("llama-server 시작", "선택한 기본 모델로 llama-server를 시작합니다")
	stopItem := systray.AddMenuItem("llama-server 중지", "모델을 해제하고 llama-server를 중지합니다")
	systray.AddSeparator()
	quitTitle := "Managed Llama 종료"
	quitTooltip := "llama-server와 관리 게이트웨이를 종료합니다"
	if a.serviceTray {
		quitTitle = "트레이 아이콘 종료"
		quitTooltip = "사용자 게이트웨이는 종료하고 등록된 백그라운드 서비스는 계속 실행합니다"
	}
	quitItem := systray.AddMenuItem(quitTitle, quitTooltip)

	refresh := make(chan struct{}, 1)
	go a.refreshStatus(statusItem, startItem, stopItem, refresh)
	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				if err := openDashboard(a.dashboardURL); err != nil {
					log.Printf("open dashboard: %v", err)
					statusItem.SetTitle("상태 · 대시보드 열기 실패")
					systray.SetTooltip("Managed Llama · 대시보드 열기 실패")
				}
			case <-startItem.ClickedCh:
				go a.runOperation(statusItem, startItem, stopItem, refresh, a.controller.StartServer)
			case <-stopItem.ClickedCh:
				go a.runOperation(statusItem, startItem, stopItem, refresh, a.controller.StopServer)
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func systemTrayIcon() []byte {
	iconPath := "web/favicon/managed-local-llm-tray-dark.ico"
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err == nil {
		if light, _, valueErr := key.GetIntegerValue("SystemUsesLightTheme"); valueErr == nil && light != 0 {
			iconPath = "web/favicon/managed-local-llm-tray-light.ico"
		}
		_ = key.Close()
	}
	icon, err := web.ReadFile(iconPath)
	if err != nil {
		log.Printf("load embedded system tray icon: %v", err)
		return managedLlamaIcon()
	}
	return icon
}

func (a *trayApp) runOperation(statusItem, startItem, stopItem *systray.MenuItem, refresh chan<- struct{}, operation func() error) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	startItem.Disable()
	stopItem.Disable()
	statusItem.SetTitle("요청 처리 중...")
	if err := operation(); err != nil {
		log.Printf("system tray operation: %v", err)
		statusItem.SetTitle("상태 · 작업 실패")
		systray.SetTooltip("Managed Llama · 작업 실패")
		return
	}
	select {
	case refresh <- struct{}{}:
	default:
	}
}

func (a *trayApp) refreshStatus(statusItem, startItem, stopItem *systray.MenuItem, refresh <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		runtime := a.controller.RuntimeStatus()
		state := a.controller.CurrentState()
		switch {
		case state.Phase == "service_auth_required":
			statusItem.SetTitle("상태 · 대시보드에서 인증 필요")
			systray.SetTooltip("Managed Llama · 대시보드를 열어 인증하세요")
			startItem.Disable()
			stopItem.Disable()
		case state.Phase == "service_unavailable":
			statusItem.SetTitle("상태 · 서비스 연결 대기 중")
			systray.SetTooltip("Managed Llama · 서비스 연결 대기 중")
			startItem.Disable()
			stopItem.Disable()
		case runtime.Ready:
			statusItem.SetTitle(fmt.Sprintf("상태 · 실행 중 (PID %d)", runtime.PID))
			systray.SetTooltip("Managed Llama · 실행 중")
			startItem.Disable()
			stopItem.Enable()
		case runtime.Running:
			statusItem.SetTitle(fmt.Sprintf("상태 · 시작 중 (PID %d)", runtime.PID))
			systray.SetTooltip("Managed Llama · 시작 중")
			startItem.Disable()
			stopItem.Enable()
		default:
			statusItem.SetTitle(compactStoppedStatus(state.Phase))
			systray.SetTooltip("Managed Llama · " + compactStatusLabel(state.Phase))
			stopItem.Disable()
			if state.CanStart {
				startItem.Enable()
			} else {
				startItem.Disable()
			}
		}

		select {
		case <-ticker.C:
		case <-refresh:
		}
	}
}

func compactStoppedStatus(phase string) string {
	return "상태 · " + compactStatusLabel(phase)
}

func compactStatusLabel(phase string) string {
	switch phase {
	case "ready":
		return "준비됨"
	case "setup_required":
		return "설정 필요"
	case "empty_library":
		return "모델 없음"
	case "default_model_required":
		return "기본 모델 필요"
	case "invalid_default_model":
		return "모델 확인 필요"
	case "error":
		return "오류"
	case "service_unavailable":
		return "서비스 연결 대기 중"
	default:
		return "중지됨"
	}
}

func openDashboard(address string) error {
	return process.HideConsole(exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address)).Start()
}

func localDashboardURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}).String()
}

// managedLlamaIcon builds a small 32-bit ICO at runtime. Keeping it in Go
// avoids shipping a second asset next to the single-file Windows executable.
func managedLlamaIcon() []byte {
	const size = 32
	const headerSize = 6 + 16
	const bitmapHeaderSize = 40
	const pixelBytes = size * size * 4
	const maskBytes = size * size / 8
	imageSize := bitmapHeaderSize + pixelBytes + maskBytes
	icon := make([]byte, headerSize+imageSize)

	binary.LittleEndian.PutUint16(icon[2:4], 1)
	binary.LittleEndian.PutUint16(icon[4:6], 1)
	icon[6], icon[7] = size, size
	binary.LittleEndian.PutUint16(icon[10:12], 1)
	binary.LittleEndian.PutUint16(icon[12:14], 32)
	binary.LittleEndian.PutUint32(icon[14:18], uint32(imageSize))
	binary.LittleEndian.PutUint32(icon[18:22], headerSize)

	dib := icon[headerSize:]
	binary.LittleEndian.PutUint32(dib[0:4], bitmapHeaderSize)
	binary.LittleEndian.PutUint32(dib[4:8], size)
	binary.LittleEndian.PutUint32(dib[8:12], size*2)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	binary.LittleEndian.PutUint32(dib[20:24], pixelBytes)

	pixels := dib[bitmapHeaderSize : bitmapHeaderSize+pixelBytes]
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			rounded := (x >= 5 && x < 27 && y >= 3 && y < 29) ||
				(x >= 3 && x < 29 && y >= 5 && y < 27)
			if !rounded {
				continue
			}
			// ICO bitmap rows are stored bottom-up and pixels are BGRA.
			offset := ((size-1-y)*size + x) * 4
			pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = 42, 35, 28, 255
			leftStroke := x >= 8 && x <= 11 && y >= 9 && y <= 23
			rightStroke := x >= 20 && x <= 23 && y >= 9 && y <= 23
			diagonal := y >= 9 && y <= 17 && (x == y-1 || x == y || x == 32-y || x == 31-y)
			if leftStroke || rightStroke || diagonal {
				pixels[offset], pixels[offset+1], pixels[offset+2] = 92, 220, 132
			}
		}
	}
	return icon
}
