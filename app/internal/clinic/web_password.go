package clinic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

var ErrCareNotRunning = errors.New("CARE isn't running; start CARE from Overview, then try again")

const (
	webPasswordUserEnv     = "CARE_WEB_USERNAME"
	webPasswordSecretEnv   = "CARE_WEB_PASSWORD"
	webPasswordMissing     = "care-clinic:no-user"
	webPasswordInvalid     = "care-clinic:invalid:"
	webPasswordChanged     = "care-clinic:password-set"
	webPasswordTimeout     = 2 * time.Minute
	webPasswordMissingCode = 3
	webPasswordInvalidCode = 4
)

var webPasswordScript = fmt.Sprintf(webPasswordProgram, webPasswordMissingCode, webPasswordInvalidCode)

const webPasswordProgram = `import os, sys
from django.contrib.auth import get_user_model
from django.contrib.auth.password_validation import validate_password
from django.core.exceptions import ValidationError
User = get_user_model()
user = User._default_manager.filter(**{User.USERNAME_FIELD: os.environ["` + webPasswordUserEnv + `"]}).first()
if user is None:
    print("` + webPasswordMissing + `")
    sys.exit(%d)
password = os.environ["` + webPasswordSecretEnv + `"]
try:
    validate_password(password, user)
except ValidationError as error:
    for message in error.messages:
        print("` + webPasswordInvalid + `" + " ".join(str(message).split()))
    sys.exit(%d)
user.set_password(password)
user.save()
print("` + webPasswordChanged + `")
`

func (e *Clinic) backendRunning() (bool, error) {
	out, err := e.Status()
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "backend running" {
			return true, nil
		}
	}
	return false, nil
}

func (e *Clinic) ResetWebPassword(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("enter the CARE username whose password should change")
	}
	if password == "" {
		return errors.New("enter a new CARE web password")
	}
	running, err := e.backendRunning()
	if err != nil {
		e.logln("Couldn't check whether CARE is running: " + err.Error())
		return ErrCareNotRunning
	}
	if !running {
		return ErrCareNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), webPasswordTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "compose", "exec", "-T",
		"-e", webPasswordUserEnv, "-e", webPasswordSecretEnv,
		"backend", "python", "manage.py", "shell", "-c", webPasswordScript)
	cmd.Env = append(e.baseEnv(), webPasswordUserEnv+"="+username, webPasswordSecretEnv+"="+password)
	cmd.Dir = e.workdir()
	out, runErr := cmd.CombinedOutput()
	output := strings.ReplaceAll(string(out), password, "[new password]")
	missing, changed := false, false
	var problems []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == webPasswordMissing:
			missing = true
		case line == webPasswordChanged:
			changed = true
		case strings.HasPrefix(line, webPasswordInvalid):
			if message := strings.TrimSpace(strings.TrimPrefix(line, webPasswordInvalid)); message != "" {
				problems = append(problems, message)
			}
		}
	}
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	switch {
	case runErr == nil && changed:
		e.logln("Changed the CARE web password for " + username + ".")
		return nil
	case missing && code == webPasswordMissingCode:
		return fmt.Errorf("no CARE user named %q; usernames are case-sensitive", username)
	case len(problems) > 0 && code == webPasswordInvalidCode:
		return errors.New("CARE didn't accept that password: " + strings.Join(problems, " "))
	}
	detail := strings.TrimSpace(output)
	if ctx.Err() != nil {
		detail = "timed out after " + webPasswordTimeout.String() + "; " + detail
	}
	if runErr != nil {
		detail = runErr.Error() + ": " + detail
	}
	e.logln("CARE web password change for " + username + " failed: " + detail)
	return errors.New("CARE couldn't change that web password; nothing was confirmed. Open the log file for details")
}
