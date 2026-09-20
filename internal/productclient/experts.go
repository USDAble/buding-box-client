package productclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
)

type PlatformSkill struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     uint32 `json:"version"`
	Content     string `json:"content"`
}
type PlatformExpert struct {
	ID                   string          `json:"id"`
	Code                 string          `json:"code"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	CategoryCode         string          `json:"category_code"`
	IconKey              string          `json:"icon_key"`
	Version              uint32          `json:"version"`
	DefaultModelID       string          `json:"default_model_id"`
	AllowedModelIDs      []string        `json:"allowed_model_ids"`
	RequiredCapabilities []string        `json:"required_capabilities"`
	RecommendedQuestions []string        `json:"recommended_questions"`
	Skills               []PlatformSkill `json:"skills"`
}
type PlatformExperts struct {
	Experts []PlatformExpert `json:"experts"`
}
type PlatformSkills struct {
	Skills []PlatformSkill `json:"skills"`
}

func (c *Client) Experts(ctx context.Context) (*PlatformExperts, error) {
	var out PlatformExperts
	err := c.doAuthorized(ctx, http.MethodGet, "/client/experts", nil, &out)
	return &out, err
}
func (c *Client) Skills(ctx context.Context) (*PlatformSkills, error) {
	var out PlatformSkills
	err := c.doAuthorized(ctx, http.MethodGet, "/client/skills", nil, &out)
	return &out, err
}

func (c *Client) ExpertDetail(ctx context.Context, id string, version uint32) (*PlatformExpert, error) {
	var out PlatformExpert
	err := c.doAuthorized(ctx, http.MethodGet, "/client/experts/"+url.PathEscape(id)+"?version="+strconv.FormatUint(uint64(version), 10), nil, &out)
	return &out, err
}
func (c *Client) SkillDetail(ctx context.Context, id string, version uint32) (*PlatformSkill, error) {
	var out PlatformSkill
	err := c.doAuthorized(ctx, http.MethodGet, "/client/skills/"+url.PathEscape(id)+"?version="+strconv.FormatUint(uint64(version), 10), nil, &out)
	return &out, err
}

// SessionCacheScope partitions in-memory snapshots by endpoint and credential
// generation. Rotation expires this cache; no credential or digest is persisted.
func (c *Client) SessionCacheScope() string {
	sum := sha256.Sum256([]byte(c.baseURL + "\x00" + c.creds.AccessToken()))
	return hex.EncodeToString(sum[:])
}
