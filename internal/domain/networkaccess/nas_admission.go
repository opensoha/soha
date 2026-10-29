package networkaccess

import "github.com/opensoha/soha/internal/networkprotocol"

type NASAdmissionSnapshot struct {
	Session                         Session
	Subject                         Subject
	Device                          Device
	Site                            Site
	Binding                         NASBinding
	Policies                        PolicySnapshot
	Profiles                        map[string]SiteProfileBinding
	StrongBinding, CredentialActive bool
	FloorProfile, InputsHash        string
	RadiusAttributes                *networkprotocol.RadiusAttributes
	Revision                        int64
	Attempts                        int
}

type NASAdmissionMutation struct {
	Profile, InputsHash, Reason string
	PolicyVersion               int
	RadiusAttributes            *networkprotocol.RadiusAttributes
	Command                     *SessionCommand
	Attempts                    int
}
