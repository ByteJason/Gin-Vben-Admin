package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
	platformi18n "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/i18n"
)

var (
	ErrAlreadyInstalled = errors.New("application is already installed")
	ErrApplyBusy        = errors.New("installation is already running")
	ErrInvalidApply     = errors.New("installation request is invalid")
	ErrPreflightFailed  = errors.New("installation preflight failed")
	ErrApplyFailed      = errors.New("installation apply failed")
	ErrApplyRollback    = errors.New("installation rollback failed")
)

type applyStageError struct {
	stage string
	err   error
}

func (e *applyStageError) Error() string {
	return fmt.Sprintf("installation stage %s: %v", e.stage, e.err)
}

func (e *applyStageError) Unwrap() error {
	return e.err
}

func withApplyFailureStage(stage string, err error) error {
	if err == nil || !validApplyFailureStage(stage) {
		return err
	}
	return &applyStageError{stage: stage, err: err}
}

func applyFailureStage(err error) string {
	var staged *applyStageError
	if errors.As(err, &staged) && validApplyFailureStage(staged.stage) {
		return staged.stage
	}
	return ""
}

func validApplyFailureStage(stage string) bool {
	switch stage {
	case "request", "coordination", "plan", "database", "redis", "recovery", "journal", "schema", "identity", "environment", "marker", "lock", "complete":
		return true
	default:
		return false
	}
}

type AdminAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ApplyRequest struct {
	Mode       string             `json:"mode"`
	Locale     string             `json:"locale,omitempty"`
	LocaleMode string             `json:"localeMode,omitempty"`
	Database   DatabaseConnection `json:"database"`
	Redis      RedisConnection    `json:"redis"`
	Admin      AdminAccount       `json:"admin"`
}

type StepStatus string

const StepCompleted StepStatus = "completed"

type ApplyStep struct {
	ID     string     `json:"id"`
	Status StepStatus `json:"status"`
}

type ApplyResult struct {
	State       State             `json:"state"`
	SelectedUI  installstate.UI   `json:"selectedUi"`
	Mode        installstate.Mode `json:"mode"`
	InstalledAt time.Time         `json:"installedAt"`
	Steps       []ApplyStep       `json:"steps"`
}

type SchemaReceipt struct {
	Version         uint
	PreviousVersion uint
	AppliedSteps    uint
}

type IdentityReceipt struct {
	Reference string `json:"reference"`
}

