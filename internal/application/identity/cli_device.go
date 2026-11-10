package identity

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) CreateCLIDeviceAuthorization(ctx context.Context, input domainidentity.CLIDeviceCreate, source string) (domainidentity.CLIDeviceAuthorization, error) {
	if err := s.cliAvailable(true); err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	input, err := validateCLIDeviceInput(input)
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	device, err := cliRandomSecret()
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	handle, err := cliRandomSecret()
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	code, err := cliRandomCode()
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	keys, err := s.jwtKeyring()
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	codeHash, err := s.cliCodeHash(keys.Active().ID(), handle, code)
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	sourceHash, err := s.cliSourceHash(source)
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	now := s.cliNow().UTC()
	grant := domainidentity.CLIGrant{ID: uuid.NewString(), DeviceHash: cliHash(device), HandleHash: cliHash(handle), CodeHash: codeHash, CodeKeyID: keys.Active().ID(), CodeChallenge: input.CodeChallenge, SourceHash: sourceHash, DeviceName: input.DeviceName, ClientVersion: input.ClientVersion, RequestedPermissions: input.PermissionKeys, State: "pending", PollInterval: int(s.cfg.CLI.PollInterval / time.Second), ExpiresAt: now.Add(s.cfg.CLI.DeviceTTL)}
	var businessErr error
	err = s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		// Check the source first so an already limited source cannot consume
		// global slots. A saturated global bucket rolls back new source rows.
		if e := s.cliBudget(ctx, "create:"+sourceHash, s.cfg.CLI.CreateLimit); e != nil {
			return e
		}
		if e := s.cliBudget(ctx, "create:global", s.cfg.CLI.ActiveGrantLimit); e != nil {
			return e
		}
		created, e := s.cliState.CreateCLIGrant(ctx, grant, s.cfg.CLI.SourcePendingLimit, s.cfg.CLI.ActiveGrantLimit)
		if e != nil {
			return e
		}
		if !created {
			// Capacity rejection still consumes admitted attempt budgets.
			businessErr = &CLIPollError{Code: "device_capacity_limited", Seconds: int(s.cfg.CLI.DeviceTTL / time.Second)}
		}
		return nil
	})
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	if businessErr != nil {
		return domainidentity.CLIDeviceAuthorization{}, businessErr
	}
	uri, err := url.Parse(strings.TrimRight(s.cfg.CLI.PublicURL, "/") + "/auth/device")
	if err != nil {
		return domainidentity.CLIDeviceAuthorization{}, err
	}
	uri.Fragment = "handle=" + handle
	_ = s.recordAudit(ctx, domainidentity.Principal{}, "cli.device.create", "success", "CLI device authorization created", map[string]any{"grantId": grant.ID})
	return domainidentity.CLIDeviceAuthorization{GrantID: grant.ID, DeviceCode: device, UserCode: code, VerificationURI: uri.String(), ExpiresIn: int(s.cfg.CLI.DeviceTTL / time.Second), ExpiresAt: grant.ExpiresAt, Interval: grant.PollInterval}, nil
}

func (s *Service) cliInspectionBudget(ctx context.Context, userID, source, kind string) error {
	hash, err := s.cliSourceHash(source)
	if err != nil {
		return err
	}
	if err := s.cliBudget(ctx, kind+":source:"+hash, s.cfg.CLI.SourceAttemptLimit); err != nil {
		return err
	}
	if err := s.cliBudget(ctx, kind+":user:"+cliHash(userID), s.cfg.CLI.UserAttemptLimit); err != nil {
		return err
	}
	return s.cliBudget(ctx, kind+":global", min(s.cfg.CLI.ActiveGrantLimit*20, 10000))
}

