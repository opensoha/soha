package proxyadapter

import "testing"

func TestSupportsMihomoUDP(t *testing.T) {
	for _, tc := range []struct {
		protocol string
		want     bool
	}{
		{"socks5", true}, {"ss", true}, {"trojan", true}, {"vmess", true}, {"vless", true},
		{"http", false}, {"https", false}, {"ssr", false}, {"snell", false}, {"unknown", false}, {"", false},
		{"SOCKS5", false}, {"SS", false}, {"Trojan", false}, {"socks", false}, {"shadowsocks", false},
		{" socks5", false}, {"socks5 ", false}, {"socks5\n", false}, {"ss\x00", false},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			if got := SupportsMihomoUDP(tc.protocol); got != tc.want {
				t.Fatalf("SupportsMihomoUDP(%q)=%v, want %v", tc.protocol, got, tc.want)
			}
		})
	}
}
