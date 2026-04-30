package dto

type PasswordLoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type ProPasswordLoginRequest struct {
	Login    string `json:"login"`
	Username string `json:"username"`
	Password string `json:"password"`
	Type     string `json:"type"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type OIDCExchangeRequest struct {
	Code string `json:"code"`
}
