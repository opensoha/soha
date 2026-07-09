package dto

type PasswordLoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type LoginVerificationOptions struct {
	SliderEnabled bool `json:"sliderEnabled"`
}

type LoginOptionsResponse struct {
	Verification LoginVerificationOptions `json:"verification"`
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

type UpdateProfileRequest struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	AvatarURL   string `json:"avatarUrl"`
	AvatarFit   string `json:"avatarFit"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}
