package resources

import (
	"testing"
	"time"
)

func TestParseNVIDIAOutput(t *testing.T) {
	gpus, err := parseGPUs("0, GPU-one, NVIDIA RTX 4090, 24564, 12000\n1, GPU-two, NVIDIA RTX 3090, 24576, 2048\n")
	if err != nil || len(gpus) != 2 {
		t.Fatalf("parseGPUs() = %#v, %v", gpus, err)
	}
	uses := parseGPUUses("123, GPU-one, 8192\n456, GPU-two, [N/A]\n", gpus)
	if got := *uses[123][0].UsedVRAMMiB; got != 8192 {
		t.Fatalf("used memory = %d", got)
	}
	if uses[123][0].GPUIndex != 0 || uses[456][0].UsedVRAMMiB != nil {
		t.Fatalf("unexpected GPU uses: %#v", uses)
	}
}

func TestProtectedProcesses(t *testing.T) {
	if !isProtected(4, "System") || !isProtected(100, "LSASS.EXE") {
		t.Fatal("critical Windows processes must be protected")
	}
	if isProtected(100, "python.exe") {
		t.Fatal("ordinary process unexpectedly protected")
	}
}

func TestSameStartTime(t *testing.T) {
	now := time.Now()
	if !sameStartTime(now, now.Add(time.Millisecond)) || sameStartTime(now, now.Add(2*time.Millisecond)) {
		t.Fatal("unexpected process identity time comparison")
	}
}

func TestProtectManagedTree(t *testing.T) {
	list := []Process{{PID: 10, CanTerminate: true}, {PID: 11, ParentPID: 10, CanTerminate: true}, {PID: 12, ParentPID: 11, CanTerminate: true}, {PID: 20, CanTerminate: true}}
	protectManagedTree(list, 10)
	for index := 0; index < 3; index++ {
		if !list[index].ManagedByLlama || !list[index].Protected || list[index].CanTerminate {
			t.Fatalf("managed process was not protected: %#v", list[index])
		}
	}
	if list[3].ManagedByLlama || !list[3].CanTerminate {
		t.Fatalf("unrelated process was changed: %#v", list[3])
	}
}
