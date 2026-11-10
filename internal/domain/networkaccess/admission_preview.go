package networkaccess

type AdmissionPreviewSnapshot struct {
	Subject          Subject
	Device           Device
	Site             Site
	Binding          NASBinding
	Policies         PolicySnapshot
	Profiles         map[string]SiteProfileBinding
	Presence         PresenceRecord
	ResponsePolicies []EndpointResponsePolicy
	Restriction      EndpointRestriction
}
