package config

import "testing"

func TestEnvBoolParsesAdmin2FADisabledValues(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: "true", want: true},
		{value: "1", want: true},
		{value: "on", want: true},
		{value: "false", want: false},
		{value: "0", want: false},
		{value: "", want: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("ADMIN_2FA_DISABLED", tt.value)
			if got := envBool("ADMIN_2FA_DISABLED", false); got != tt.want {
				t.Fatalf("envBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMediaProxyRoutesDisabledByDefault(t *testing.T) {
	t.Setenv("MEDIAPROXY_ROUTES_ENABLED", "")
	if got := envBool("MEDIAPROXY_ROUTES_ENABLED", false); got {
		t.Fatal("mediaproxy production routes must default to disabled")
	}
}

func TestLoadReadsMediaProxyRoutesFlag(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "enabled", value: "true", want: true},
		{name: "disabled", value: "false", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MEDIAPROXY_ROUTES_ENABLED", tt.value)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MediaProxyRoutes != tt.want {
				t.Fatalf("MediaProxyRoutes=%v, want %v", cfg.MediaProxyRoutes, tt.want)
			}
		})
	}
}