type EnvironmentReceipt struct {
	Digest         string `json:"digest"`
	Reference      string `json:"reference"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	BackupName     string `json:"backupName,omitempty"`
	Replaced       bool   `json:"replaced,omitempty"`
}

type ApplyMarkerStore interface {
	Load(context.Context) (installstate.Marker, bool, error)
	Create(context.Context, installstate.Marker) error
	Remove(context.Context, installstate.Marker) error
}

type ApplyDependencyChecker interface {
	CheckDatabase(context.Context, DatabaseConnection) (DependencyCheck, error)
	CheckRedis(context.Context, RedisConnection) (DependencyCheck, error)
}

type SchemaInstaller interface {
	Up(context.Context, DatabaseConnection) (SchemaReceipt, error)
}

type IdentityInstaller interface {
	Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error)
	Rollback(context.Context, IdentityReceipt) error
}

type IdentityRecovery interface {
	RecoverRollback(context.Context, DatabaseConnection, IdentityReceipt) error
}

// IdentityFinalizer forgets in-memory database connection material retained
// solely for same-process rollback once the installation marker commits.
type IdentityFinalizer interface {
	Finalize(context.Context, IdentityReceipt) error
}

type PreparedIdentityInstaller interface {
	InitializeWithReference(context.Context, DatabaseConnection, AdminAccount, string) (IdentityReceipt, error)
}

type EnvironmentInstaller interface {
	Publish(context.Context, ApplyRequest, Plan) (EnvironmentReceipt, error)
	Rollback(context.Context, EnvironmentReceipt) error
}

type PreparedEnvironmentInstaller interface {
	PublishWithReference(context.Context, ApplyRequest, Plan, string) (EnvironmentReceipt, error)
	RecoverPrepared(context.Context, string) error
}

// EnvironmentFinalizer is the post-commit seam for deleting the exact
// transaction-owned pre-install environment backup. Finalization happens only
// after the marker is atomically visible and is idempotent across restart.
type EnvironmentFinalizer interface {
	Finalize(context.Context, EnvironmentReceipt) error
}

type ApplyService struct {
	markers      ApplyMarkerStore
	plans        PlanProvider
	dependencies ApplyDependencyChecker
	schemas      SchemaInstaller
	identity     IdentityInstaller
	environment  EnvironmentInstaller
	journal      ApplyTransactionJournal
	now          func() time.Time
	mutex        sync.Mutex
	pending      *pendingRollback
}

type pendingRollback struct {
	identity          *IdentityReceipt
	environment       *EnvironmentReceipt
	marker            *installstate.Marker
	transaction       *ApplyTransaction
	environmentIntent string
}

func NewApplyService(markers ApplyMarkerStore, plans PlanProvider, dependencies ApplyDependencyChecker, schemas SchemaInstaller, identity IdentityInstaller, environment EnvironmentInstaller, journal ApplyTransactionJournal, now func() time.Time) *ApplyService {
	if now == nil {
		now = time.Now
	}
	return &ApplyService{
		markers: markers, plans: plans, dependencies: dependencies, schemas: schemas,
		identity: identity, environment: environment, journal: journal, now: now,
	}
}

func (s *ApplyService) Apply(ctx context.Context, request ApplyRequest) (ApplyResult, error) {
	return s.apply(ctx, request, nil)
}

// ApplyWithProgress is the asynchronous-install seam. The callback receives
// only allowlisted stage identifiers and never receives credentials or raw
// infrastructure errors.
func (s *ApplyService) ApplyWithProgress(ctx context.Context, request ApplyRequest, report func(string)) (ApplyResult, error) {
	return s.apply(ctx, request, report)
}

func (s *ApplyService) apply(ctx context.Context, request ApplyRequest, report func(string)) (result ApplyResult, returnErr error) {
	if s == nil || s.markers == nil || s.plans == nil || s.dependencies == nil || s.schemas == nil || s.identity == nil || s.environment == nil {
		return ApplyResult{}, withApplyFailureStage("plan", errors.New("installation apply service is not configured"))
	}
	if !s.mutex.TryLock() {
		return ApplyResult{}, withApplyFailureStage("coordination", ErrApplyBusy)
	}
	defer s.mutex.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, withApplyFailureStage("request", err)
	}
	mode, err := validateApplyRequest(request)
	if err != nil {
		return ApplyResult{}, withApplyFailureStage("request", err)
	}
	databaseTarget, err := databaseTargetDigest(request.Database)
	if err != nil {
		return ApplyResult{}, withApplyFailureStage("request", ErrInvalidApply)
	}
	releaseOwnership, err := s.acquireApplyOwnership(ctx)
	if err != nil {
		return ApplyResult{}, withApplyFailureStage("coordination", err)
	}
	defer func() {
		if releaseErr := releaseOwnership(); releaseErr != nil {
			result = ApplyResult{}
			returnErr = errors.Join(returnErr, withApplyFailureStage("coordination", ErrPreflightFailed))
		}
	}()
	_, installed, err := s.markers.Load(ctx)
	if err != nil {
		return ApplyResult{}, withApplyFailureStage("lock", errors.New("read installation lock"))
	}
	if installed {
		_ = s.reconcileCompleted(ctx)
		return ApplyResult{}, withApplyFailureStage("lock", ErrAlreadyInstalled)
	}
	var interrupted *ApplyTransaction
	if s.journal != nil {
		if transaction, exists, err := s.journal.Load(ctx); err != nil {
			if errors.Is(err, ErrApplyBusy) {
				return ApplyResult{}, withApplyFailureStage("coordination", ErrApplyBusy)
			}
			return ApplyResult{}, withApplyFailureStage("journal", ErrPreflightFailed)
		} else if exists {
			if err := transaction.Validate(); err != nil {
				return ApplyResult{}, withApplyFailureStage("journal", ErrPreflightFailed)
			}
			if transaction.DatabaseTarget != databaseTarget {
				return ApplyResult{}, withApplyFailureStage("journal", ErrPreflightFailed)
			}
			interrupted = &transaction
		}
	}

	steps := make([]ApplyStep, 0, 7)
	plan, err := s.plans.Plan(ctx, PlanRequest{Mode: string(mode)})
	if err != nil || !validProfile(InstallationProfile{SelectedUI: plan.SelectedUI}) || plan.Mode != mode || !plan.CanCleanup || !plan.CanBuild || !plan.CanWriteEnv {
		return ApplyResult{}, withApplyFailureStage("plan", ErrPreflightFailed)
	}
	steps = appendCompleted(steps, "plan")
	reportApplyStep(report, "plan")

	database, err := s.dependencies.CheckDatabase(ctx, request.Database)
	if err != nil || !database.OK {
		return ApplyResult{}, withApplyFailureStage("database", ErrPreflightFailed)
	}
	steps = appendCompleted(steps, "database")
	reportApplyStep(report, "database")
	redis, err := s.dependencies.CheckRedis(ctx, request.Redis)
	if err != nil || !redis.OK {
		return ApplyResult{}, withApplyFailureStage("redis", ErrPreflightFailed)
	}
	steps = appendCompleted(steps, "redis")
	reportApplyStep(report, "redis")
	if interrupted != nil {
		if interrupted.SelectedUI != plan.SelectedUI {
			return ApplyResult{}, withApplyFailureStage("recovery", ErrPreflightFailed)
		}
		if err := s.recoverInterrupted(ctx, request.Database, interrupted); err != nil {
			return ApplyResult{}, withApplyFailureStage("recovery", err)
		}
	}

	var transaction *ApplyTransaction
	if s.journal != nil {
		id, err := newJobID()
		if err != nil {
			return ApplyResult{}, withApplyFailureStage("journal", errors.New("create installation transaction"))
		}
		transaction = &ApplyTransaction{
			Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner, ID: id,
			SelectedUI: plan.SelectedUI, Mode: mode, DatabaseTarget: databaseTarget, Phase: TransactionApplying,
			CurrentStep: "schema", CompletedSteps: []string{"plan", "database", "redis"},
			UpdatedAt: s.now().UTC(),
		}
		if err := transaction.Validate(); err != nil {
			return ApplyResult{}, withApplyFailureStage("journal", ErrPreflightFailed)
		}
		if err := s.journal.Create(ctx, *transaction); err != nil {
			if errors.Is(err, ErrApplyBusy) {
				return ApplyResult{}, withApplyFailureStage("coordination", ErrApplyBusy)
			}
			return ApplyResult{}, withApplyFailureStage("journal", errors.New("create installation transaction journal"))
		}
	}

	if _, err := s.schemas.Up(ctx, request.Database); err != nil {
		s.persistFailure(ctx, transaction, nil)
		return ApplyResult{}, withApplyFailureStage("schema", errors.New("install database schema"))
	}
	steps = appendCompleted(steps, "schema")
	reportApplyStep(report, "schema")
	if err := s.advanceTransaction(ctx, transaction, "identity", "schema"); err != nil {
		s.persistFailure(ctx, transaction, nil)
		return ApplyResult{}, withApplyFailureStage("journal", err)
	}
	s.pending = &pendingRollback{transaction: transaction}
	identityReceipt := IdentityReceipt{}
	preparedIdentity := false
	if transaction != nil {
		if prepared, ok := s.identity.(PreparedIdentityInstaller); ok {
			preparedIdentity = true
			identityReceipt = IdentityReceipt{Reference: transaction.ID}
			transaction.Identity = &identityReceipt
			transaction.UpdatedAt = s.now().UTC()
			if err := s.journal.Update(ctx, *transaction); err != nil {
				s.persistFailure(ctx, transaction, nil)
				s.pending = nil
				return ApplyResult{}, withApplyFailureStage("journal", errors.New("prepare identity recovery journal"))
			}
			s.pending.identity = &identityReceipt
			created, initializeErr := prepared.InitializeWithReference(ctx, request.Database, request.Admin, identityReceipt.Reference)
			if initializeErr == nil && created.Reference != identityReceipt.Reference {
				initializeErr = errors.New("identity installer changed prepared reference")
			}
			err = initializeErr
		} else {
			identityReceipt, err = s.identity.Initialize(ctx, request.Database, request.Admin)
		}
	} else {
		identityReceipt, err = s.identity.Initialize(ctx, request.Database, request.Admin)
	}
	if identityReceipt != (IdentityReceipt{}) {
		s.pending.identity = &identityReceipt
	}
	if err != nil {
		var rollbackErr error
		if preparedIdentity {
			rollbackErr = s.rollbackPreparedIdentity(request.Database, identityReceipt)
		} else {
			rollbackErr = s.rollback(ctx, nil, receiptPointer(identityReceipt))
		}
		if rollbackErr == nil {
			s.pending = nil
		}
		if transaction != nil && identityReceipt != (IdentityReceipt{}) {
			transaction.Identity = &identityReceipt
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("identity", rollbackErr)
	}
	steps = appendCompleted(steps, "identity")
	reportApplyStep(report, "identity")
	if transaction != nil {
		transaction.Identity = &identityReceipt
	}
	if err := s.advanceTransaction(ctx, transaction, "environment", "identity"); err != nil {
		rollbackErr := s.rollback(ctx, nil, &identityReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("journal", rollbackErr)
	}
	environmentReceipt := EnvironmentReceipt{}
	preparedEnvironment := false
	if transaction != nil {
		if prepared, ok := s.environment.(PreparedEnvironmentInstaller); ok {
			preparedEnvironment = true
			transaction.EnvironmentIntent = transaction.ID
			transaction.UpdatedAt = s.now().UTC()
			if err := s.journal.Update(ctx, *transaction); err != nil {
				rollbackErr := s.rollback(ctx, nil, &identityReceipt)
				if rollbackErr == nil {
					s.pending = nil
				}
				s.persistFailure(ctx, transaction, rollbackErr)
				return ApplyResult{}, applyFailure("journal", rollbackErr)
			}
			s.pending.environmentIntent = transaction.EnvironmentIntent
			environmentReceipt, err = prepared.PublishWithReference(ctx, request, plan, transaction.EnvironmentIntent)
			if err == nil && environmentReceipt.Reference != transaction.EnvironmentIntent {
				err = errors.New("environment installer changed prepared reference")
			}
		} else {
			environmentReceipt, err = s.environment.Publish(ctx, request, plan)
		}
	} else {
		environmentReceipt, err = s.environment.Publish(ctx, request, plan)
	}
	if environmentReceipt != (EnvironmentReceipt{}) {
		s.pending.environment = &environmentReceipt
	}
	if err != nil {
		var rollbackErr error
		if preparedEnvironment && environmentReceipt == (EnvironmentReceipt{}) {
			rollbackErr = s.rollbackPreparedEnvironment(transaction.EnvironmentIntent, &identityReceipt)
		} else {
			rollbackErr = s.rollback(ctx, receiptPointer(environmentReceipt), &identityReceipt)
		}
		if rollbackErr == nil {
			s.pending = nil
		}
		if transaction != nil {
			transaction.Identity = &identityReceipt
			if environmentReceipt != (EnvironmentReceipt{}) {
				transaction.Environment = &environmentReceipt
			}
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("environment", rollbackErr)
	}
	steps = appendCompleted(steps, "environment")
	reportApplyStep(report, "environment")
	if transaction != nil {
		transaction.Environment = &environmentReceipt
	}
	if err := s.advanceTransaction(ctx, transaction, "lock", "environment"); err != nil {
		rollbackErr := s.rollback(ctx, &environmentReceipt, &identityReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("journal", rollbackErr)
	}

	installedAt := s.now().UTC()
	profileHash, manifestHash := installationIntegrityDigests(plan.SelectedUI, mode)
	finalMarker := installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: CurrentInstallerVersion,
		InstalledAt:      installedAt,
		SelectedUI:       plan.SelectedUI,
		Mode:             mode,
		// These legacy fields now protect the selected UI profile and the
		// installation manifest. They no longer represent a frontend build;
		// `pnpm run build` intentionally remains a post-install action.
		ArtifactHash: profileHash,
		ManifestHash: manifestHash,
	}
	if err := finalMarker.Validate(); err != nil {
		rollbackErr := s.rollback(ctx, &environmentReceipt, &identityReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("marker", rollbackErr)
	}
	s.pending.marker = &finalMarker
	if transaction != nil {
		transaction.Marker = &finalMarker
		transaction.UpdatedAt = s.now().UTC()
		if err := s.journal.Update(ctx, *transaction); err != nil {
			rollbackErr := s.rollback(ctx, &environmentReceipt, &identityReceipt)
			if rollbackErr == nil {
				s.pending = nil
			}
			s.persistFailure(ctx, transaction, rollbackErr)
			return ApplyResult{}, applyFailure("journal", rollbackErr)
		}
	}
	if err := s.markers.Create(ctx, finalMarker); err != nil {
		rollbackErr := errors.Join(s.rollbackMarker(finalMarker), s.rollback(ctx, &environmentReceipt, &identityReceipt))
		if rollbackErr == nil {
			s.pending = nil
		}
		s.persistFailure(ctx, transaction, rollbackErr)
		return ApplyResult{}, applyFailure("lock", rollbackErr)
	}
	steps = appendCompleted(steps, "lock")
	reportApplyStep(report, "lock")
	s.pending = nil
	completionCtx, cancelCompletion := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCompletion()
	finalizeErr := errors.Join(
		s.finalizeEnvironment(completionCtx, environmentReceipt),
		s.finalizeIdentity(completionCtx, identityReceipt),
	)
	if transaction != nil {
		// The atomic marker is deliberately published before the journal is
		// removed. A crash can therefore leave a recoverable journal next to a
		// valid marker, but can never expose "installed" before prior steps.
		if housekeeper, ok := s.journal.(CompletionHousekeeper); ok {
			finalizeErr = errors.Join(finalizeErr, housekeeper.CleanupCompleted(completionCtx, finalMarker))
		}
		// A valid marker is the commit point, so finalization failure does not
		// turn an installed result into a rollback. Keeping the credential-free
		// journal provides the exact receipt for startup/status reconciliation.
		if finalizeErr == nil {
			_ = s.journal.Remove(completionCtx, transaction.ID)
		}
	}
	return ApplyResult{State: StateInstalled, SelectedUI: plan.SelectedUI, Mode: mode, InstalledAt: installedAt, Steps: steps}, nil
}

func (s *ApplyService) rollbackPreparedIdentity(database DatabaseConnection, receipt IdentityReceipt) error {
	recovery, ok := s.identity.(IdentityRecovery)
	if !ok {
		return ErrApplyRollback
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := recovery.RecoverRollback(rollbackCtx, database, receipt); err != nil {
		return ErrApplyRollback
	}
	return nil
}

func (s *ApplyService) rollbackPreparedEnvironment(reference string, identity *IdentityReceipt) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var rollbackErrors []error
	prepared, ok := s.environment.(PreparedEnvironmentInstaller)
	if !ok || prepared.RecoverPrepared(rollbackCtx, reference) != nil {
		rollbackErrors = append(rollbackErrors, ErrApplyRollback)
	}
	if identity != nil && *identity != (IdentityReceipt{}) {
		if err := s.identity.Rollback(rollbackCtx, *identity); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	return errors.Join(rollbackErrors...)
}

// ReconcileCompleted is the startup/status housekeeping seam for a crash in
// the tiny interval after the atomic marker rename and before journal removal.
// It never removes an admin-init envelope, an invalid record, or a transaction
// whose complete persisted marker differs from the valid installed marker.
func (s *ApplyService) ReconcileCompleted(ctx context.Context) (returnErr error) {
	if s == nil || s.markers == nil || s.journal == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// A pristine server has no completed state to reconcile. Check before
	// acquiring the durable process lease so ordinary pre-init startup remains
	// read-only and does not create runtime lock artifacts. The marker is read
	// again under ownership below before any cleanup occurs.
	if _, installed, err := s.markers.Load(ctx); err != nil {
		return fmt.Errorf("read installation marker during reconciliation: %w", err)
	} else if !installed {
		return nil
	}
	releaseOwnership, err := s.acquireApplyOwnership(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := releaseOwnership(); releaseErr != nil {
			returnErr = errors.Join(returnErr, ErrPreflightFailed)
		}
	}()
	return s.reconcileCompleted(ctx)
}

func (s *ApplyService) reconcileCompleted(ctx context.Context) error {
	marker, installed, err := s.markers.Load(ctx)
	if err != nil {
		return fmt.Errorf("read installation marker during reconciliation: %w", err)
	}
	if !installed {
		return nil
	}
	if err := marker.Validate(); err != nil {
		return fmt.Errorf("validate installation marker during reconciliation: %w", err)
	}
	transaction, exists, err := s.journal.Load(ctx)
	if errors.Is(err, ErrApplyBusy) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read installation transaction during reconciliation: %w", err)
	}
	if !exists {
		if housekeeper, ok := s.journal.(CompletionHousekeeper); ok {
			if err := housekeeper.CleanupCompleted(ctx, marker); err != nil {
				return fmt.Errorf("clean completed installation backup: %w", err)
			}
		}
		return nil
	}
	if err := transaction.Validate(); err != nil {
		return fmt.Errorf("validate installation transaction during reconciliation: %w", err)
	}
	if !completedTransactionMatchesMarker(transaction, marker) {
		return nil
	}
	if transaction.Environment != nil {
		if err := s.finalizeEnvironment(ctx, *transaction.Environment); err != nil {
			return fmt.Errorf("finalize completed installation environment: %w", err)
		}
	}
	if transaction.Identity != nil {
		if err := s.finalizeIdentity(ctx, *transaction.Identity); err != nil {
			return fmt.Errorf("finalize completed installation identity: %w", err)
		}
	}
	if housekeeper, ok := s.journal.(CompletionHousekeeper); ok {
		if err := housekeeper.CleanupCompleted(ctx, marker); err != nil {
			return fmt.Errorf("clean completed installation backup: %w", err)
		}
	}
	if err := s.journal.Remove(ctx, transaction.ID); err != nil {
		return fmt.Errorf("remove completed installation transaction: %w", err)
	}
	return nil
}

func (s *ApplyService) acquireApplyOwnership(ctx context.Context) (func() error, error) {
	owner, ok := s.journal.(ApplyOwnership)
	if !ok {
		return func() error { return nil }, nil
	}
	release, err := owner.AcquireApply(ctx)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, ErrApplyBusy) {
			return nil, ErrApplyBusy
		}
		return nil, ErrPreflightFailed
	}
	if release == nil {
		return nil, ErrPreflightFailed
	}
	return release, nil
}

func completedTransactionMatchesMarker(transaction ApplyTransaction, marker installstate.Marker) bool {
	if transaction.Phase != TransactionApplying || transaction.CurrentStep != "lock" ||
		transaction.SelectedUI != marker.SelectedUI || transaction.Mode != marker.Mode ||
		transaction.Marker == nil || *transaction.Marker != marker ||
		transaction.Identity == nil || transaction.Environment == nil ||
		transaction.EnvironmentIntent != transaction.ID {
		return false
	}
	expected := []string{"plan", "database", "redis", "schema", "identity", "environment"}
	if len(transaction.CompletedSteps) != len(expected) {
		return false
	}
	for index := range expected {
		if transaction.CompletedSteps[index] != expected[index] {
			return false
		}
	}
	return true
}

func (s *ApplyService) finalizeEnvironment(ctx context.Context, receipt EnvironmentReceipt) error {
	finalizer, ok := s.environment.(EnvironmentFinalizer)
	if !ok || receipt == (EnvironmentReceipt{}) {
		return nil
	}
	if err := finalizer.Finalize(ctx, receipt); err != nil {
		return ErrApplyFailed
	}
	return nil
}

func (s *ApplyService) finalizeIdentity(ctx context.Context, receipt IdentityReceipt) error {
	finalizer, ok := s.identity.(IdentityFinalizer)
	if !ok || receipt == (IdentityReceipt{}) {
		return nil
	}
	if err := finalizer.Finalize(ctx, receipt); err != nil {
		return ErrApplyFailed
	}
	return nil
}

func (s *ApplyService) recoverInterrupted(ctx context.Context, database DatabaseConnection, transaction *ApplyTransaction) error {
	if transaction == nil || s.journal == nil {
		return nil
	}
	if transaction.Phase == TransactionRetryable && transaction.Identity == nil && transaction.EnvironmentIntent == "" && transaction.Environment == nil && transaction.Marker == nil {
		if err := s.journal.Remove(ctx, transaction.ID); err != nil {
			return ErrApplyBusy
		}
		return nil
	}

	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var rollbackErrors []error
	if transaction.Marker != nil {
		if err := s.markers.Remove(rollbackCtx, *transaction.Marker); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	if transaction.Environment != nil {
		if err := s.environment.Rollback(rollbackCtx, *transaction.Environment); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	} else if transaction.EnvironmentIntent != "" {
		prepared, ok := s.environment.(PreparedEnvironmentInstaller)
		if !ok || prepared.RecoverPrepared(rollbackCtx, transaction.EnvironmentIntent) != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	if transaction.Identity != nil {
		recovery, ok := s.identity.(IdentityRecovery)
		if !ok {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		} else if err := recovery.RecoverRollback(rollbackCtx, database, *transaction.Identity); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	if len(rollbackErrors) != 0 {
		transaction.Phase = TransactionCompensationPending
		transaction.CurrentStep = "failed"
		transaction.UpdatedAt = s.now().UTC()
		_ = s.journal.Update(ctx, *transaction)
		return applyFailure("recovery", ErrApplyRollback)
	}
	if err := s.journal.Remove(ctx, transaction.ID); err != nil {
		return ErrApplyBusy
	}
	return nil
}

func (s *ApplyService) advanceTransaction(ctx context.Context, transaction *ApplyTransaction, next, completed string) error {
	if transaction == nil {
		return nil
	}
	transaction.CompletedSteps = append(transaction.CompletedSteps, completed)
	transaction.CurrentStep = next
	transaction.UpdatedAt = s.now().UTC()
	if err := transaction.Validate(); err != nil {
		return errors.New("validate installation transaction journal")
	}
	if err := s.journal.Update(ctx, *transaction); err != nil {
		return errors.New("update installation transaction journal")
	}
	return nil
}

func (s *ApplyService) persistFailure(_ context.Context, transaction *ApplyTransaction, rollbackErr error) {
	if s == nil || s.journal == nil || transaction == nil {
		return
	}
	transaction.CurrentStep = "failed"
	transaction.UpdatedAt = s.now().UTC()
	if rollbackErr == nil {
		transaction.Phase = TransactionRetryable
		transaction.Identity = nil
		transaction.Environment = nil
		transaction.EnvironmentIntent = ""
		transaction.Marker = nil
	} else {
		transaction.Phase = TransactionCompensationPending
	}
	if transaction.Validate() == nil {
		// The caller context commonly carries the cancellation that caused the
		// apply failure. Durable recovery state must still be committed before
		// returning so a graceful shutdown cannot recreate an already-compensated
		// receipt on restart.
		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.journal.Update(persistCtx, *transaction)
	}
}

// CanRollback reports whether a failed transaction left compensatable
// non-credential side effects. It is intentionally separate from marker
// status: a completed installation is never rolled back through this API.
func (s *ApplyService) CanRollback() bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.pending != nil
}

// Rollback compensates the last failed transaction after the caller has
// explicitly confirmed the operation at the HTTP boundary. Database schema
// migrations remain under the explicit migration runbook; this method only
// restores the receipts owned by the installer transaction.
func (s *ApplyService) Rollback(ctx context.Context) (returnErr error) {
	if s == nil {
		return ErrRollbackUnavailable
	}
	if !s.mutex.TryLock() {
		return ErrApplyBusy
	}
	defer s.mutex.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.pending == nil {
		return ErrRollbackUnavailable
	}
	releaseOwnership, err := s.acquireApplyOwnership(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := releaseOwnership(); releaseErr != nil {
			returnErr = errors.Join(returnErr, ErrRollbackUnavailable)
		}
	}()
	pending := *s.pending
	var rollbackErr error
	if pending.environment == nil && pending.environmentIntent != "" {
		rollbackErr = s.rollbackPreparedEnvironment(pending.environmentIntent, pending.identity)
	} else {
		rollbackErr = s.rollback(ctx, pending.environment, pending.identity)
	}
	if rollbackErr != nil {
		return ErrRollbackUnavailable
	}
	if pending.marker != nil {
		if err := s.rollbackMarker(*pending.marker); err != nil {
			return ErrRollbackUnavailable
		}
	}
	if pending.transaction != nil && s.journal != nil {
		transaction := *pending.transaction
		transaction.Phase = TransactionRetryable
		transaction.CurrentStep = "failed"
		transaction.Identity = nil
		transaction.Environment = nil
		transaction.EnvironmentIntent = ""
		transaction.Marker = nil
		transaction.UpdatedAt = s.now().UTC()
		if err := transaction.Validate(); err != nil {
			return ErrRollbackUnavailable
		}
		if err := s.journal.Update(ctx, transaction); err != nil {
			return ErrRollbackUnavailable
		}
	}
	s.pending = nil
	return nil
}

func reportApplyStep(report func(string), step string) {
	if report != nil {
		report(step)
	}
}

func (s *ApplyService) rollbackMarker(marker installstate.Marker) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.markers.Remove(rollbackCtx, marker)
}

func (s *ApplyService) rollback(_ context.Context, environment *EnvironmentReceipt, identity *IdentityReceipt) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var rollbackErrors []error
	if environment != nil && *environment != (EnvironmentReceipt{}) {
		if err := s.environment.Rollback(rollbackCtx, *environment); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	if identity != nil && *identity != (IdentityReceipt{}) {
		if err := s.identity.Rollback(rollbackCtx, *identity); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	return errors.Join(rollbackErrors...)
}

func applyFailure(stage string, rollbackErr error) error {
	if rollbackErr != nil {
		return withApplyFailureStage(stage, fmt.Errorf("%w at %s: %w", ErrApplyFailed, stage, ErrApplyRollback))
	}
	return withApplyFailureStage(stage, fmt.Errorf("%w at %s", ErrApplyFailed, stage))
}

func receiptPointer[T comparable](receipt T) *T {
	var zero T
	if receipt == zero {
		return nil
	}
	return &receipt
}

func validateApplyRequest(request ApplyRequest) (installstate.Mode, error) {
	mode, err := validatePlanRequest(PlanRequest{Mode: request.Mode})
	if err != nil {
		return "", ErrInvalidApply
	}
	if err := validateDatabaseConnection(request.Database); err != nil {
		return "", ErrInvalidApply
	}
	if err := validateRedisConnection(request.Redis); err != nil {
		return "", ErrInvalidApply
	}
	localeMode := request.LocaleMode
	if localeMode == "" {
		localeMode = string(platformi18n.ModeSingle)
	}
	locale := request.Locale
	if locale == "" {
		locale = platformi18n.LocaleZhCN
	}
	localeConfig := platformi18n.Config{Mode: platformi18n.Mode(localeMode), DefaultLocale: locale, SupportedLocales: []string{platformi18n.LocaleZhCN, platformi18n.LocaleEnUS}}
	if err := localeConfig.Validate(); err != nil {
		return "", ErrInvalidApply
	}
	username := strings.TrimSpace(request.Admin.Username)
	passwordBytes := len([]byte(request.Admin.Password))
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n") || passwordBytes < 12 || passwordBytes > 128 || strings.ContainsAny(request.Admin.Password, "\x00\r\n") {
		return "", ErrInvalidApply
	}
	return mode, nil
}

func appendCompleted(steps []ApplyStep, id string) []ApplyStep {
	return append(steps, ApplyStep{ID: id, Status: StepCompleted})
}

func installationIntegrityDigests(ui installstate.UI, mode installstate.Mode) (string, string) {
	profile := sha256.Sum256([]byte("ui-profile:v1\nselected_ui=" + string(ui) + "\n"))
	manifest := sha256.Sum256([]byte("installation:v1\ninstaller=" + CurrentInstallerVersion + "\nselected_ui=" + string(ui) + "\nmode=" + string(mode) + "\n"))
	return hex.EncodeToString(profile[:]), hex.EncodeToString(manifest[:])
}
