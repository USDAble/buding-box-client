package productclient

// This file is the client's half of the contract in 中台交付包 §4. Field names
// are quoted from that document verbatim; where the document shows a value in
// two places under two names (`id` in the catalog, `modelId` in the gateway
// request) the two structs keep both names on purpose — they are the same value
// in two different messages, not two fields to be unified (开发规范 §3.8).
//
// Every type here is a wire DTO. None of them is the product's own state: the
// client stores no authoritative balance, plan or activation, and nothing in
// this package computes a price.

// Account is the minimal account summary. There is deliberately no plaintext
// phone field — the platform only ever sends the masked form.
type Account struct {
	ID          string `json:"id,omitempty"`
	PhoneMasked string `json:"phoneMasked"`
	Nickname    string `json:"nickname"`
}

// ActivationStatus values seen in the contract. An unrecognised value must be
// treated as "not active" (fail-closed), never as active.
const (
	ActivationActive  = "active"
	ActivationExpired = "expired"
)

// Activation is the 365-day first-activation grant. v1 displays expiry but does
// not block chat on it (中台交付包 §1.2) — that decision belongs to the product
// layer, not here.
type Activation struct {
	Status      string `json:"status"`
	ActivatedAt string `json:"activatedAt,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// Plan is the entitlement bundle. It is a separate concept from Activation and
// from credits; collapsing the three is exactly the defect §1.2 calls out.
type Plan struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	ExpiresAt    *string  `json:"expiresAt"`
	Entitlements []string `json:"entitlements"`
}

// Allows reports whether the plan grants an entitlement. Absence is denial:
// an unknown or empty list never widens access.
func (p Plan) Allows(entitlement string) bool {
	for _, e := range p.Entitlements {
		if e == entitlement {
			return true
		}
	}
	return false
}

// Credits is a read-only balance snapshot. Amounts are integer microCredits —
// never floats (中台交付包 §3.1). The client displays this value; it never
// derives, rounds or adjusts it.
type Credits struct {
	BalanceMicroCredits int64  `json:"balanceMicroCredits"`
	Currency            string `json:"currency"`
	UpdatedAt           string `json:"updatedAt"`
}

// TokenUsage breaks tokens out by billing class. `UsageSource` distinguishes a
// provider-reported figure from a platform estimate, because an estimate may
// still be reconciled later (中台交付包 §5.1).
type TokenUsage struct {
	InputTokens       int64  `json:"inputTokens"`
	CachedInputTokens int64  `json:"cachedInputTokens"`
	OutputTokens      int64  `json:"outputTokens"`
	ReasoningTokens   int64  `json:"reasoningTokens"`
	UsageSource       string `json:"usageSource"`
}

// Usage source values. `estimated` means the ledger figure is provisional and
// the UI must not present it as final.
const (
	UsageSourceProvider  = "provider"
	UsageSourceEstimated = "estimated"
)

// Models transport values. The catalog may only ever advertise `gateway`: any
// other value means the platform is trying to hand the client a direct supplier
// endpoint, which is a protocol error (中台交付包 §4.3).
const (
	TransportGateway = "gateway"
)

// ModelCapabilities is what a model can do, as asserted by the platform. It is
// not a permission — permission comes from the policy envelope.
type ModelCapabilities struct {
	Vision bool `json:"vision"`
	Tools  bool `json:"tools"`
	Stream bool `json:"stream"`
}

// Model is one catalog entry. It carries no supplier connection information:
// the forbidden fields are `baseUrl`, `apiKey` and any directly usable
// endpoint (中台交付包 §4.3).
//
// DisplayName is the only source for a user-visible model name. The client
// renders it verbatim and keeps no local id→name table, so the platform can
// rename a model without a client release.
type Model struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"displayName"`
	ModeIDs          []string          `json:"modeIds"`
	Transport        string            `json:"transport"`
	Capabilities     ModelCapabilities `json:"capabilities"`
	MaxContextTokens int               `json:"maxContextTokens"`
	MaxOutputTokens  int               `json:"maxOutputTokens"`
	Eligible         bool              `json:"eligible"`
	Default          bool              `json:"default,omitempty"`
	PricingVersion   string            `json:"pricingVersion"`
}

// Usable reports whether the client may offer this model. It requires both the
// gateway transport and eligibility; a model that fails either test is not
// shown as selectable, and the local endpoint code path is not a fallback.
func (m Model) Usable() bool {
	return m.Eligible && m.Transport == TransportGateway && m.ID != ""
}

// Capability is one row of the capability matrix. The four booleans answer
// different questions and must not be conflated (中台交付包 §4.3):
// available = the code exists, visible = the menu shows it,
// entitled = the account may start it, and permissionPolicy governs the
// runtime decision.
type Capability struct {
	ID               string `json:"id"`
	Available        bool   `json:"available"`
	Visible          bool   `json:"visible"`
	Entitled         bool   `json:"entitled"`
	PermissionPolicy string `json:"permissionPolicy"`
	MinClientVersion string `json:"minClientVersion"`
}

