//go:build windows

package sohacli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPrivateCredentialACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "owned")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateConfigDirectory(dir, true); err != nil {
		t.Fatal(err)
	}
	directorySD, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native directory ACL: %s", directorySD.String())
	if err := requirePrivateConfigDirectory(dir, false); err != nil {
		t.Fatalf("owned directory rejected: %v", err)
	}
	path := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(path, []byte("test-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restrictPrivateFile(path); err != nil {
		t.Fatal(err)
	}
	actual, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := actual.Control()
	acl, _, aclErr := actual.DACL()
	if err != nil || aclErr != nil || control&windows.SE_DACL_PROTECTED == 0 || acl == nil || acl.AceCount != 1 {
		t.Fatal("credential file ACL is not restricted to the current user")
	}
	// Only this disposable directory receives a deliberately broad ACL.
	broad, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err = broad.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := applyPrivateWindowsACL(dir, true); err != nil {
			t.Errorf("restore owned directory ACL: %v", err)
		}
	}()
	if err := requirePrivateConfigDirectory(dir, false); err == nil {
		t.Fatal("a directory writable by everyone was accepted")
	}
}
