package installer

import (
	"context"
	"errors"
	"fmt"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
)

type PlanAction string

const (
	ActionKeep   PlanAction = "keep"
	ActionRemove PlanAction = "remove"
	ActionCreate PlanAction = "create"
	ActionWrite  PlanAction = "write"
)

type PlanRequest struct {
	SelectedUI string `json:"selectedUi"`
	Mode       string `json:"mode"`
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

type PlanService struct {
	inspector PathInspector
}

func NewPlanService(inspector PathInspector) *PlanService {
	return &PlanService{inspector: inspector}
}

func (s *PlanService) Plan(ctx context.Context, request PlanRequest) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	selectedUI, mode, err := validatePlanRequest(request)
	if err != nil {
		return Plan{}, err
	}
	if s == nil || s.inspector == nil {
		return Plan{}, errors.New("installation path inspector is not configured")
	}

	paths := []string{
		"install",
		"admin/apps/web-antd",
		"admin/apps/web-ele",
		"admin/apps/web-naive",
		"admin/apps/web",
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
		case ActionRemove:
			if !permission.CanRead || !permission.CanRename || !permission.CanDelete {
				plan.CanCleanup = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "cleanup_not_available")
			}
		case ActionCreate:
			if !permission.CanCreate || !permission.CanRename {
				plan.CanCleanup = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "target_not_writable")
			}
		case ActionWrite:
			if !permission.CanWrite || !permission.CanCreate || !permission.CanRename {
				plan.CanWriteEnv = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "configuration_not_writable")
			}
		case ActionKeep:
			if (path == "install" || path == selectedPath) && !permission.CanRead {
				plan.CanBuild = false
				plan.Reasons = appendPlanReasons(plan.Reasons, path, permission.Reasons, "build_input_not_readable")
			}
		}
	}
	return plan, nil
}

func validatePlanRequest(request PlanRequest) (installstate.UI, installstate.Mode, error) {
	ui := installstate.UI(request.SelectedUI)
	switch ui {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
	default:
		return "", "", fmt.Errorf("unsupported management UI %q", request.SelectedUI)
	}
	mode := installstate.Mode(request.Mode)
	switch mode {
	case installstate.ModeEmbedded, installstate.ModeStandalone, installstate.ModeAPIOnly, installstate.ModeDev:
	default:
		return "", "", fmt.Errorf("unsupported UI mode %q", request.Mode)
	}
	return ui, mode, nil
}

func actionForPath(path, selectedPath string) PlanAction {
	switch {
	case path == "install" || path == selectedPath:
		return ActionKeep
	case path == "admin/apps/web":
		return ActionCreate
	case path == ".env":
		return ActionWrite
	default:
		return ActionRemove
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
