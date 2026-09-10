//go:build !product_production

// Package mock provides the contract sample implementations of the
// productclient interfaces: scripted, deterministic, and test-only.
//
// It is excluded from production builds on purpose (`!product_production`).
// A mock that answers Login successfully and reports a healthy balance is the
// same class of hazard as a local model provider: if it can be linked into a
// shipped binary, "the platform said yes" becomes a fact a build flag can
// fabricate. Test binaries and developer builds link it; a release cannot.
//
// The distinction from a `local` implementation is deliberate and load-bearing
// (P0-01 §合同骨架, last bullet): a mock is a *contract sample*, not a local
// business authority. Nothing here persists state, computes a price, or is
// reachable from the production runtime.
package mock

import (
	"context"
	"errors"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// ErrUnscripted is returned by any method whose func field was left nil.
//
// Returning a zero value instead would let a test pass against a stub that
// silently asserts "success with empty data" — the exact shape of a bug that
// only shows up in production. Failing loudly makes the missing script visible.
var ErrUnscripted = errors.New("productclient/mock: method not scripted")

// Auth is a scripted productclient.AuthClient.
type Auth struct {
	SendCodeFunc func(ctx context.Context, phone string) (productclient.Cooldown, error)
	LoginFunc    func(ctx context.Context, req productclient.LoginRequest) (productclient.LoginResult, error)
	RefreshFunc  func(ctx context.Context, refreshToken string) (productclient.Session, error)
	LogoutFunc   func(ctx context.Context) error
}

var _ productclient.AuthClient = (*Auth)(nil)

func (m *Auth) SendCode(ctx context.Context, phone string) (productclient.Cooldown, error) {
	if m.SendCodeFunc == nil {
		return productclient.Cooldown{}, ErrUnscripted
	}
	return m.SendCodeFunc(ctx, phone)
}

func (m *Auth) Login(ctx context.Context, req productclient.LoginRequest) (productclient.LoginResult, error) {
	if m.LoginFunc == nil {
		return productclient.LoginResult{}, ErrUnscripted
	}
	return m.LoginFunc(ctx, req)
}

func (m *Auth) Refresh(ctx context.Context, refreshToken string) (productclient.Session, error) {
	if m.RefreshFunc == nil {
		return productclient.Session{}, ErrUnscripted
	}
	return m.RefreshFunc(ctx, refreshToken)
}

func (m *Auth) Logout(ctx context.Context) error {
	if m.LogoutFunc == nil {
		return ErrUnscripted
	}
	return m.LogoutFunc(ctx)
}

// ControlPlane is a scripted productclient.ControlPlaneClient.
type ControlPlane struct {
	BootstrapFunc           func(ctx context.Context) (productclient.Bootstrap, error)
	ModelsFunc              func(ctx context.Context, knownVersion string) (productclient.CatalogEnvelope, error)
	SensitiveDictionaryFunc func(ctx context.Context, knownVersion string) (productclient.DictionaryEnvelope, error)
	LegalDocumentsFunc      func(ctx context.Context, knownVersion string) (productclient.LegalEnvelope, error)
	AcceptLegalFunc         func(ctx context.Context, req productclient.AcceptLegalRequest) (productclient.LegalReceipt, error)
}

var _ productclient.ControlPlaneClient = (*ControlPlane)(nil)

func (m *ControlPlane) Bootstrap(ctx context.Context) (productclient.Bootstrap, error) {
	if m.BootstrapFunc == nil {
		return productclient.Bootstrap{}, ErrUnscripted
	}
	return m.BootstrapFunc(ctx)
}

func (m *ControlPlane) Models(ctx context.Context, knownVersion string) (productclient.CatalogEnvelope, error) {
	if m.ModelsFunc == nil {
		return productclient.CatalogEnvelope{}, ErrUnscripted
	}
	return m.ModelsFunc(ctx, knownVersion)
}

func (m *ControlPlane) SensitiveDictionary(ctx context.Context, knownVersion string) (productclient.DictionaryEnvelope, error) {
	if m.SensitiveDictionaryFunc == nil {
		return productclient.DictionaryEnvelope{}, ErrUnscripted
	}
	return m.SensitiveDictionaryFunc(ctx, knownVersion)
}

func (m *ControlPlane) LegalDocuments(ctx context.Context, knownVersion string) (productclient.LegalEnvelope, error) {
	if m.LegalDocumentsFunc == nil {
		return productclient.LegalEnvelope{}, ErrUnscripted
	}
	return m.LegalDocumentsFunc(ctx, knownVersion)
}

func (m *ControlPlane) AcceptLegal(ctx context.Context, req productclient.AcceptLegalRequest) (productclient.LegalReceipt, error) {
	if m.AcceptLegalFunc == nil {
		return productclient.LegalReceipt{}, ErrUnscripted
	}
	return m.AcceptLegalFunc(ctx, req)
}

// Usage is a scripted productclient.UsageClient. It is read-only by
// construction: there is no method that could charge.
type Usage struct {
	UsageFunc         func(ctx context.Context) (productclient.Usage, error)
	RequestStatusFunc func(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error)
	LedgerFunc        func(ctx context.Context, query productclient.LedgerQuery) (productclient.LedgerPage, error)
}

var _ productclient.UsageClient = (*Usage)(nil)

func (m *Usage) Usage(ctx context.Context) (productclient.Usage, error) {
	if m.UsageFunc == nil {
		return productclient.Usage{}, ErrUnscripted
	}
	return m.UsageFunc(ctx)
}

func (m *Usage) RequestStatus(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error) {
	if m.RequestStatusFunc == nil {
		return productclient.RequestStatus{}, ErrUnscripted
	}
	return m.RequestStatusFunc(ctx, clientRequestID)
}

func (m *Usage) Ledger(ctx context.Context, query productclient.LedgerQuery) (productclient.LedgerPage, error) {
	if m.LedgerFunc == nil {
		return productclient.LedgerPage{}, ErrUnscripted
	}
	return m.LedgerFunc(ctx, query)
}

// Sessions is a scripted productclient.SessionLister.
type Sessions struct {
	SessionsFunc      func(ctx context.Context) ([]productclient.DeviceSession, error)
	RevokeSessionFunc func(ctx context.Context, sessionID string) error
}

var _ productclient.SessionLister = (*Sessions)(nil)

func (m *Sessions) Sessions(ctx context.Context) ([]productclient.DeviceSession, error) {
	if m.SessionsFunc == nil {
		return nil, ErrUnscripted
	}
	return m.SessionsFunc(ctx)
}

func (m *Sessions) RevokeSession(ctx context.Context, sessionID string) error {
	if m.RevokeSessionFunc == nil {
		return ErrUnscripted
	}
	return m.RevokeSessionFunc(ctx, sessionID)
}
