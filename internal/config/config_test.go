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

func TestLoadReadsOwnerAdminBasicOnlyMode(t *testing.T) {
	t.Setenv("OWNER_ADMIN_AUTH_MODE", "BASIC_ONLY")
	t.Setenv("OWNER_ADMIN_HOST", "Owner-Admin.Example")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OwnerAdminAuthMode != "basic_only" || cfg.OwnerAdminHost != "owner-admin.example" {
		t.Fatalf("owner Admin mode=%q host=%q", cfg.OwnerAdminAuthMode, cfg.OwnerAdminHost)
	}
}

func TestOwnerAdminBasicOnlyRequiresExactHost(t *testing.T) {
	for _, host := range []string{"", "owner-admin.example:443", "owner-admin.example/path"} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("OWNER_ADMIN_AUTH_MODE", "basic_only")
			t.Setenv("OWNER_ADMIN_HOST", host)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted unsafe owner Admin host %q", host)
			}
		})
	}
}

func TestLoadReadsPublicMediaURLConfiguration(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://stream.example/")
	t.Setenv("PUBLIC_MEDIA_NODE_PATHS_JSON", `{"uhd":"/https/media.example/443/"}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicMediaBaseURL != "https://stream.example" || cfg.PublicMediaNodePaths["uhd"] != "/https/media.example/443/" {
		t.Fatalf("public media config = %q %+v", cfg.PublicMediaBaseURL, cfg.PublicMediaNodePaths)
	}
}

func TestPublicMediaURLConfigurationFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		base  string
		paths string
	}{
		{name: "http base", base: "http://stream.example"},
		{name: "base path", base: "https://stream.example/media"},
		{name: "base query", base: "https://stream.example?token=value"},
		{name: "userinfo", base: "https://user@stream.example"},
		{name: "invalid json", base: "https://stream.example", paths: "uhd=/uhd"},
		{name: "relative path", base: "https://stream.example", paths: `{"uhd":"uhd"}`},
		{name: "path query", base: "https://stream.example", paths: `{"uhd":"/uhd?token=value"}`},
		{name: "protocol relative", base: "https://stream.example", paths: `{"uhd":"//other.example/uhd"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PUBLIC_MEDIA_BASE_URL", tt.base)
			t.Setenv("PUBLIC_MEDIA_NODE_PATHS_JSON", tt.paths)
			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted unsafe public media configuration")
			}
		})
	}
}

func TestPublicMediaBaseRejectsOwnerAdminHost(t *testing.T) {
	t.Setenv("OWNER_ADMIN_AUTH_MODE", "basic_only")
	t.Setenv("OWNER_ADMIN_HOST", "owner-admin.example")
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://owner-admin.example")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted owner Admin as the public media base")
	}
}

func TestMediaProxyRoutesUnknownValuesFailClosed(t *testing.T) {
	for _, value := range []string{"enabled", "maybe", "2", "unexpected"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MEDIAPROXY_ROUTES_ENABLED", value)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MediaProxyRoutes {
				t.Fatalf("unknown feature flag value %q enabled managed routes", value)
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

func TestFailoverDNSGuardEnvironmentDefaultsFailClosedAndReadsExplicitValues(t *testing.T) {
	clearListenEnv(t)
	for _, key := range []string{"FAILOVER_DNS_PROVIDER_MODE", "FAILOVER_DNS_ALLOWED_RECORDS", "FAILOVER_DNS_REAL_APPLY_ENABLED", "FAILOVER_MOCK_FIXTURE_ENABLED"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailoverDNSProviderMode != "" || cfg.FailoverDNSAllowedRecords != "" || cfg.FailoverDNSRealApply || cfg.FailoverMockFixture {
		t.Fatalf("default DNS guard mode=%q allowlist=%q real=%v fixture=%v", cfg.FailoverDNSProviderMode, cfg.FailoverDNSAllowedRecords, cfg.FailoverDNSRealApply, cfg.FailoverMockFixture)
	}
	t.Setenv("FAILOVER_DNS_PROVIDER_MODE", " MOCK ")
	t.Setenv("FAILOVER_DNS_ALLOWED_RECORDS", " stream.example:A ")
	t.Setenv("FAILOVER_DNS_REAL_APPLY_ENABLED", "true")
	t.Setenv("FAILOVER_MOCK_FIXTURE_ENABLED", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailoverDNSProviderMode != "mock" || cfg.FailoverDNSAllowedRecords != "stream.example:A" || !cfg.FailoverDNSRealApply || !cfg.FailoverMockFixture {
		t.Fatalf("explicit DNS guard config mode=%q allowlist=%q real=%v fixture=%v", cfg.FailoverDNSProviderMode, cfg.FailoverDNSAllowedRecords, cfg.FailoverDNSRealApply, cfg.FailoverMockFixture)
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
