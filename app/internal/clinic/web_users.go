package clinic

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

// WebUser is one CARE web account an operator can pick when resetting a
// web password. It never carries a password or hash.
type WebUser struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Superuser bool   `json:"superuser"`
}

const (
	webUsersMarker = "care-clinic:users:"
	webUsersLimit  = 5000
)

// Read-only: lists sign-in accounts, skipping CARE's soft-deleted users and
// service accounts when those fields exist.
var webUsersScript = `import json
from django.contrib.auth import get_user_model
from django.core.exceptions import FieldDoesNotExist
User = get_user_model()
users = User._default_manager.all()
for field, value in (("deleted", False), ("is_service_account", False)):
    try:
        User._meta.get_field(field)
    except FieldDoesNotExist:
        continue
    users = users.filter(**{field: value})
rows = []
for user in users.order_by(User.USERNAME_FIELD)[:` + strconv.Itoa(webUsersLimit) + `]:
    name = user.get_full_name() if hasattr(user, "get_full_name") else ""
    rows.append({
        "username": user.get_username(),
        "name": " ".join(str(name or "").split()),
        "active": bool(getattr(user, "is_active", True)),
        "superuser": bool(getattr(user, "is_superuser", False)),
    })
print("` + webUsersMarker + `" + json.dumps(rows))
`

// ListWebUsers returns CARE's web sign-in accounts so an operator can pick
// whose password to reset instead of typing a case-sensitive username.
func (e *Clinic) ListWebUsers() ([]WebUser, error) {
	running, err := e.backendRunning()
	if err != nil {
		e.logln("Couldn't check whether CARE is running: " + err.Error())
		return nil, ErrCareNotRunning
	}
	if !running {
		return nil, ErrCareNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), webPasswordTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "docker", "compose", "exec", "-T",
		"backend", "python", "manage.py", "shell", "-c", webUsersScript)
	cmd.Env = e.baseEnv()
	cmd.Dir = e.workdir()
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, webUsersMarker) {
				continue
			}
			var users []WebUser
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, webUsersMarker)), &users); err != nil {
				break
			}
			if users == nil {
				users = []WebUser{}
			}
			return users, nil
		}
	}
	detail := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		detail = "timed out after " + webPasswordTimeout.String() + "; " + detail
	}
	if runErr != nil {
		detail = runErr.Error() + ": " + detail
	}
	e.logln("Listing CARE web users failed: " + detail)
	return nil, errors.New("CARE couldn't list its web users. Open the log file for details")
}
