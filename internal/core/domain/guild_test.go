package domain_test

import (
	"errors"
	"testing"

	"github.com/dwang288/sfw-sasuke/internal/core/domain"
)

func quota(n int64) *int64 { return &n }

func TestGuildSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		s       domain.GuildSettings
		wantErr error // nil means valid
	}{
		{
			name: "valid manage_guild, no quota",
			s:    domain.GuildSettings{GuildID: "g", UploadPolicy: domain.UploadPolicyManageGuild},
		},
		{
			name: "valid everyone, zero quota",
			s:    domain.GuildSettings{GuildID: "g", UploadPolicy: domain.UploadPolicyEveryone, StorageQuota: quota(0)},
		},
		{
			name:    "unknown policy",
			s:       domain.GuildSettings{GuildID: "g", UploadPolicy: "root"},
			wantErr: domain.ErrInvalidUploadPolicy,
		},
		{
			name:    "empty policy",
			s:       domain.GuildSettings{GuildID: "g"},
			wantErr: domain.ErrInvalidUploadPolicy,
		},
		{
			name:    "negative quota",
			s:       domain.GuildSettings{GuildID: "g", UploadPolicy: domain.UploadPolicyEveryone, StorageQuota: quota(-1)},
			wantErr: domain.ErrInvalidGuildSettings,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate: got %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate: got %v, want errors.Is %v", err, tc.wantErr)
			}
		})
	}
}

func TestUploadPolicyValid(t *testing.T) {
	valid := []domain.UploadPolicy{domain.UploadPolicyManageGuild, domain.UploadPolicyEveryone}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("Valid(%q): got false, want true", p)
		}
	}
	for _, p := range []domain.UploadPolicy{"", "admin", "MANAGE_GUILD"} {
		if p.Valid() {
			t.Errorf("Valid(%q): got true, want false", p)
		}
	}
}

func TestDefaultGuildSettings(t *testing.T) {
	got := domain.DefaultGuildSettings("g")
	if got.GuildID != "g" {
		t.Errorf("GuildID: got %q, want g", got.GuildID)
	}
	if got.UploadPolicy != domain.UploadPolicyManageGuild {
		t.Errorf("UploadPolicy: got %q, want manage_guild", got.UploadPolicy)
	}
	if got.StorageQuota != nil {
		t.Errorf("StorageQuota: got %v, want nil (unlimited)", *got.StorageQuota)
	}
	// The default must itself be valid.
	if err := got.Validate(); err != nil {
		t.Errorf("DefaultGuildSettings should be valid, got %v", err)
	}
}
