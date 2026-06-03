package identity

import "time"

type Principal struct {
	UserID         string   `json:"userId"`
	UserName       string   `json:"userName"`
	Email          string   `json:"email"`
	Roles          []string `json:"roles"`
	Teams          []string `json:"teams"`
	Projects       []string `json:"projects"`
	Tags           []string `json:"tags"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
}

type LinkedIdentity struct {
	ID             string     `json:"id"`
	ProviderType   string     `json:"providerType"`
	ProviderID     string     `json:"providerId"`
	ProviderUserID string     `json:"providerUserId"`
	DisplayName    string     `json:"displayName,omitempty"`
	Email          string     `json:"email,omitempty"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty"`
}

type UserProfile struct {
	UserID      string           `json:"userId"`
	Username    string           `json:"username"`
	DisplayName string           `json:"displayName"`
	Email       string           `json:"email"`
	Phone       string           `json:"phone,omitempty"`
	Status      string           `json:"status"`
	Roles       []string         `json:"roles"`
	Teams       []string         `json:"teams"`
	Projects    []string         `json:"projects"`
	Tags        []string         `json:"tags"`
	Identities  []LinkedIdentity `json:"identities"`
	Sessions    []SessionRecord  `json:"sessions"`
	LastLoginAt *time.Time       `json:"lastLoginAt,omitempty"`
}

type AccessContext struct {
	TokenID     string    `json:"tokenId"`
	TokenKind   string    `json:"tokenKind,omitempty"`
	SessionID   string    `json:"sessionId,omitempty"`
	SubjectType string    `json:"subjectType,omitempty"`
	SubjectID   string    `json:"subjectId,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type TokenSet struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	TokenType    string    `json:"tokenType"`
	ExpiresIn    int64     `json:"expiresIn"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type AuthResult struct {
	User   Principal `json:"user"`
	Tokens TokenSet  `json:"tokens"`
}

type Provider struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	LoginURL string `json:"loginUrl,omitempty"`
}

type SessionRecord struct {
	ID             string         `json:"id"`
	UserID         string         `json:"userId"`
	UserName       string         `json:"userName"`
	Email          string         `json:"email"`
	ProviderType   string         `json:"providerType"`
	Status         string         `json:"status"`
	ExpiresAt      time.Time      `json:"expiresAt"`
	LastSeenAt     time.Time      `json:"lastSeenAt"`
	CreatedAt      time.Time      `json:"createdAt"`
	RefreshTokenID string         `json:"refreshTokenId"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}
