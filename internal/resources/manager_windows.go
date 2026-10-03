package resources

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	background "managed-llama/internal/process"
)

type GPUUse struct {
	GPUIndex    int    `json:"gpu_index"`
	GPUUUID     string `json:"gpu_uuid"`
	UsedVRAMMiB *int64 `json:"used_vram_mib,omitempty"`
}

type Process struct {
	PID            uint32    `json:"pid"`
	ParentPID      uint32    `json:"parent_pid,omitempty"`
	Name           string    `json:"name"`
	Executable     string    `json:"executable,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	GPU            []GPUUse  `json:"gpu,omitempty"`
	TotalVRAMMiB   int64     `json:"total_vram_mib,omitempty"`
	ManagedByLlama bool      `json:"managed_by_llama"`
	Protected      bool      `json:"protected"`
	CanTerminate   bool      `json:"can_terminate"`
}

type GPU struct {
	Index          int    `json:"index"`
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	MemoryTotalMiB int64  `json:"memory_total_mib"`
	MemoryUsedMiB  int64  `json:"memory_used_mib"`
}

type Snapshot struct {
	CapturedAt      time.Time `json:"captured_at"`
	Processes       []Process `json:"processes"`
	GPUs            []GPU     `json:"gpus"`
	ProcessError    string    `json:"process_error,omitempty"`
	NVIDIAAvailable bool      `json:"nvidia_available"`
	NVIDIAError     string    `json:"nvidia_error,omitempty"`
}

type Event struct {
	At         time.Time `json:"at"`
	PID        uint32    `json:"pid"`
	Name       string    `json:"name"`
	Executable string    `json:"executable,omitempty"`
	Tree       bool      `json:"tree"`
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
}

type Manager struct {
	mu     sync.Mutex
	events []Event
}

func New() *Manager { return &Manager{} }

func (m *Manager) Snapshot(managedPID int) Snapshot {
	processes, processErr := listProcesses(uint32(managedPID))
	result := Snapshot{CapturedAt: time.Now(), Processes: processes}
	if processErr != nil {
		result.ProcessError = "프로세스 목록을 읽을 수 없습니다: " + processErr.Error()
		return result
	}
	gpus, uses, err := queryNVIDIA()
	result.GPUs = gpus
	result.NVIDIAAvailable = len(gpus) > 0
	if err != nil {
		result.NVIDIAError = err.Error()
		if len(gpus) == 0 {
			return result
		}
	}
	byPID := make(map[uint32]int, len(result.Processes))
	for index := range result.Processes {
		byPID[result.Processes[index].PID] = index
	}
	for pid, gpuUses := range uses {
		index, ok := byPID[pid]
		if !ok {
			result.Processes = append(result.Processes, Process{PID: pid, Name: "알 수 없는 GPU 프로세스", GPU: gpuUses})
			index = len(result.Processes) - 1
		}
		result.Processes[index].GPU = gpuUses
		for _, use := range gpuUses {
			if use.UsedVRAMMiB != nil {
				result.Processes[index].TotalVRAMMiB += *use.UsedVRAMMiB
			}
		}
	}
	sort.Slice(result.Processes, func(i, j int) bool {
		if result.Processes[i].TotalVRAMMiB != result.Processes[j].TotalVRAMMiB {
			return result.Processes[i].TotalVRAMMiB > result.Processes[j].TotalVRAMMiB
		}
		return strings.ToLower(result.Processes[i].Name) < strings.ToLower(result.Processes[j].Name)
	})
	return result
}

func (m *Manager) Terminate(pid uint32, expectedStart time.Time, tree bool, managedPID int) error {
	process, err := findProcess(pid, uint32(managedPID))
	if err != nil {
		return err
	}
	if process.ManagedByLlama {
		return errors.New("managed llama 프로세스는 서버의 전체 해제 · 종료 기능을 사용하세요")
	}
	if process.Protected || !process.CanTerminate {
		return errors.New("보호되었거나 현재 권한으로 종료할 수 없는 프로세스입니다")
	}
	if expectedStart.IsZero() || process.StartedAt.IsZero() || !sameStartTime(expectedStart, process.StartedAt) {
		return errors.New("프로세스가 목록을 조회한 이후 변경되었습니다. 새로고침 후 다시 시도하세요")
	}

	message := "프로세스를 종료했습니다"
	if tree {
		cmd := background.HideConsole(exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(pid), 10), "/T", "/F"))
		output, killErr := cmd.CombinedOutput()
		if killErr != nil {
			err = fmt.Errorf("프로세스 트리 종료 실패: %w: %s", killErr, strings.TrimSpace(string(output)))
		}
		message = "프로세스 트리를 종료했습니다"
	} else {
		var handle windows.Handle
		handle, err = windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
		if err == nil {
			defer windows.CloseHandle(handle)
			err = windows.TerminateProcess(handle, 1)
		}
		if err != nil {
			err = fmt.Errorf("프로세스 종료 실패: %w", err)
		}
	}

	event := Event{At: time.Now(), PID: pid, Name: process.Name, Executable: process.Executable, Tree: tree, Success: err == nil, Message: message}
	if err != nil {
		event.Message = err.Error()
	}
	m.appendEvent(event)
	return err
}

func (m *Manager) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event(nil), m.events...)
}

func (m *Manager) appendEvent(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append([]Event{event}, m.events...)
	if len(m.events) > 100 {
		m.events = m.events[:100]
	}
}

func listProcesses(managedPID uint32) ([]Process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	selfPID := uint32(os.Getpid())
	var list []Process
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		process := inspectProcess(entry.ProcessID, entry.ParentProcessID, name, selfPID, managedPID)
		list = append(list, process)
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	protectManagedTree(list, managedPID)
	return list, nil
}

func protectManagedTree(list []Process, managedPID uint32) {
	if managedPID == 0 {
		return
	}
	managed := map[uint32]bool{managedPID: true}
	for changed := true; changed; {
		changed = false
		for index := range list {
			if !managed[list[index].PID] && managed[list[index].ParentPID] {
				managed[list[index].PID] = true
				changed = true
			}
		}
	}
	for index := range list {
		if managed[list[index].PID] {
			list[index].ManagedByLlama = true
			list[index].Protected = true
			list[index].CanTerminate = false
		}
	}
}

func findProcess(pid, managedPID uint32) (Process, error) {
	list, err := listProcesses(managedPID)
	if err != nil {
		return Process{}, err
	}
	for _, process := range list {
		if process.PID == pid {
			return process, nil
		}
	}
	return Process{}, errors.New("프로세스가 이미 종료되었습니다")
}

func inspectProcess(pid, parentPID uint32, name string, selfPID, managedPID uint32) Process {
	process := Process{PID: pid, ParentPID: parentPID, Name: name, ManagedByLlama: managedPID != 0 && pid == managedPID}
	process.Protected = isProtected(pid, name) || pid == selfPID
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err == nil {
		defer windows.CloseHandle(handle)
		buffer := make([]uint16, 32768)
		size := uint32(len(buffer))
		if windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size) == nil {
			process.Executable = windows.UTF16ToString(buffer[:size])
		}
		var creation, exit, kernel, user windows.Filetime
		if windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user) == nil {
			process.StartedAt = time.Unix(0, creation.Nanoseconds())
		}
	}
	if !process.Protected && !process.ManagedByLlama && !process.StartedAt.IsZero() {
		terminateHandle, terminateErr := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
		if terminateErr == nil {
			process.CanTerminate = true
			windows.CloseHandle(terminateHandle)
		}
	}
	return process
}

func isProtected(pid uint32, name string) bool {
	if pid <= 4 {
		return true
	}
	_, protected := protectedNames[strings.ToLower(name)]
	return protected
}

var protectedNames = map[string]struct{}{
	"csrss.exe": {}, "lsass.exe": {}, "registry": {}, "secure system": {},
	"services.exe": {}, "smss.exe": {}, "system": {}, "wininit.exe": {},
	"winlogon.exe": {},
}

func sameStartTime(a, b time.Time) bool {
	// JSON round trips preserve more precision than Windows process timestamps need,
	// but tolerate a millisecond for clients that normalize date values.
	delta := a.Sub(b)
	if delta < 0 {
		delta = -delta
	}
	return delta <= time.Millisecond
}

func queryNVIDIA() ([]GPU, map[uint32][]GPUUse, error) {
	executable, err := findNvidiaSMI()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gpuOutput, err := background.HideConsole(exec.CommandContext(ctx, executable,
		"--query-gpu=index,uuid,name,memory.total,memory.used",
		"--format=csv,noheader,nounits")).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("nvidia-smi GPU 조회 실패: %w", err)
	}
	gpus, err := parseGPUs(string(gpuOutput))
	if err != nil {
		return nil, nil, fmt.Errorf("nvidia-smi GPU 결과 해석 실패: %w", err)
	}
	processOutput, err := background.HideConsole(exec.CommandContext(ctx, executable,
		"--query-compute-apps=pid,gpu_uuid,used_gpu_memory",
		"--format=csv,noheader,nounits")).Output()
	if err != nil {
		return gpus, map[uint32][]GPUUse{}, fmt.Errorf("nvidia-smi 프로세스 조회 실패: %w", err)
	}
	return gpus, parseGPUUses(string(processOutput), gpus), nil
}

func findNvidiaSMI() (string, error) {
	if path, err := exec.LookPath("nvidia-smi.exe"); err == nil {
		return path, nil
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		path := filepath.Join(programFiles, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("nvidia-smi를 찾을 수 없습니다")
}

func parseGPUs(output string) ([]GPU, error) {
	records, err := csv.NewReader(strings.NewReader(strings.TrimSpace(output))).ReadAll()
	if err != nil {
		return nil, err
	}
	result := make([]GPU, 0, len(records))
	for _, record := range records {
		if len(record) < 5 {
			continue
		}
		index, indexErr := strconv.Atoi(strings.TrimSpace(record[0]))
		total, totalErr := strconv.ParseInt(strings.TrimSpace(record[3]), 10, 64)
		used, usedErr := strconv.ParseInt(strings.TrimSpace(record[4]), 10, 64)
		if indexErr != nil || totalErr != nil || usedErr != nil {
			return nil, fmt.Errorf("unexpected row %q", record)
		}
		result = append(result, GPU{Index: index, UUID: strings.TrimSpace(record[1]), Name: strings.TrimSpace(record[2]), MemoryTotalMiB: total, MemoryUsedMiB: used})
	}
	return result, nil
}

func parseGPUUses(output string, gpus []GPU) map[uint32][]GPUUse {
	result := make(map[uint32][]GPUUse)
	reader := csv.NewReader(strings.NewReader(strings.TrimSpace(output)))
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(record) < 3 {
			continue
		}
		pidValue, err := strconv.ParseUint(strings.TrimSpace(record[0]), 10, 32)
		if err != nil {
			continue
		}
		uuid := strings.TrimSpace(record[1])
		use := GPUUse{GPUIndex: -1, GPUUUID: uuid}
		for _, gpu := range gpus {
			if gpu.UUID == uuid {
				use.GPUIndex = gpu.Index
				break
			}
		}
		if value, err := strconv.ParseInt(strings.TrimSpace(record[2]), 10, 64); err == nil {
			use.UsedVRAMMiB = &value
		}
		pid := uint32(pidValue)
		result[pid] = append(result[pid], use)
	}
	return result
}