func (s *Service) validateCLIUserCode(grant *domainidentity.CLIGrant, handle, code string) error {
	if !grant.ExpiresAt.After(s.cliNow().UTC()) {
		grant.State = "expired"
		return cliError(apperrors.ErrInvalidArgument, "expired_token", "Device authorization expired; start a new login")
	}
	if grant.State != "pending" {
		return cliError(apperrors.ErrConflict, "device_authorization_closed", "This device authorization is already closed")
	}
	hash, err := s.cliCodeHash(grant.CodeKeyID, handle, code)
	if err != nil || !cliUserCodePattern.MatchString(code) || !cliEqual(grant.CodeHash, hash) {
		grant.Attempts++
		if grant.Attempts >= 5 {
			grant.State = "denied"
		}
		return cliError(apperrors.ErrInvalidArgument, "invalid_user_code", "The device code is invalid or no longer available")
	}
	return nil
}

func (s *Service) InspectCLIDeviceAuthorization(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, origin, source string, input domainidentity.CLIDeviceInspect) (domainidentity.CLIDeviceConfirmation, error) {
	if err := s.cliAvailable(true); err != nil {
		return domainidentity.CLIDeviceConfirmation{}, err
	}
	if !cliSecretPattern.MatchString(input.VerificationHandle) {
		return domainidentity.CLIDeviceConfirmation{}, apperrors.ErrInvalidArgument
	}
	var result domainidentity.CLIDeviceConfirmation
	var businessErr error
	var lockedGrantID string
	err := s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		if e := s.requireCLIBrowser(ctx, principal, access, origin); e != nil {
			return e
		}
		if e := s.cliInspectionBudget(ctx, principal.UserID, source, "inspect"); e != nil {
			return e
		}
		grant, e := s.cliState.GetCLIGrant(ctx, "handle", cliHash(input.VerificationHandle))
		if e != nil {
			if !errors.Is(e, apperrors.ErrNotFound) {
				return e
			}
			businessErr = cliError(apperrors.ErrInvalidArgument, "invalid_user_code", "The device code is invalid or no longer available")
			return nil
		}
		previousAttempts := grant.Attempts
		if e := s.validateCLIUserCode(&grant, input.VerificationHandle, input.UserCode); e != nil {
			businessErr = e
			lockedGrantID = cliNewCodeLock(grant, previousAttempts)
			return s.cliState.SaveCLIGrant(ctx, grant)
		}
		current, e := s.CurrentPrincipal(ctx, principal.UserID)
		if e != nil {
			return e
		}
		permissions, e := s.cliPermissions(ctx, current, grant.RequestedPermissions)
		if e != nil {
			return e
		}
		confirmation, e := cliRandomSecret()
		if e != nil {
			return e
		}
		grant.ConfirmationHash = cliHash(confirmation)
		grant.ConfirmationUserID = principal.UserID
		grant.ConfirmationSessionID = access.SessionID
		grant.ConfirmationExpiresAt = minCLITime(s.cliNow().UTC().Add(time.Minute), grant.ExpiresAt)
		grant.GrantedPermissions = permissions
		if e := s.cliState.SaveCLIGrant(ctx, grant); e != nil {
			return e
		}
		result = domainidentity.CLIDeviceConfirmation{GrantID: grant.ID, ConfirmationID: confirmation, DeviceName: grant.DeviceName, ClientVersion: grant.ClientVersion, User: current, PermissionKeys: permissions, ExpiresAt: grant.ExpiresAt, ConfirmationExpiresAt: grant.ConfirmationExpiresAt, SessionAbsoluteExpiresIn: int(s.cfg.CLI.AbsoluteTTL / time.Second)}
		return nil
	})
	if err != nil {
		return result, err
	}
	s.auditCLICodeLock(ctx, principal, lockedGrantID)
	if businessErr != nil {
		return domainidentity.CLIDeviceConfirmation{}, businessErr
	}
	return result, nil
}