// Permission policy values.
const (
	PermissionDeny  = "deny"
	PermissionAsk   = "ask"
	PermissionAllow = "allow"
)

// Catalog is the signed model list plus its own validity window. The client
// caches the raw envelope, not this decoded struct, so it can re-verify later
// (P0-04 §模型目录的本地缓存与刷新).
type Catalog struct {
	Version string  `json:"version"`
	TTLSec  int     `json:"ttlSec"`
	Models  []Model `json:"models"`
	Modes   []Mode  `json:"modes"`
}

// Mode is a named model group. The product's three mode ids are fixed in
// internal/chatmode; the catalog supplies which models belong to each and which
// one is the default. Members outside the current catalog are dropped by the
// projection, never displayed.
type Mode struct {
	ID           string   `json:"id"`
	Models       []Model  `json:"models,omitempty"`
	ModelIDs     []string `json:"modelIds,omitempty"`
	DefaultModel string   `json:"defaultModel,omitempty"`
}

// PolicyEnvelope is the signed wrapper around catalog and capabilities. The
// signature is over the JCS-normalised payload *without* the signature field
// (中台交付包 §3.1).
//
// PolicyVersion lives here and only here. There is deliberately no second copy
// on Bootstrap: a field outside the signed envelope can be rewritten in transit
// while verification still passes, which would defeat both version
// monotonicity and policy revocation.
type PolicyEnvelope struct {
	PolicyVersion    string       `json:"policyVersion"`
	IssuedAt         string       `json:"issuedAt"`
	ExpiresAt        string       `json:"expiresAt"`
	MinClientVersion string       `json:"minClientVersion"`
	Audience         string       `json:"audience"`
	KeyID            string       `json:"keyId"`
	Catalog          Catalog      `json:"catalog"`
	Capabilities     []Capability `json:"capabilities"`
	Signature        string       `json:"signature"`
}

// CatalogEnvelope is what `GET /catalog/models` returns: the same signed policy
// payload, so a refresh cannot deliver a catalog under weaker rules than
// bootstrap did.
type CatalogEnvelope struct {
	Policy PolicyEnvelope `json:"policy"`
}

// DictionaryEnvelope is the signed sensitive-word delta. It is versioned and
// signed independently of the policy so that a dictionary outage cannot break
// the menu, and vice versa (P0-04 §发给中台的控制面交付要求 4).
type DictionaryEnvelope struct {
	Version   string   `json:"version"`
	KeyID     string   `json:"keyId"`
	IssuedAt  string   `json:"issuedAt"`
	ExpiresAt string   `json:"expiresAt"`
	Audience  string   `json:"audience"`
	Added     []string `json:"added"`
	Removed   []string `json:"removed"`
	Signature string   `json:"signature"`
}

// LegalDocument is one approved agreement or notice. The client never composes
// or edits this text, and only ever publishes versions the platform approved.
type LegalDocument struct {
	Type               string  `json:"type"`
	Version            string  `json:"version"`
	EffectiveAt        string  `json:"effectiveAt"`
	Title              string  `json:"title"`
	Summary            string  `json:"summary"`
	ContentURL         string  `json:"contentUrl,omitempty"`
	Content            string  `json:"content,omitempty"`
	ContentSHA256      string  `json:"contentSha256"`
	RequiresAcceptance bool    `json:"requiresAcceptance"`
	AcceptedAt         *string `json:"acceptedAt"`
}

// LegalEnvelope wraps the legal documents. It is signed like the others: an
// unsigned notice could be swapped for a different one, which is the attack
// this envelope exists to stop.
type LegalEnvelope struct {
	Version   string          `json:"version"`
	KeyID     string          `json:"keyId"`
	IssuedAt  string          `json:"issuedAt"`
	ExpiresAt string          `json:"expiresAt"`
	Audience  string          `json:"audience"`
	Documents []LegalDocument `json:"documents"`
	Signature string          `json:"signature"`
}

// AcceptLegalRequest records one acceptance. It is only sent when the product
// requires explicit consent; the client must never manufacture a local
// acceptance record instead of posting one.
type AcceptLegalRequest struct {
	DocumentType    string `json:"documentType"`
	Version         string `json:"version"`
	ClientRequestID string `json:"clientRequestId"`
}

// LegalReceipt is the platform's acknowledgement of an acceptance.
type LegalReceipt struct {
	DocumentType string `json:"documentType"`
	Version      string `json:"version"`
	AcceptedAt   string `json:"acceptedAt"`
}

// DictionaryRef is the lightweight dictionary pointer embedded in bootstrap, so
// a cold start does not need a second round trip when the words are unchanged.
type DictionaryRef struct {
	Version string `json:"version"`
	ETag    string `json:"etag"`
}

// LegalSummary is the same lightweight pointer for legal documents.
type LegalSummary struct {
	Documents []LegalDocument `json:"documents"`
}

