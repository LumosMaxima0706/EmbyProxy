package config

import (
	"strings"
	"testing"
)

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

func TestListenAddressesPreserveLegacyDefaults(t *testing.T) {
	clearListenEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr() != "0.0.0.0:8787" || cfg.AdminAddr() != "" || cfg.ProxyPort() != 8787 {
		t.Fatalf("proxy=%q admin=%q port=%d", cfg.Addr(), cfg.AdminAddr(), cfg.ProxyPort())
	}
}

func TestListenAddrOverridesHostAndPort(t *testing.T) {
	clearListenEnv(t)
	t.Setenv("LISTEN_ADDR", "127.0.0.1:19080")
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("PORT", "9999")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr() != "127.0.0.1:19080" || cfg.ProxyPort() != 19080 {
		t.Fatalf("proxy=%q port=%d", cfg.Addr(), cfg.ProxyPort())
	}
}

func TestHostAndPortConfigureProxyListener(t *testing.T) {
	clearListenEnv(t)
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "19080")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr() != "127.0.0.1:19080" {
		t.Fatalf("proxy=%q", cfg.Addr())
	}
}

func TestAdminListenAddrOverridesAdminHostAndPorts(t *testing.T) {
	clearListenEnv(t)
	t.Setenv("LISTEN_ADDR", "127.0.0.1:19080")
	t.Setenv("ADMIN_LISTEN_ADDR", "127.0.0.1:19081")
	t.Setenv("ADMIN_HOST", "0.0.0.0")
	t.Setenv("ADMIN_PORT", "9998")
	t.Setenv("DYNDNS_PORT", "9999")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAddr() != "127.0.0.1:19081" {
		t.Fatalf("admin=%q", cfg.AdminAddr())
	}
}

func TestAdminHostSupportsAdminAndLegacyDyndnsPorts(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
	}{
		{name: "admin port", key: "ADMIN_PORT"},
		{name: "legacy dyndns port", key: "DYNDNS_PORT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearListenEnv(t)
			t.Setenv("ADMIN_HOST", "127.0.0.1")
			t.Setenv(test.key, "19081")
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AdminAddr() != "127.0.0.1:19081" {
				t.Fatalf("admin=%q", cfg.AdminAddr())
			}
		})
	}
}

func TestLegacyDyndnsPortAloneDoesNotEnableAdminListener(t *testing.T) {
	clearListenEnv(t)
	t.Setenv("DYNDNS_PORT", "19081")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAddr() != "" {
		t.Fatalf("admin=%q", cfg.AdminAddr())
	}
}

func TestInvalidListenAddressesFailFastWithoutLeakingCredentials(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing port", key: "LISTEN_ADDR", value: "127.0.0.1"},
		{name: "invalid port", key: "LISTEN_ADDR", value: "127.0.0.1:not-a-port"},
		{name: "zero port", key: "LISTEN_ADDR", value: "127.0.0.1:0"},
		{name: "empty host", key: "ADMIN_LISTEN_ADDR", value: ":19081"},
		{name: "invalid legacy port", key: "PORT", value: "not-a-port"},
		{name: "out of range legacy port", key: "PORT", value: "65536"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearListenEnv(t)
			const credential = "credential-must-not-appear"
			t.Setenv("ADMIN_TOKEN", credential)
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil {
				t.Fatal("expected invalid listen address error")
			}
			if strings.Contains(err.Error(), credential) {
				t.Fatalf("error leaked credential: %q", err)
			}
		})
	}
}

func TestSeparateListenersCannotUseSameAddress(t *testing.T) {
	clearListenEnv(t)
	t.Setenv("LISTEN_ADDR", "127.0.0.1:19080")
	t.Setenv("ADMIN_LISTEN_ADDR", "127.0.0.1:19080")
	if _, err := Load(); err == nil {
		t.Fatal("expected duplicate listener error")
	}
}

func clearListenEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"LISTEN_ADDR", "HOST", "PORT", "ADMIN_LISTEN_ADDR", "ADMIN_HOST", "ADMIN_PORT", "DYNDNS_PORT"} {
		t.Setenv(key, "")
	}
}
