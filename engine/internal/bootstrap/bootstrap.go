// Package bootstrap guarantees the default tenant, tenant KEK, journal source
// and initial administrator exist before the engine serves traffic.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/auth"
	"github.com/bangsmackpow/pocketarchive/engine/internal/config"
	"github.com/bangsmackpow/pocketarchive/engine/internal/crypto"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Result struct {
	Tenant       store.Tenant
	AdminCreated bool
	AdminEmail   string
	NeedsAdmin   bool
}

func Ensure(ctx context.Context, st *store.Store, master *crypto.MasterKey, cfg config.Config) (Result, error) {
	tenant, err := st.GetTenantBySlug(ctx, cfg.DefaultTenantSlug)
	if errors.Is(err, store.ErrNotFound) {
		kek, err := crypto.NewDataKey()
		if err != nil {
			return Result{}, err
		}
		// The tenant ID is needed as AAD, so generate it before the insert.
		id := uuid.New()
		wrapped, err := master.WrapKEK(id, kek)
		if err != nil {
			return Result{}, fmt.Errorf("wrap tenant kek: %w", err)
		}
		tenant, err = st.CreateTenantWithID(ctx, id, cfg.DefaultTenantSlug, cfg.DefaultTenantName, cfg.DefaultRetentionDays, wrapped)
		if err != nil {
			// Another instance may have won the race.
			tenant, err = st.GetTenantBySlug(ctx, cfg.DefaultTenantSlug)
			if err != nil {
				return Result{}, err
			}
		}
	} else if err != nil {
		return Result{}, err
	}

	if len(tenant.WrappedKEK) == 0 {
		kek, err := crypto.NewDataKey()
		if err != nil {
			return Result{}, err
		}
		wrapped, err := master.WrapKEK(tenant.ID, kek)
		if err != nil {
			return Result{}, err
		}
		if err := st.SetTenantKEK(ctx, tenant.ID, wrapped); err != nil {
			return Result{}, err
		}
		tenant.WrappedKEK = wrapped
	}

	result := Result{Tenant: tenant}
	count, err := st.CountUsers(ctx, tenant.ID)
	if err != nil {
		return Result{}, err
	}
	if count == 0 && cfg.BootstrapAdminEmail != "" && cfg.BootstrapAdminPass != "" {
		hash, err := auth.HashPassword(cfg.BootstrapAdminPass)
		if err != nil {
			return Result{}, fmt.Errorf("bootstrap admin password: %w", err)
		}
		_, err = st.CreateUser(ctx, store.User{
			TenantID:     tenant.ID,
			Email:        cfg.BootstrapAdminEmail,
			PasswordHash: hash,
			Role:         "admin",
		})
		if err != nil {
			return Result{}, err
		}
		result.AdminCreated = true
		result.AdminEmail = cfg.BootstrapAdminEmail
	} else if count == 0 {
		result.NeedsAdmin = true
	}

	if err := st.EnsureDefaultSource(ctx, tenant.ID, "Office 365 Journal"); err != nil {
		return Result{}, err
	}
	return result, nil
}
