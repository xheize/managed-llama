package autostart

import "testing"

func TestManagerCommandIncludesExecutableConfigAndListen(t *testing.T) {
	manager := newManager(`C:\Program Files\Managed Llama\managed-llama.exe`, `C:\Users\Tester\Managed Llama\config.json`, "127.0.0.1:3030")
	want := `"C:\Program Files\Managed Llama\managed-llama.exe" -config "C:\Users\Tester\Managed Llama\config.json" -listen "127.0.0.1:3030"`
	if manager.command != want {
		t.Fatalf("command = %q, want %q", manager.command, want)
	}
}

func TestSetupManagedStartupDoesNotWritePerUserRegistry(t *testing.T) {
	manager := &Manager{setupManaged: true}
	// These calls must succeed without creating an empty per-user Run command.
	if err := manager.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := manager.Set(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := manager.Enabled(); err != nil || !enabled {
		t.Fatalf("setup shortcut: %v, %v", enabled, err)
	}
}
