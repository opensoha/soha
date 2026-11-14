package proxyadapter

// SupportsMihomoUDP reports eligibility in the controlled native Mihomo protocol
// set. Input must already be normalized by the source converter: aliases,
// whitespace and case variants are rejected. An explicit udp:false remains false
// at the caller. Eligibility does not prove an upstream server supports UDP and
// never bypasses authorization, destination protection, rules or lease checks.
func SupportsMihomoUDP(protocol string) bool {
	switch protocol {
	case "socks5", "ss", "trojan", "vmess", "vless":
		return true
	default:
		return false
	}
}