func (s *Service) DecideCLIDeviceAuthorization(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, origin, source string, input domainidentity.CLIDeviceDecision) (domainidentity.CLIDeviceStatus, error) {
	if err := s.cliAvailable(true); err != nil {
		return domainidentity.CLIDeviceStatus{}, err
	}
	if !validCLIDecisionInput(input) {
		return domainidentity.CLIDeviceStatus{}, apperrors.ErrInvalidArgument
	}
	var result domainidentity.CLIDeviceStatus
	var businessErr error
	var lockedGrantID string
	err := s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		if e := s.requireCLIBrowser(ctx, principal, access, origin); e != nil {
			return e
		}
		if e := s.cliInspectionBudget(ctx, principal.UserID, source, "decision"); e != nil {
			return e
		}
		grant, e := s.cliState.GetCLIGrant(ctx, "handle", cliHash(input.VerificationHandle))
		if e != nil {
			return apperrors.ErrNotFound
		}
		if s.cliDecisionAlreadyApplied(grant, principal, access, input) {
			result = cliGrantStatus(grant)
			return nil
		}
		previousAttempts := grant.Attempts
		if e := s.validateCLIUserCode(&grant, input.VerificationHandle, input.UserCode); e != nil {
			businessErr = e
			lockedGrantID = cliNewCodeLock(grant, previousAttempts)
			return s.cliState.SaveCLIGrant(ctx, grant)
		}
		if !grant.ConfirmationExpiresAt.After(s.cliNow().UTC()) || grant.ConfirmationUserID != principal.UserID || grant.ConfirmationSessionID != access.SessionID || !cliEqual(grant.ConfirmationHash, cliHash(input.ConfirmationID)) {
			return cliError(apperrors.ErrAccessDenied, "invalid_device_confirmation", "Inspect the request again using the same signed in account and browser session")
		}
		if input.Decision == "deny" {
			grant.State = "denied"
		} else {
			current, e := s.CurrentPrincipal(ctx, principal.UserID)
			if e != nil {
				return e
			}
			limited, e := appaccess.RuntimePrincipalWithin(ctx, s.permissions, current, grant.GrantedPermissions)
			if e != nil {
				return e
			}
			grant.GrantedPermissions = limited.PermissionKeys
			grant.UserID = principal.UserID
			grant.State = "approved"
		}
		if e := s.cliState.SaveCLIGrant(ctx, grant); e != nil {
			return e
		}
		result = cliGrantStatus(grant)
		return nil
	})
	if err != nil {
		return result, err
	}
	s.auditCLICodeLock(ctx, principal, lockedGrantID)
	if businessErr != nil {
		return result, businessErr
	}
	_ = s.recordAudit(ctx, principal, "cli.device."+input.Decision, "success", "CLI device decision recorded", map[string]any{"grantId": result.GrantID})
	return result, nil
}

func (s *Service) ExchangeCLIDeviceToken(ctx context.Context, input domainidentity.CLIDeviceTokenRequest) (domainidentity.AuthResult, error) {
	if err := s.cliAvailable(false); err != nil {
		return domainidentity.AuthResult{}, err
	}
	requestHash, err := cliRequestHash(input.ClaimRequestID)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	if !cliSecretPattern.MatchString(input.DeviceCode) || !cliVerifierPattern.MatchString(input.CodeVerifier) {
		return domainidentity.AuthResult{}, apperrors.ErrInvalidArgument
	}
	var result domainidentity.AuthResult
	var businessErr error
	var issuedGrantID string
	err = s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		grant, e := s.cliState.GetCLIGrant(ctx, "device", cliHash(input.DeviceCode))
		if e != nil {
			return apperrors.ErrUnauthorized
		}
		if !cliPKCE(grant, input.CodeVerifier) {
			return apperrors.ErrUnauthorized
		}
		if grant.State == "issued" {
			if !cliEqual(grant.ClaimRequestHash, requestHash) {
				return cliError(apperrors.ErrConflict, "device_already_claimed", "This device request has already been claimed")
			}
			result, e = s.openCLIReceipt(ctx, grant.ID+":"+requestHash, grant.Receipt, grant.ReceiptExpiresAt)
			return e
		}
		if e := s.cliBudget(ctx, "poll:"+grant.ID, 120); e != nil {
			businessErr = e
			return nil
		}
		if e := s.cliPollGrant(&grant); e != nil {
			businessErr = e
			return s.cliState.SaveCLIGrant(ctx, grant)
		}
		result, e = s.issueCLISession(ctx, grant.UserID, grant.GrantedPermissions, grant.DeviceName, grant.ClientVersion)
		if e != nil {
			return e
		}
		receipt, e := s.sealCLIReceipt(grant.ID+":"+requestHash, result)
		if e != nil {
			return e
		}
		grant.State = "issued"
		grant.SessionID = result.Tokens.SessionID
		grant.ClaimRequestHash = requestHash
		grant.Receipt = receipt
		grant.ReceiptExpiresAt = s.cliNow().UTC().Add(cliReceiptWindow)
		issuedGrantID = grant.ID
		return s.cliState.SaveCLIGrant(ctx, grant)
	})
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	if businessErr != nil {
		return domainidentity.AuthResult{}, businessErr
	}
	s.auditCLIGrantIssued(ctx, result, issuedGrantID)
	return result, nil
}

