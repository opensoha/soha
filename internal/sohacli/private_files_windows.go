//go:build windows

package sohacli

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

func privateWindowsDescriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	inherit := ""
	if directory {
		inherit = "OICI"
	}
	return windows.SecurityDescriptorFromString("D:P(A;" + inherit + ";FA;;;" + user.User.Sid.String() + ")")
}

func applyPrivateWindowsACL(path string, directory bool) error {
	sd, err := privateWindowsDescriptor(directory)
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func requirePrivateConfigDirectory(dir string, created bool) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("profile config directory must be a regular directory")
	}
	home, homeErr := os.UserHomeDir()
	if created || homeErr == nil && filepath.Clean(dir) == filepath.Join(home, ".soha") {
		return applyPrivateWindowsACL(dir, true)
	}
	if err := requirePrivateWindowsACL(dir, true); err != nil {
		return fmt.Errorf("profile config requires a directory ACL restricted to the current user; choose a dedicated config directory")
	}
	return nil
}

func requirePrivateWindowsACL(path string, directory bool) error {
	actual, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	control, _, err := actual.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("credential ACL must be protected")
	}
	acl, _, err := actual.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		return fmt.Errorf("credential ACL must contain exactly one user grant")
	}
	expected, err := privateWindowsDescriptor(directory)
	if err != nil {
		return err
	}
	wantACL, _, err := expected.DACL()
	if err != nil {
		return err
	}
	var ace, want *windows.ACCESS_ALLOWED_ACE
	if err = windows.GetAce(acl, 0, &ace); err != nil {
		return err
	}
	if err = windows.GetAce(wantACL, 0, &want); err != nil {
		return err
	}
	// Windows may add automatic-inheritance control flags to the descriptor.
	// Compare the protected grant itself, including its SID, mask and ACE flags.
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != want.Mask || ace.Header.AceFlags != want.Header.AceFlags || !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals((*windows.SID)(unsafe.Pointer(&want.SidStart))) {
		return fmt.Errorf("credential ACL does not match the current user grant")
	}
	return nil
}

func restrictPrivateFile(path string) error { return applyPrivateWindowsACL(path, false) }

func atomicReplaceFile(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
