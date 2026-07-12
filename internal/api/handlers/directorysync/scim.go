package directorysync

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

type scimValue struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
	Type    string `json:"type,omitempty"`
}
type scimUser struct {
	Schemas      []string    `json:"schemas"`
	ID           string      `json:"id"`
	ExternalID   string      `json:"externalId,omitempty"`
	UserName     string      `json:"userName"`
	DisplayName  string      `json:"displayName,omitempty"`
	Active       *bool       `json:"active,omitempty"`
	Emails       []scimValue `json:"emails,omitempty"`
	PhoneNumbers []scimValue `json:"phoneNumbers,omitempty"`
}
type scimMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}
type scimGroup struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	ExternalID  string       `json:"externalId,omitempty"`
	DisplayName string       `json:"displayName"`
	ParentID    string       `json:"parentId,omitempty"`
	Members     []scimMember `json:"members,omitempty"`
}
type scimPatch struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	} `json:"Operations"`
}

func (h *Handler) SCIMServiceProviderConfig(c *gin.Context) {
	if _, ok := h.scimConnection(c, domain.SCIMScopeRead); !ok {
		return
	}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusOK, gin.H{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"}, "patch": gin.H{"supported": true}, "bulk": gin.H{"supported": false}, "filter": gin.H{"supported": true, "maxResults": 200}, "changePassword": gin.H{"supported": false}, "sort": gin.H{"supported": false}, "etag": gin.H{"supported": false}, "authenticationSchemes": []gin.H{{"type": "oauthbearertoken", "name": "Bearer Token", "primary": true}}})
}

func (h *Handler) SCIMListUsers(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeRead)
	if !ok {
		return
	}
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "SCIM users unavailable")
		return
	}
	resources := make([]scimUser, 0, len(snapshot.People))
	for _, item := range snapshot.People {
		resources = append(resources, personToSCIM(item))
	}
	h.scimList(c, resources)
}
func (h *Handler) SCIMGetUser(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeRead)
	if !ok {
		return
	}
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err == nil {
		for _, item := range snapshot.People {
			if item.ExternalID == c.Param("resourceID") {
				c.Header("Content-Type", "application/scim+json")
				c.JSON(http.StatusOK, personToSCIM(item))
				return
			}
		}
	}
	h.scimError(c, http.StatusNotFound, "notFound", "User not found")
}
func (h *Handler) SCIMCreateUser(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	var input scimUser
	if c.ShouldBindJSON(&input) != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", "Invalid User payload")
		return
	}
	if input.ID == "" {
		input.ID = input.ExternalID
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	if err := h.writeSCIMUser(c, connectionID, input); err != nil {
		return
	}
	input.Schemas = []string{scimUserSchema}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusCreated, input)
}
func (h *Handler) SCIMPatchUser(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	input, found := h.scimUserByID(c, connectionID, c.Param("resourceID"))
	if !found {
		return
	}
	var patch scimPatch
	if c.ShouldBindJSON(&patch) != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", "Invalid PatchOp payload")
		return
	}
	if err := validatePatch(patch, map[string]bool{"active": true, "username": true, "displayname": true, "emails": true, "phonenumbers": true}); err != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}
	applyUserPatch(&input, patch)
	if err := h.writeSCIMUser(c, connectionID, input); err != nil {
		return
	}
	input.Schemas = []string{scimUserSchema}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusOK, input)
}
func (h *Handler) SCIMDeleteUser(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	_, policy, err := h.repository.GetConnection(c.Request.Context(), connectionID)
	if err != nil || !policy.SyncPeople {
		h.scimError(c, http.StatusForbidden, "mutability", "People synchronization is disabled")
		return
	}
	if err := h.repository.DeleteSCIMPerson(c.Request.Context(), connectionID, c.Param("resourceID")); err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "Delete failed")
		return
	}
	if !h.applySCIM(c, connectionID, policy) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SCIMListGroups(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeRead)
	if !ok {
		return
	}
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "SCIM groups unavailable")
		return
	}
	members := map[string][]scimMember{}
	for _, m := range snapshot.Memberships {
		members[m.ExternalOrganizationID] = append(members[m.ExternalOrganizationID], scimMember{Value: m.ExternalPersonID})
	}
	resources := make([]scimGroup, 0, len(snapshot.Organizations))
	for _, item := range snapshot.Organizations {
		resources = append(resources, organizationToSCIM(item, members[item.ExternalID]))
	}
	h.scimList(c, resources)
}
func (h *Handler) SCIMGetGroup(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeRead)
	if !ok {
		return
	}
	group, found := h.scimGroupByID(c, connectionID, c.Param("resourceID"))
	if !found {
		return
	}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusOK, group)
}
func (h *Handler) SCIMCreateGroup(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	var input scimGroup
	if c.ShouldBindJSON(&input) != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", "Invalid Group payload")
		return
	}
	if input.ID == "" {
		input.ID = input.ExternalID
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	if err := h.writeSCIMGroup(c, connectionID, input); err != nil {
		return
	}
	input.Schemas = []string{scimGroupSchema}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusCreated, input)
}
func (h *Handler) SCIMPatchGroup(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	input, found := h.scimGroupByID(c, connectionID, c.Param("resourceID"))
	if !found {
		return
	}
	var patch scimPatch
	if c.ShouldBindJSON(&patch) != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", "Invalid PatchOp payload")
		return
	}
	if err := validatePatch(patch, map[string]bool{"displayname": true, "parentid": true, "members": true}); err != nil {
		h.scimError(c, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}
	applyGroupPatch(&input, patch)
	if err := h.writeSCIMGroup(c, connectionID, input); err != nil {
		return
	}
	input.Schemas = []string{scimGroupSchema}
	c.Header("Content-Type", "application/scim+json")
	c.JSON(http.StatusOK, input)
}
func (h *Handler) SCIMDeleteGroup(c *gin.Context) {
	connectionID, ok := h.scimConnection(c, domain.SCIMScopeWrite)
	if !ok {
		return
	}
	if err := h.repository.DeleteSCIMOrganization(c.Request.Context(), connectionID, c.Param("resourceID")); err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "Delete failed")
		return
	}
	_, policy, err := h.repository.GetConnection(c.Request.Context(), connectionID)
	if err != nil || !h.applySCIM(c, connectionID, policy) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) writeSCIMUser(c *gin.Context, connectionID string, input scimUser) error {
	_, policy, err := h.repository.GetConnection(c.Request.Context(), connectionID)
	if err != nil || !policy.SyncPeople {
		h.scimError(c, http.StatusForbidden, "mutability", "People synchronization is disabled")
		return errors.New("people disabled")
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	person := domain.Person{ExternalID: input.ID, ProviderSubject: input.ID, Username: input.UserName, DisplayName: input.DisplayName, Email: firstSCIMValue(input.Emails), Phone: firstSCIMValue(input.PhoneNumbers), Status: domain.ProjectionSuspended}
	if active {
		person.Status = domain.ProjectionActive
	}
	if err := h.repository.UpsertSCIMPerson(c.Request.Context(), connectionID, person); err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "User write failed")
		return err
	}
	if !h.applySCIM(c, connectionID, policy) {
		return errors.New("reconcile failed")
	}
	input.Schemas = []string{scimUserSchema}
	return nil
}
func (h *Handler) writeSCIMGroup(c *gin.Context, connectionID string, input scimGroup) error {
	org := domain.Organization{ExternalID: input.ID, ExternalParentID: input.ParentID, Name: input.DisplayName, Status: domain.ProjectionActive}
	if err := h.repository.UpsertSCIMOrganization(c.Request.Context(), connectionID, org); err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "Group write failed")
		return err
	}
	memberIDs := make([]string, 0, len(input.Members))
	for _, m := range input.Members {
		if strings.TrimSpace(m.Value) != "" {
			memberIDs = append(memberIDs, m.Value)
		}
	}
	if err := h.repository.ReplaceSCIMMemberships(c.Request.Context(), connectionID, input.ID, memberIDs); err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "Membership write failed")
		return err
	}
	_, policy, err := h.repository.GetConnection(c.Request.Context(), connectionID)
	if err != nil || !h.applySCIM(c, connectionID, policy) {
		return errors.New("reconcile failed")
	}
	input.Schemas = []string{scimGroupSchema}
	return nil
}
func (h *Handler) applySCIM(c *gin.Context, connectionID string, policy domain.Policy) bool {
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err == nil && !policy.SyncPeople {
		snapshot.People = nil
		snapshot.Memberships = nil
	}
	if err == nil {
		_, _, err = h.service.ApplyTriggered(c.Request.Context(), connectionID, snapshot, "scim", "scim")
	}
	if err != nil {
		h.scimError(c, http.StatusInternalServerError, "serverError", "SCIM reconciliation failed")
		return false
	}
	return true
}
func (h *Handler) scimConnection(c *gin.Context, scope string) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		h.scimError(c, http.StatusUnauthorized, "invalidToken", "Bearer token required")
		return "", false
	}
	connectionID, err := h.repository.ResolveSCIMConnectionForScope(c.Request.Context(), hashToken(token), scope)
	if err != nil {
		h.scimError(c, http.StatusUnauthorized, "invalidToken", "Invalid bearer token")
		return "", false
	}
	return connectionID, true
}
func (h *Handler) scimUserByID(c *gin.Context, connectionID, id string) (scimUser, bool) {
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err == nil {
		for _, item := range snapshot.People {
			if item.ExternalID == id {
				return personToSCIM(item), true
			}
		}
	}
	h.scimError(c, http.StatusNotFound, "notFound", "User not found")
	return scimUser{}, false
}
func (h *Handler) scimGroupByID(c *gin.Context, connectionID, id string) (scimGroup, bool) {
	snapshot, err := h.repository.SCIMSnapshot(c.Request.Context(), connectionID)
	if err == nil {
		members := []scimMember{}
		for _, m := range snapshot.Memberships {
			if m.ExternalOrganizationID == id {
				members = append(members, scimMember{Value: m.ExternalPersonID})
			}
		}
		for _, item := range snapshot.Organizations {
			if item.ExternalID == id {
				return organizationToSCIM(item, members), true
			}
		}
	}
	h.scimError(c, http.StatusNotFound, "notFound", "Group not found")
	return scimGroup{}, false
}
func (h *Handler) scimList(c *gin.Context, resources any) {
	c.Header("Content-Type", "application/scim+json")
	count := 0
	switch v := resources.(type) {
	case []scimUser:
		count = len(v)
	case []scimGroup:
		count = len(v)
	}
	c.JSON(http.StatusOK, gin.H{"schemas": []string{scimListSchema}, "totalResults": count, "startIndex": 1, "itemsPerPage": count, "Resources": resources})
}
func (h *Handler) scimError(c *gin.Context, status int, scimType, detail string) {
	c.Header("Content-Type", "application/scim+json")
	c.JSON(status, gin.H{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"}, "status": status, "scimType": scimType, "detail": detail})
}
func personToSCIM(item domain.Person) scimUser {
	active := item.Status == domain.ProjectionActive
	out := scimUser{Schemas: []string{scimUserSchema}, ID: item.ExternalID, UserName: item.Username, DisplayName: item.DisplayName, Active: &active}
	if item.Email != "" {
		out.Emails = []scimValue{{Value: item.Email, Primary: true, Type: "work"}}
	}
	if item.Phone != "" {
		out.PhoneNumbers = []scimValue{{Value: item.Phone, Primary: true, Type: "work"}}
	}
	return out
}
func organizationToSCIM(item domain.Organization, members []scimMember) scimGroup {
	return scimGroup{Schemas: []string{scimGroupSchema}, ID: item.ExternalID, DisplayName: item.Name, ParentID: item.ExternalParentID, Members: members}
}
func firstSCIMValue(values []scimValue) string {
	for _, value := range values {
		if value.Primary && value.Value != "" {
			return value.Value
		}
	}
	if len(values) > 0 {
		return values[0].Value
	}
	return ""
}

func validatePatch(patch scimPatch, supportedPaths map[string]bool) error {
	if len(patch.Schemas) != 1 || !strings.EqualFold(strings.TrimSpace(patch.Schemas[0]), scimPatchSchema) {
		return errors.New("PatchOp schema is required")
	}
	if len(patch.Operations) == 0 {
		return errors.New("PatchOp requires at least one operation")
	}
	for _, operation := range patch.Operations {
		op := strings.ToLower(strings.TrimSpace(operation.Op))
		if op != "add" && op != "replace" && op != "remove" {
			return errors.New("PatchOp operation is not supported")
		}
		path := strings.ToLower(strings.TrimSpace(operation.Path))
		if path == "" || !supportedPaths[path] {
			return errors.New("PatchOp path is not supported")
		}
	}
	return nil
}
func applyUserPatch(user *scimUser, patch scimPatch) {
	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch path {
		case "active":
			if value, ok := op.Value.(bool); ok {
				user.Active = &value
			}
		case "username":
			user.UserName = stringValue(op.Value)
		case "displayname":
			user.DisplayName = stringValue(op.Value)
		case "emails":
			user.Emails = scimValues(op.Value)
		case "phonenumbers":
			user.PhoneNumbers = scimValues(op.Value)
		}
	}
}
func applyGroupPatch(group *scimGroup, patch scimPatch) {
	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch path {
		case "displayname":
			group.DisplayName = stringValue(op.Value)
		case "parentid":
			group.ParentID = stringValue(op.Value)
		case "members":
			if strings.EqualFold(op.Op, "remove") {
				group.Members = nil
			} else {
				group.Members = scimMembers(op.Value)
			}
		}
	}
}
func stringValue(value any) string { result, _ := value.(string); return result }
func scimValues(value any) []scimValue {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []scimValue{}
	for _, item := range raw {
		record, ok := item.(map[string]any)
		if ok {
			out = append(out, scimValue{Value: stringValue(record["value"]), Type: stringValue(record["type"]), Primary: record["primary"] == true})
		}
	}
	return out
}
func scimMembers(value any) []scimMember {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []scimMember{}
	for _, item := range raw {
		record, ok := item.(map[string]any)
		if ok {
			out = append(out, scimMember{Value: stringValue(record["value"]), Display: stringValue(record["display"])})
		}
	}
	return out
}
