package config

import "testing"

func TestEndpointsFor(t *testing.T) {
	cases := []struct{ domain, api, auth string }{
		{"zentoris.com", "https://main.api.zentoris.com", "https://auth.api.zentoris.com"},
		{"local.zentoris.com", "https://main.api.local.zentoris.com", "https://auth.api.local.zentoris.com"},
		{"", "https://main.api.zentoris.com", "https://auth.api.zentoris.com"},
		{"Internal.Zentoris.Com", "https://main.api.internal.zentoris.com", "https://auth.api.internal.zentoris.com"},
	}
	for _, c := range cases {
		api, auth := endpointsFor(c.domain)
		if api != c.api || auth != c.auth {
			t.Errorf("endpointsFor(%q) = (%q, %q), want (%q, %q)", c.domain, api, auth, c.api, c.auth)
		}
	}
}

func TestIsLocalDomain(t *testing.T) {
	for _, d := range []string{"local.zentoris.com", "local-01.zentoris.com", "LOCAL.zentoris.com"} {
		if !isLocalDomain(d) {
			t.Errorf("isLocalDomain(%q) = false, want true", d)
		}
	}
	// Must NOT match an arbitrary host merely named `local` outside the trusted domain, or TLS
	// verification would be silently disabled against it.
	for _, d := range []string{"zentoris.com", "internal.zentoris.com", "notlocal.zentoris.com", "local.evil.com", "local-01.attacker.com"} {
		if isLocalDomain(d) {
			t.Errorf("isLocalDomain(%q) = true, want false", d)
		}
	}
}

func TestApplyDomainDerivesBothURLs(t *testing.T) {
	c := &Config{Domain: "internal.zentoris.com"}
	if err := c.ApplyDomain(false, false, false); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	if c.APIBase != "https://main.api.internal.zentoris.com" {
		t.Errorf("APIBase = %q", c.APIBase)
	}
	if c.AuthBase != "https://auth.api.internal.zentoris.com" {
		t.Errorf("AuthBase = %q", c.AuthBase)
	}
}

func TestApplyDomainKeepsExplicitURL(t *testing.T) {
	c := &Config{Domain: "internal.zentoris.com", APIBase: "https://custom.example"}
	if err := c.ApplyDomain(true /* explicitAPI */, false, false); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	if c.APIBase != "https://custom.example" {
		t.Errorf("explicit APIBase overwritten: %q", c.APIBase)
	}
	if c.AuthBase != "https://auth.api.internal.zentoris.com" {
		t.Errorf("AuthBase should still derive: %q", c.AuthBase)
	}
}

func TestApplyDomainLocalDefaultsInsecure(t *testing.T) {
	c := &Config{Domain: "local-01.zentoris.com"}
	if err := c.ApplyDomain(false, false, false); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	if !c.Insecure {
		t.Error("a local domain should default Insecure=true")
	}

	// An explicit insecure choice is preserved rather than being recomputed.
	c = &Config{Domain: "zentoris.com", Insecure: true}
	if err := c.ApplyDomain(false, false, true /* explicitInsecure */); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	if !c.Insecure {
		t.Error("explicit Insecure=true should be preserved")
	}
}

func TestApplyDomainRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"https://foo.com", "foo.com/bar", "foo.com:8443", "has space", ""} {
		c := &Config{Domain: bad}
		if err := c.ApplyDomain(false, false, false); err == nil {
			t.Errorf("ApplyDomain(domain=%q) = nil error, want a validation error", bad)
		}
	}
}

func TestLoadDefaultsToHostedPlatform(t *testing.T) {
	for _, k := range []string{"ZENTORIS_DOMAIN", "ZENTORIS_API", "ZENTORIS_AUTH", "ZENTORIS_INSECURE"} {
		t.Setenv(k, "")
	}
	cfg := Load()
	if cfg.APIBase != "https://main.api.zentoris.com" || cfg.AuthBase != "https://auth.api.zentoris.com" {
		t.Errorf("hosted defaults wrong: api=%q auth=%q", cfg.APIBase, cfg.AuthBase)
	}
	if cfg.Insecure {
		t.Error("hosted default must not skip TLS verification")
	}
}

func TestLoadDomainEnvDerivesURLs(t *testing.T) {
	t.Setenv("ZENTORIS_API", "")
	t.Setenv("ZENTORIS_AUTH", "")
	t.Setenv("ZENTORIS_INSECURE", "")
	t.Setenv("ZENTORIS_DOMAIN", "local.zentoris.com")
	cfg := Load()
	if cfg.APIBase != "https://main.api.local.zentoris.com" {
		t.Errorf("APIBase = %q", cfg.APIBase)
	}
	if !cfg.Insecure {
		t.Error("local domain via ZENTORIS_DOMAIN should default insecure")
	}
}
