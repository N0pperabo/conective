//go:build windows

package tun

import (
	"testing"
)

func TestIsWintunDevice(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		desc       string
		want       bool
	}{
		{
			name:       "Exact Xray Tunnel with Wintun GUID",
			instanceID: `SWD\Wintun\{ADC74C92-7FE4-4B8A-2947-49A81E662917}`,
			desc:       "Xray Tunnel",
			want:       true,
		},
		{
			name:       "FreeNodeTUN adapter",
			instanceID: `SWD\Wintun\{12345678-ABCD-EF01-2345-6789ABCDEF01}`,
			desc:       "FreeNodeTUN",
			want:       true,
		},
		{
			name:       "ConectiveTUN adapter",
			instanceID: `SWD\Wintun\{87654321-ABCD-EF01-2345-6789ABCDEF01}`,
			desc:       "ConectiveTUN",
			want:       true,
		},
		{
			name:       "Wintun Userspace Tunnel",
			instanceID: `SWD\Wintun\{98765432-FEDC-BA98-7654-3210FEDCBA98}`,
			desc:       "Wintun Userspace Tunnel",
			want:       true,
		},
		{
			name:       "Case insensitive lowercase swd\\wintun and xray tunnel",
			instanceID: `swd\wintun\{abcdef01-2345-6789}`,
			desc:       "xray tunnel",
			want:       true,
		},
		{
			name:       "Case insensitive uppercase SWD\\WINTUN and FREENODE",
			instanceID: `SWD\WINTUN\{ABCDEF01-2345-6789}`,
			desc:       "FREENODE TUNNEL",
			want:       true,
		},
		{
			name:       "WireSock adapter should NOT match",
			instanceID: `ROOT\NET\0000`,
			desc:       "WireSock Virtual Adapter",
			want:       false,
		},
		{
			name:       "WAN Miniport should NOT match",
			instanceID: `SWD\MSRRAS\MS_PPPOEMINIPORT`,
			desc:       "WAN Miniport (PPPOE)",
			want:       false,
		},
		{
			name:       "Intel Wireless adapter should NOT match",
			instanceID: `PCI\VEN_8086&DEV_02F0&SUBSYS_02348086&REV_00\3&11583659&0&A3`,
			desc:       "Intel(R) Wireless-AC 9560",
			want:       false,
		},
		{
			name:       "TAP-Windows adapter should NOT match",
			instanceID: `ROOT\NET\0002`,
			desc:       "TAP-Windows Adapter V9",
			want:       false,
		},
		{
			name:       "Wintun ID prefix but non-matching description",
			instanceID: `SWD\Wintun\{11112222-3333-4444}`,
			desc:       "Unrelated Virtual Adapter",
			want:       false,
		},
		{
			name:       "Matching description but non-Wintun ID prefix",
			instanceID: `PCI\VEN_10EC&DEV_8168`,
			desc:       "Xray Tunnel",
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsWintunDevice(tc.instanceID, tc.desc)
			if got != tc.want {
				t.Errorf("IsWintunDevice(%q, %q) = %v; want %v", tc.instanceID, tc.desc, got, tc.want)
			}
		})
	}
}

func TestParseWintunDevices(t *testing.T) {
	mockPnpOutput := `
Microsoft PnP Utility

Instance ID:                {5d624f94-8850-40c3-a3fa-a4fd2080baf3}\vwifimp_wfd\4&2996a1a9&1&13
Device Description:         Microsoft Wi-Fi Direct Virtual Adapter
Class Name:                 Net
Status:                     Started

Instance ID:                ROOT\NET\0000
Device Description:         WireSock Virtual Adapter
Class Name:                 Net
Status:                     Started

Instance ID:                SWD\Wintun\{ADC74C92-7FE4-4B8A-2947-49A81E662917}
Device Description:         Xray Tunnel
Class Name:                 Net
Status:                     Started

Instance ID:                ROOT\NET\0002
Device Description:         TAP-Windows Adapter V9
Class Name:                 Net
Status:                     Started

Instance ID:                SWD\Wintun\{11112222-3333-4444-5555-666677778888}
Device Description:         FreeNodeTUN
Class Name:                 Net
Status:                     Started

Instance ID:                PCI\VEN_8086&DEV_02F0&SUBSYS_02348086&REV_00\3&11583659&0&A3
Device Description:         Intel(R) Wireless-AC 9560
Class Name:                 Net
Status:                     Started

Instance ID:                SWD\Wintun\{99998888-7777-6666-5555-444433332222}
Device Description:         Wintun Userspace Tunnel
Class Name:                 Net
Status:                     Disconnected
`

	matched := ParseWintunDevices(mockPnpOutput)
	expected := []string{
		`SWD\Wintun\{ADC74C92-7FE4-4B8A-2947-49A81E662917}`,
		`SWD\Wintun\{11112222-3333-4444-5555-666677778888}`,
		`SWD\Wintun\{99998888-7777-6666-5555-444433332222}`,
	}

	if len(matched) != len(expected) {
		t.Fatalf("ParseWintunDevices returned %d devices; want %d (%v)", len(matched), len(expected), matched)
	}

	for i, id := range matched {
		if id != expected[i] {
			t.Errorf("device[%d] = %q; want %q", i, id, expected[i])
		}
	}
}

func TestManagerLifecycle(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.IsActive() {
		t.Error("expected new manager to be inactive")
	}

	// TeardownAdapter on unconfigured manager should succeed cleanly
	if err := mgr.TeardownAdapter(); err != nil {
		t.Errorf("TeardownAdapter failed: %v", err)
	}

	if mgr.IsActive() {
		t.Error("expected manager to remain inactive after teardown")
	}
}

func TestRecoverOrphanedTUNRoutes(t *testing.T) {
	mgr := NewManager()
	// Should execute without panicking
	mgr.RecoverOrphanedTUNRoutes()
}

func TestCleanupStaleWintunAdapters(t *testing.T) {
	// Should execute safely without panic even in non-elevated testing environment
	CleanupStaleWintunAdapters()
}

func TestIsStaleAdapterName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Single canonical name", "ConectiveTUN", false},
		{"Lowercase canonical name", "conectivetun", false},
		{"Numbered candidate 2", "ConectiveTUN2", true},
		{"Numbered candidate 3", "ConectiveTUN3", true},
		{"Candidate with space", "ConectiveTUN 2", true},
		{"Legacy FreeNode name", "FreeNodeTUN", true},
		{"Legacy FreeNode numbered", "FreeNodeTUN2", true},
		{"Arbitrary freenode prefix", "freenode_adapter", true},
		{"Physical Wi-Fi interface", "Wi-Fi", false},
		{"Physical Ethernet interface", "Ethernet", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsStaleAdapterName(tc.input)
			if got != tc.expected {
				t.Errorf("IsStaleAdapterName(%q) = %v; want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestResetOrRemoveAdapterSafe(t *testing.T) {
	// Calling ResetOrRemoveAdapter for non-existent adapter should execute safely
	_ = ResetOrRemoveAdapter("NonExistentAdapter")
}

func TestDeterministicWintunGUID(t *testing.T) {
	guid := DeterministicWintunGUID("ConectiveTUN")
	expected := "{D41FBB06-50D6-C889-B757-971E81B1495D}"
	if guid != expected {
		t.Errorf("DeterministicWintunGUID(\"ConectiveTUN\") = %s; want %s", guid, expected)
	}
}

