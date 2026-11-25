package smtp

import (
	"testing"
)

func TestAuthorizedPolicy(t *testing.T) {
	backend := &Backend{
		allowedDomains:   map[string]struct{}{"contoso.com": {}},
		policyConfigured: true,
		maxBytes:         1024,
	}
	s := &session{backend: backend}
	if !s.authorized("postmaster@contoso.com") {
		t.Fatal("allowed journal sender rejected")
	}
	if s.authorized("attacker@evil.example") {
		t.Fatal("disallowed sender accepted")
	}
	if s.authorized("") {
		t.Fatal("empty sender accepted with domain policy")
	}
}

func TestNoPolicyRejects(t *testing.T) {
	backend := &Backend{allowedDomains: map[string]struct{}{}, maxBytes: 1024}
	s := &session{backend: backend}
	if s.authorized("postmaster@contoso.com") {
		t.Fatal("mail accepted without a sender policy")
	}
}

func TestAuthRequiredWithoutTrustedIP(t *testing.T) {
	backend := &Backend{
		allowedDomains:   map[string]struct{}{"contoso.com": {}},
		policyConfigured: true,
		authUsername:     "journal",
		authPassword:     "secret",
		maxBytes:         1024,
	}
	s := &session{backend: backend}
	if s.authorized("postmaster@contoso.com") {
		t.Fatal("unauthenticated sender accepted when AUTH is configured without a trusted CIDR")
	}
	s.authenticated = true
	if !s.authorized("postmaster@contoso.com") {
		t.Fatal("authenticated sender rejected")
	}
}
