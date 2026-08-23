package installer

import (
	"context"
	"errors"
	"fmt"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

type PlanAction string

const (
	ActionKeep  PlanAction = "keep"
	ActionWrite PlanAction = "write"
)

type PlanRequest struct {
	Mode string `json:"mode"`
}

type PathPermission struct {
	CanRead   bool     `json:"canRead"`
	CanWrite  bool     `json:"canWrite"`
	CanCreate bool     `json:"canCreate"`
	CanRename bool     `json:"canRename"`
	CanDelete bool     `json:"canDelete"`
	Reasons   []string `json:"reasons"`
}

type PlanEntry struct {
	Path       string         `json:"path"`
	Action     PlanAction     `json:"action"`
	Permission PathPermission `json:"permission"`
}

type Plan struct {
	SelectedUI      installstate.UI   `json:"selectedUi"`
	Mode            installstate.Mode `json:"mode"`
	CanCleanup      bool              `json:"canCleanup"`
	CanBuild        bool              `json:"canBuild"`
	CanWriteEnv     bool              `json:"canWriteEnv"`
	RequiresRestart bool              `json:"requiresRestart"`
	Entries         []PlanEntry       `json:"entries"`
	Reasons         []string          `json:"reasons"`
}

type PathInspector interface {
	Inspect(context.Context, string) (PathPermission, error)
}

// PlanProvider is the application boundary consumed by the HTTP installer
// transport. Keeping the interface here prevents transport and bootstrap from
// depending on a concrete filesystem implementation.
type PlanProvider interface {
	Plan(context.Context, PlanRequest) (Plan, error)
}

type PlanService struct {
	inspector PathInspector
	profiles  ProfileProvider
}

func NewPlanService(inspector PathInspector) *PlanService {
	return &PlanService{inspector: inspector}
}

// NewPlanServiceWithProfile takes UI selection from the read-only initializer
// profile; clients provide only a requested runtime mode.
func NewPlanServiceWithProfile(inspector PathInspector, profiles ProfileProvider) *PlanService {
	return &PlanService{inspector: inspector, profiles: profiles}
}

func (s *PlanService) Plan(ctx context.Context, request PlanRequest) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mode, err := validatePlanRequest(request)
	if err != nil {
		return Plan{}, err
	}
	if s == nil || s.inspector == nil || s.profiles == nil {
		return Plan{}, errors.New("installation path inspector is not configured")
	}
	profile, exists, err := s.profiles.Profile(ctx)
	if err != nil || !exists || !validProfile(profile) || profile.Installing {
		return Plan{}, errors.New("installation UI profile is not ready")
	}
	selectedUI := profile.SelectedUI

	paths := []string{
		"admin/apps/install",
		"admin/apps/web-" + string(selectedUI),
		".env",
	}
	selectedPath := "admin/apps/web-" + string(selectedUI)
	plan := Plan{
		SelectedUI:      selectedUI,
		Mode:            mode,
		CanCleanup:      true,
		CanBuild:        true,
		CanWriteEnv:     true,
		RequiresRestart: mode == installstate.ModeEmbedded,
		Entries:         make([]PlanEntry, 0, len(paths)),
	}

	for _, path := range paths {
		permission, inspectErr := s.inspector.Inspect(ctx, path)
		if inspectErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Plan{}, ctxErr
			}
			permission.Reasons = append(permission.Reasons, "path_unavailable")
		}
		action := actionForPath(path, selectedPath)
		entry := PlanEntry{Path: path, Action: action, Permission: permission}
		plan.Entries = append(plan.Entries, entry)

		switch action {
		case ActionWrite:
			if !permission.CanWrite || !permission.CanCreate || !permission.CanRename {
				plan.CanWriteEnv = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "configuration_not_writable")
			}
		case ActionKeep:
			if (path == "admin/apps/install" || path == selectedPath) && !permission.CanRead {
				plan.CanBuild = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "build_input_not_readable")
			}
		}
	}
	return plan, nil
}

func validatePlanRequest(request PlanRequest) (installstate.Mode, error) {
	mode := installstate.Mode(request.Mode)
	switch mode {
	case installstate.ModeEmbedded, installstate.ModeStandalone, installstate.ModeAPIOnly, installstate.ModeDev:
	default:
		return "", fmt.Errorf("unsupported UI mode %q", request.Mode)
	}
	return mode, nil
}

func actionForPath(path, selectedPath string) PlanAction {
	switch {
	case path == ".env":
		return ActionWrite
	default:
		return ActionKeep
	}
}

func appendPlanReasons(target []string, path string, reasons []string, fallback string) []string {
	if len(reasons) == 0 {
		return append(target, path+":"+fallback)
	}
	for _, reason := range reasons {
		target = append(target, path+":"+reason)
	}
	return target
}
