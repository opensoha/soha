package identity

import "time"

type Principal struct {
	UserID         string   `json:"userId"`
	UserName       string   `json:"userName"`
	Email          string   `json:"email"`
	AvatarURL      string   `json:"avatarUrl,omitempty"`
	AvatarFit      string   `json:"avatarFit,omitempty"`
	Roles          []string `json:"roles"`
	Teams          []string `json:"teams"`
	Projects       []string `json:"projects"`
	Tags           []string `json:"tags"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
}

// User is the persistence-neutral identity record used by authentication services.
type User struct {
	ID           string
	Username     string
	Email        string
	DisplayName  string
	Status       string
	Tags         []string
	Preferences  map[string]any
	AuthzVersion int64
}

type AuthzState struct {
	UserID       string
	Status       string
	AuthzVersion int64
}

type Session struct {
	ID             string
	UserID         string
	RefreshTokenID string
	ProviderType   string
	Status         string
	ExpiresAt      time.Time
	LastSeenAt     time.Time
	Metadata       map[string]any
	AuthzVersion   int64
}

type EphemeralToken struct {
	Token     string
	Kind      string
	Payload   map[string]any
	ExpiresAt time.Time
	CreatedAt time.Time
}

type OIDCIdentity struct {
	ID             string
	UserID         string
	ProviderType   string
	ProviderID     string
	ProviderUserID string
	Profile        map[string]any
	LastLoginAt    time.Time
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
	AvatarURL   string           `json:"avatarUrl,omitempty"`
	AvatarFit   string           `json:"avatarFit,omitempty"`
	Status      string           `json:"status"`
	Roles       []string         `json:"roles"`
	Teams       []string         `json:"teams"`
	Projects    []string         `json:"projects"`
	Tags        []string         `json:"tags"`
	Identities  []LinkedIdentity `json:"identities"`
	Sessions    []SessionRecord  `json:"sessions"`
	LastLoginAt *time.Time       `json:"lastLoginAt,omitempty"`
}

type ProfileUpdate struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	AvatarFit   string `json:"avatarFit,omitempty"`
}

type PasswordChange struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type AccessContext struct {
	TokenID     string         `json:"tokenId"`
	TokenKind   string         `json:"tokenKind,omitempty"`
	TokenPrefix string         `json:"tokenPrefix,omitempty"`
	SessionID   string         `json:"sessionId,omitempty"`
	SubjectType string         `json:"subjectType,omitempty"`
	SubjectID   string         `json:"subjectId,omitempty"`
	Scopes      []string       `json:"scopes,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	ExpiresAt   time.Time      `json:"expiresAt"`
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

type StreamTicketRequest struct {
	Path string `json:"path"`
}

type StreamTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Provider struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	IconURL  string `json:"iconUrl,omitempty"`
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
