package main

import (
	"fmt"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/mdns"
)

type SetupIssue struct {
	Step    string `json:"step"`
	Message string `json:"message"`
}

// ValidateSetup is read-only apart from the existing temporary folder write
// probe. RunSetup repeats it under the exclusive job lock before starting work.
func (a *App) ValidateSetup(name, password, backupDir string) (issues []SetupIssue, err error) {
	err = a.withReadJob(func() error {
		if err := a.requireServer(); err != nil {
			return err
		}
		issues = a.validateSetup(name, password, backupDir)
		return nil
	})
	return issues, err
}

func (a *App) validateSetup(name, password, backupDir string) []SetupIssue {
	issues := []SetupIssue{}
	add := func(step, message, detail string) {
		issues = append(issues, SetupIssue{Step: step, Message: message})
		if detail != "" {
			a.logln(fmt.Sprintf("Setup check (%s): %s", step, detail))
		}
	}
	cfg := a.loadConfig()
	if cfg.SetupDone || cfg.Removing || cfg.AdminPwHash != "" {
		add("cleanup", "An earlier installation needs attention before setup can continue.", "")
		return issues
	}
	if disk := a.DiskStatus(); !disk.OK || disk.Need == 0 {
		add("space", "There isn't enough confirmed free space to install CARE. Check this step again.", disk.Message)
	}
	wsl := a.WSLStatus()
	if restart := a.RestartPlan(); wsl.Applicable && (!wsl.OK || restart.Needed) {
		message := "Windows setup needs another look."
		if restart.Needed {
			message = "Windows still needs to restart."
		}
		add("windows", message, wsl.Message)
	}
	docker := a.DockerStatus()
	git := a.GitStatus()
	if !docker.OK || !git.OK {
		add("software", "The required software is no longer ready.", docker.Message+" "+git.Message)
	}
	if docker.OK {
		report, err := a.scanResidue()
		if err != nil {
			add("cleanup", "The earlier setup couldn't be checked. Check this step again.", err.Error())
		} else if !report.Clean {
			add("cleanup", "Something from an earlier setup is still on this computer.", "")
		}
	}
	if network := a.NetworkStatus(); network.Applicable && !network.OK {
		add("network", "This network's profile needs another look.", network.Message)
	}
	if err := mdns.ValidateLabel(name); err != nil {
		add("address", "Choose a valid clinic name before installing.", err.Error())
	} else if err := mdns.CheckAvailable(name); err != nil {
		add("address", "The clinic address is no longer confirmed as free. Check it again.", err.Error())
	}
	if problem := a.ValidateBackupDir(backupDir); problem != "" {
		add("backup", "The backup location can't be used. Reconnect it or choose a different location.", problem)
	}
	recovery := setupRecoveryStatus(cfg)
	if !recovery.BackupSaved || !recovery.BackupVerified {
		add("backup", "Save and check the backup recovery file before installing.", recovery.BackupProblem)
	} else if err := a.recoveryLocation(cfg.BackupRecoveryPath, backupDir); err != nil {
		add("backup", "Keep the recovery file separate from CARE's own folders.", err.Error())
	}
	if !recovery.CodesSaved || recovery.CodesProblem != "" {
		add("admin", "The recovery codes file is unavailable. Save a fresh set; the old codes stop working.", recovery.CodesProblem)
	} else if err := a.recoveryLocation(cfg.AdminRecoveryPath, backupDir); err != nil {
		add("admin", "Keep the recovery codes outside CARE's own folders.", err.Error())
	}
	if err := ValidatePassword(password); err != nil {
		add("admin", "Choose a password with 8 to 20 characters, upper and lowercase letters, and a number.", err.Error())
	}
	// Multiple causes can point to the same screen; keep one actionable row.
	seen := make(map[string]bool)
	result := make([]SetupIssue, 0, len(issues))
	for _, issue := range issues {
		if !seen[issue.Step] {
			result = append(result, issue)
			seen[issue.Step] = true
		}
	}
	return result
}