func (s *Service) cliPollGrant(grant *domainidentity.CLIGrant) error {
	now := s.cliNow().UTC()
	if !grant.ExpiresAt.After(now) {
		grant.State = "expired"
		return cliError(apperrors.ErrInvalidArgument, "expired_token", "Device authorization expired; start a new login")
	}
	switch grant.State {
	case "denied":
		return cliError(apperrors.ErrAccessDenied, "device_authorization_denied", "Device authorization was denied")
	case "cancelled":
		return cliError(apperrors.ErrConflict, "device_authorization_cancelled", "Device authorization was cancelled")
	case "pending", "approved":
	default:
		return cliError(apperrors.ErrConflict, "device_authorization_closed", "Device authorization is closed")
	}
	if grant.NextPollAt.After(now) {
		grant.PollInterval += 5
		grant.NextPollAt = now.Add(time.Duration(grant.PollInterval) * time.Second)
		return &CLIPollError{Code: "slow_down", Seconds: grant.PollInterval}
	}
	grant.NextPollAt = now.Add(time.Duration(grant.PollInterval) * time.Second)
	if grant.State == "pending" {
		return &CLIPollError{Code: "authorization_pending", Seconds: grant.PollInterval, Pending: true}
	}
	return nil
}

func (s *Service) CancelCLIDeviceAuthorization(ctx context.Context, input domainidentity.CLIDeviceCancelRequest) (domainidentity.CLIDeviceStatus, error) {
	if err := s.cliAvailable(false); err != nil {
		return domainidentity.CLIDeviceStatus{}, err
	}
	if !cliSecretPattern.MatchString(input.DeviceCode) || !cliVerifierPattern.MatchString(input.CodeVerifier) {
		return domainidentity.CLIDeviceStatus{}, apperrors.ErrInvalidArgument
	}
	var result domainidentity.CLIDeviceStatus
	var actor domainidentity.Principal
	err := s.cliState.WithinCLITransaction(ctx, func(ctx context.Context) error {
		grant, e := s.cliState.GetCLIGrant(ctx, "device", cliHash(input.DeviceCode))
		if e != nil {
			return apperrors.ErrUnauthorized
		}
		if !cliPKCE(grant, input.CodeVerifier) {
			return apperrors.ErrUnauthorized
		}
		if grant.SessionID != "" {
			if e := s.sessionAdmin.RevokeSessionByID(ctx, grant.SessionID); e != nil {
				return fmt.Errorf("revoke cancelled CLI session: %w", e)
			}
		}
		grant.State = "cancelled"
		grant.Receipt = ""
		grant.ReceiptExpiresAt = time.Time{}
		if e := s.cliState.SaveCLIGrant(ctx, grant); e != nil {
			return e
		}
		result = cliGrantStatus(grant)
		actor.UserID = grant.UserID
		return nil
	})
	if err == nil {
		_ = s.recordAudit(ctx, actor, "cli.device.cancel", "success", "CLI device authorization cancelled", map[string]any{"grantId": result.GrantID, "sessionId": result.SessionID})
	}
	return result, err
}

