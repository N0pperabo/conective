package procutil

import (
	"fmt"
	"testing"
)

func TestGetRunningApps(t *testing.T) {
	apps, err := GetRunningApps()
	if err != nil {
		t.Fatalf("GetRunningApps failed: %v", err)
	}
	fmt.Printf("Found %d running apps:\n", len(apps))
	for i, app := range apps {
		fmt.Printf("[%d] Name: %-25s | Exe: %-20s | Title: %-30s | Path: %s\n",
			i+1, app.Name, app.ExeName, app.Title, app.Path)
	}
}

func TestGetInstalledApps(t *testing.T) {
	apps, err := GetInstalledApps()
	if err != nil {
		t.Fatalf("GetInstalledApps failed: %v", err)
	}
	fmt.Printf("Found %d installed apps (sample of first 10):\n", len(apps))
	for i := 0; i < len(apps) && i < 10; i++ {
		app := apps[i]
		fmt.Printf("[%d] Name: %-25s | Exe: %-20s | Path: %s\n",
			i+1, app.Name, app.ExeName, app.Path)
	}
}
