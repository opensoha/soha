package dto

type PasswordLoginRequest struct {
	Login             string `json:"login"`
	Password          string `json:"password"`
	VerificationToken string `json:"verificationToken"`
}

type LoginVerificationOptions struct {
	SliderEnabled bool `json:"sliderEnabled"`
}

type LoginOptionsResponse struct {
	Verification LoginVerificationOptions `json:"verification"`
}

type LoginVerificationChallengeRequest struct {
	Type        string `json:"type"`
	SliderValue int    `json:"sliderValue"`
}

type LoginVerificationChallengeResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expiresIn"`
}

type ProPasswordLoginRequest struct {
	Login             string `json:"login"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	Type              string `json:"type"`
	VerificationToken string `json:"verificationToken"`
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