func (s *Service) auditCLIGrantIssued(ctx context.Context, result domainidentity.AuthResult, grantID string) {
	if grantID != "" {
		_ = s.recordAudit(ctx, result.User, "cli.device.claim", "success", "CLI device session issued", map[string]any{"grantId": grantID, "sessionId": result.Tokens.SessionID})
	}
}

func cliNewCodeLock(grant domainidentity.CLIGrant, previousAttempts int) string {
	if previousAttempts == 4 && grant.Attempts == 5 {
		return grant.ID
	}
	return ""
}

func (s *Service) auditCLICodeLock(ctx context.Context, principal domainidentity.Principal, grantID string) {
	if grantID != "" {
		_ = s.recordAudit(ctx, principal, "cli.device.code_lock", "deny", "CLI device code attempt budget exhausted", map[string]any{"grantId": grantID, "reason": "code_attempts_exceeded"})
	}
}
func cliGrantStatus(grant domainidentity.CLIGrant) domainidentity.CLIDeviceStatus {
	return domainidentity.CLIDeviceStatus{GrantID: grant.ID, Status: grant.State, ExpiresAt: grant.ExpiresAt, SessionID: grant.SessionID}
}

func (s *Service) cliDecisionAlreadyApplied(grant domainidentity.CLIGrant, principal domainidentity.Principal, access domainidentity.AccessContext, input domainidentity.CLIDeviceDecision) bool {
	sameDecision := input.Decision == "approve" && (grant.State == "approved" || grant.State == "issued") || input.Decision == "deny" && grant.State == "denied"
	if !sameDecision || !grant.ExpiresAt.After(s.cliNow().UTC()) || grant.ConfirmationUserID != principal.UserID || grant.ConfirmationSessionID != access.SessionID || !cliEqual(grant.ConfirmationHash, cliHash(input.ConfirmationID)) || !cliUserCodePattern.MatchString(input.UserCode) {
		return false
	}
	hash, err := s.cliCodeHash(grant.CodeKeyID, input.VerificationHandle, input.UserCode)
	return err == nil && cliEqual(grant.CodeHash, hash)
}

func validateCLIDeviceInput(input domainidentity.CLIDeviceCreate) (domainidentity.CLIDeviceCreate, error) {
	if input.ClientID != "soha-cli" || input.CodeChallengeMethod != "S256" || !cliSecretPattern.MatchString(input.CodeChallenge) || len(input.PermissionKeys) > 256 {
		return domainidentity.CLIDeviceCreate{}, apperrors.ErrInvalidArgument
	}
	input.PermissionKeys = append([]string(nil), input.PermissionKeys...)
	deviceName, err := cliDisplay(input.DeviceName, "Terminal")
	if err != nil {
		return domainidentity.CLIDeviceCreate{}, err
	}
	version, err := cliDisplay(input.ClientVersion, "unknown")
	if err != nil {
		return domainidentity.CLIDeviceCreate{}, err
	}
	seen := map[string]bool{}
	for _, key := range input.PermissionKeys {
		if key == "" || len(key) > 128 || seen[key] {
			return domainidentity.CLIDeviceCreate{}, apperrors.ErrInvalidArgument
		}
		seen[key] = true
	}
	input.DeviceName = deviceName
	input.ClientVersion = version
	return input, nil
}

func validCLIDecisionInput(input domainidentity.CLIDeviceDecision) bool {
	return cliSecretPattern.MatchString(input.VerificationHandle) && cliSecretPattern.MatchString(input.ConfirmationID) && (input.Decision == "approve" || input.Decision == "deny")
}
