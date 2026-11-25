package host

import "testing"

func TestWindowsBuildSupported(t *testing.T) {
	for _, tc := range []struct {
		build   uint32
		edition string
		want    bool
	}{
		{19045, "Core", true},
		{19045, "Professional", true},
		{26200, "Core", true},
		{19044, "EnterpriseS", true},
		{19044, "EnterpriseSN", true},
		{19044, "IoTEnterpriseS", true},
		{19044, "Professional", false},
		{19044, "Enterprise", false},
		{19044, "", false},
		{19043, "EnterpriseS", false},
		{17763, "EnterpriseS", false},
	} {
		if got := WindowsBuildSupported(tc.build, tc.edition); got != tc.want {
			t.Errorf("WindowsBuildSupported(%d, %q) = %t, want %t", tc.build, tc.edition, got, tc.want)
		}
	}
}
