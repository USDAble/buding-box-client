package productclient

import "context"

// The interfaces in this file are the frozen contract named by P0-01
// §合同骨架. Two rules keep them from drifting:
//
//  1. Method signatures are defined here and nowhere else. Other documents
//     (including 产品客户端与中台对接/开发计划) reference this file rather than
//     restating it, because two copies of a signature means two owners write
//     incompatible code against the same name.
//  2. DTO field names, the error envelope and every `code` value come from
//     中台交付包 §3.2. This file does not repeat the code table.
//
// Implementations are `remote` (real HTTP, added in P0-01 B1) and `mock`
// (contract samples, this package's subpackage). A `local` implementation that
// serves real data is explicitly out of scope: the central platform is the only
// authority for identity, catalog, policy and balance.

// AuthClient covers phone + SMS login and its token lifecycle.
//
// The install identifier and client-version headers are injected by the shared
// transport, not passed per call (P0-01 §2), so they do not appear here.
type AuthClient interface {
	SendCode(ctx context.Context, phone string) (Cooldown, error)
	Login(ctx context.Context, req LoginRequest) (LoginResult, error)
	Refresh(ctx context.Context, refreshToken string) (Session, error)
	Logout(ctx context.Context) error
}

// ControlPlaneClient covers the signed configuration the client runs on.
//
// The `knownVersion` argument on the three GETs is the condition-request key:
// passing the version already held lets the platform answer "unchanged"
// instead of reissuing an identical envelope (中台交付包 §4.3).
type ControlPlaneClient interface {
	Bootstrap(ctx context.Context) (Bootstrap, error)
	Models(ctx context.Context, knownVersion string) (CatalogEnvelope, error)
	SensitiveDictionary(ctx context.Context, knownVersion string) (DictionaryEnvelope, error)
	LegalDocuments(ctx context.Context, knownVersion string) (LegalEnvelope, error)
	AcceptLegal(ctx context.Context, req AcceptLegalRequest) (LegalReceipt, error)
}

// UsageClient is strictly read-only. The gateway owns debit and settlement; the
// client never submits an amount, and there is no `Consume` method anywhere in
// this package or its subpackages — the settled charge is whatever the platform
// reported, and the client's only recourse after a broken stream is to ask.
type UsageClient interface {
	Usage(ctx context.Context) (Usage, error)
	RequestStatus(ctx context.Context, clientRequestID string) (RequestStatus, error)
	Ledger(ctx context.Context, query LedgerQuery) (LedgerPage, error)
}

// SessionLister is the optional device-session surface (中台交付包 §4.2.4). It
// is separate from AuthClient so a caller that only needs login does not depend
// on session revocation, and so the two can be owned by different PRs.
type SessionLister interface {
	Sessions(ctx context.Context) ([]DeviceSession, error)
	RevokeSession(ctx context.Context, sessionID string) error
}
