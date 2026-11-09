//go:build !linux

package radiusruntime

import (
	"net"
	"os"
	"os/exec"
)

func peerCredentials(net.Conn) (uint32, int, error) { return 0, 0, ErrDenied }
func processStartToken(int) (string, error)         { return "", ErrDenied }
func configureChild(*exec.Cmd)                      {}
func lockOwner(*os.File) error                      { return ErrDenied }
func ownership(os.FileInfo) (uint32, uint32, bool)  { return 0, 0, false }
func supportedHost() bool                           { return false }
