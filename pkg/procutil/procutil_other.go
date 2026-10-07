//go:build !windows

package procutil

type AppInfo struct {
	Name    string `json:"name"`
	ExeName string `json:"exe_name"`
	Title   string `json:"title"`
	Path    string `json:"path"`
}

func GetRunningApps() ([]AppInfo, error) {
	return []AppInfo{}, nil
}

func GetInstalledApps() ([]AppInfo, error) {
	return []AppInfo{}, nil
}

func GetAllApps() ([]AppInfo, error) {
	return []AppInfo{}, nil
}
