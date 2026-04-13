package identity

import "time"

type Principal struct {
	UserID   string   `json:"userId"`
	UserName string   `json:"userName"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	Teams    []string `json:"teams"`
	Projects []string `json:"projects"`
	Tags     []string `json:"tags"`
}

type AccessContext struct {
	TokenID   string    `json:"tokenId"`
	SessionID string    `json:"sessionId"`
	ExpiresAt time.Time `json:"expiresAt"`
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
