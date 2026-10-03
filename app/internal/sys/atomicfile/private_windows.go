package atomicfile

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func restrictPrivateFile(path string) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("could not identify the settings owner: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("could not identify the settings owner: %w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return fmt.Errorf("could not protect private settings: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("could not protect private settings: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("could not restrict private settings to this user: %w", err)
	}
	return nil
}