// Bootstrap is the cold-start payload: everything needed to render a usable
// first screen with one request. It is a convenience aggregate, not a second
// source of truth — the catalog it carries is the same signed envelope
// `Models()` returns.
type Bootstrap struct {
	Account             Account        `json:"account"`
	Activation          Activation     `json:"activation"`
	Plan                Plan           `json:"plan"`
	Credits             Credits        `json:"credits"`
	Policy              PolicyEnvelope `json:"policy"`
	SensitiveDictionary DictionaryRef  `json:"sensitiveDictionary"`
	Legal               LegalSummary   `json:"legal"`
}

// SendCodeRequest asks for an SMS code. Retries are forbidden: the platform
// applies IP + phone + risk limiting and the user re-submits deliberately
// (中台交付包 §3.3).
type SendCodeRequest struct {
	Phone           string `json:"phone"`
	Purpose         string `json:"purpose"`
	ClientRequestID string `json:"clientRequestId"`
}

// Cooldown is the SMS cooling-off state.
type Cooldown struct {
	CooldownSec  int `json:"cooldownSec"`
	ExpiresInSec int `json:"expiresInSec"`
}

// LoginRequest is both first activation (ActivationCode set) and a later login
// (ActivationCode empty).
type LoginRequest struct {
	Phone           string `json:"phone"`
	Code            string `json:"code"`
	Nickname        string `json:"nickname,omitempty"`
	ActivationCode  string `json:"activationCode,omitempty"`
	InstallID       string `json:"installId"`
	ClientRequestID string `json:"clientRequestId"`
}

// Session is the token pair plus its lifetime. RefreshToken is rotated on every
// refresh; the store that persists it is internal/credentialstore, not here.
type Session struct {
	AccessToken           string `json:"accessToken"`
	RefreshToken          string `json:"refreshToken"`
	AccessTokenExpiresSec int    `json:"accessTokenExpiresInSec"`
}

// LoginResult is the login response. It is a Session plus the account and
// activation state the platform resolved during login.
type LoginResult struct {
	Session
	Account    Account    `json:"account"`
	Activation Activation `json:"activation"`
}

// RequestState is the lifecycle of one gateway call. The set is closed: an
// unrecognised value must be surfaced as unknown, never coerced into `settled`
// or `reversed`, because that would silently change what the user is told about
// their balance (中台交付包 §5.4).
type RequestState string

const (
	StateReceived              RequestState = "received"
	StateReserved              RequestState = "reserved"
	StateStreaming             RequestState = "streaming"
	StateSettled               RequestState = "settled"
	StateReversed              RequestState = "reversed"
	StateReconciliationPending RequestState = "reconciliation_pending"
	StateFailed                RequestState = "failed"
)

// Final reports whether the state is terminal and its charge is authoritative.
// Only `settled` and `reversed` are final; `reconciliation_pending` is
// explicitly not, which is why it cannot be collapsed into either.
func (s RequestState) Final() bool {
	return s == StateSettled || s == StateReversed
}

// RequestStatus is the answer to "what happened to this clientRequestId" —
// the only permitted reaction to a broken stream (中台交付包 §5.4).
type RequestStatus struct {
	ClientRequestID     string       `json:"clientRequestId"`
	RequestID           string       `json:"requestId"`
	LedgerID            string       `json:"ledgerId,omitempty"`
	State               RequestState `json:"state"`
	Usage               *TokenUsage  `json:"usage,omitempty"`
	ChargeMicroCredits  int64        `json:"chargeMicroCredits,omitempty"`
	BalanceMicroCredits int64        `json:"balanceMicroCredits"`
	Code                Code         `json:"code,omitempty"`
}

// Usage is the read-only balance snapshot returned by the usage endpoint.
type Usage struct {
	BalanceMicroCredits int64  `json:"balanceMicroCredits"`
	Currency            string `json:"currency"`
	UpdatedAt           string `json:"updatedAt"`
}

// LedgerQuery is a read-only cursor page request.
type LedgerQuery struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// LedgerEntry is one append-only ledger row. Refunds and grants appear as new
// `reversal`/`adjustment` entries; history rows are never edited in place
// (中台交付包 §5.5).
type LedgerEntry struct {
	LedgerID           string       `json:"ledgerId"`
	ClientRequestID    string       `json:"clientRequestId"`
	ModelID            string       `json:"modelId"`
	PricingVersion     string       `json:"pricingVersion"`
	State              RequestState `json:"state"`
	ChargeMicroCredits int64        `json:"chargeMicroCredits"`
	CreatedAt          string       `json:"createdAt"`
}

// LedgerPage is one page of the ledger plus the balance at page time.
type LedgerPage struct {
	BalanceMicroCredits int64         `json:"balanceMicroCredits"`
	Entries             []LedgerEntry `json:"entries"`
	NextCursor          string        `json:"nextCursor,omitempty"`
}

// DeviceSession is the minimal, safe session summary a user may revoke. It
// carries no token and no precise device fingerprint (中台交付包 §4.2.4).
type DeviceSession struct {
	ID             string `json:"sessionId"`
	Current        bool   `json:"current"`
	CreatedAt      string `json:"createdAt"`
	LastActiveAt   string `json:"lastActiveAt"`
	InstallIDShort string `json:"installIdShort"`
	Status         string `json:"status"`
}
