package installer

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
	platformi18n "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/i18n"
)

func TestApplyServiceCompletesInstallationInSafeOrder(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 9)
	markers := &applyMarkerStub{calls: &calls}
	planner := &applyPlanStub{calls: &calls, plan: Plan{
		SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
		CanCleanup: true, CanBuild: true, CanWriteEnv: true,
	}}
	dependencies := &applyDependencyStub{calls: &calls}
	schemas := &applySchemaStub{calls: &calls}
	identity := &applyIdentityStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls}
	completedAt := time.Date(2026, time.August, 21, 13, 30, 0, 0, time.UTC)
	service := NewApplyService(markers, planner, dependencies, schemas, identity, environment, nil, func() time.Time { return completedAt })

	result, err := service.Apply(context.Background(), ApplyRequest{
		Mode: "embedded",
		Database: DatabaseConnection{
			Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "database-secret",
		},
		Redis: RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "redis-secret"},
		Admin: AdminAccount{Username: "admin", Password: "InitialAdmin123"},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantCalls := []string{"marker.load", "plan", "database.check", "redis.check", "schema.up", "identity.initialize", "environment.publish", "marker.create", "environment.finalize", "identity.finalize"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if planner.request.Mode != "embedded" {
		t.Fatalf("plan request = %#v, want mode sourced without UI input", planner.request)
	}
	if result.State != StateInstalled || result.SelectedUI != installstate.UIAntd || result.Mode != installstate.ModeEmbedded || result.InstalledAt != completedAt {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Steps) != 7 || result.Steps[len(result.Steps)-1].ID != "lock" {
		t.Fatalf("steps = %#v", result.Steps)
	}
	if markers.created.SelectedUI != installstate.UIAntd || markers.created.Mode != installstate.ModeEmbedded || markers.created.InstalledAt != completedAt {
		t.Fatalf("created marker = %#v", markers.created)
	}
	wantProfileHash, wantManifestHash := installationIntegrityDigests(installstate.UIAntd, installstate.ModeEmbedded)
	if markers.created.ArtifactHash != wantProfileHash || markers.created.ManifestHash != wantManifestHash {
		t.Fatalf("marker integrity digests = %q/%q, want profile/manifest digests %q/%q", markers.created.ArtifactHash, markers.created.ManifestHash, wantProfileHash, wantManifestHash)
	}
}

func TestApplyServicePreservesIdentityFailureDiagnosticAfterCompensation(t *testing.T) {
	calls := make([]string, 0, 12)
	identity := &applyIdentityStub{
		calls:         &calls,
		initializeErr: navigationDiagnosticJobError{cause: errors.New("password=TOP_SECRET_VALUE")},
	}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{
			SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
			CanCleanup: true, CanBuild: true, CanWriteEnv: true,
		}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		identity,
		&applyEnvironmentStub{calls: &calls},
		nil,
		time.Now,
	)

	_, err := service.Apply(context.Background(), validApplyRequest())
	if !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("Apply() error=%v, want ErrApplyFailed", err)
	}
	var provider FailureDiagnosticProvider
	if !errors.As(err, &provider) {
		t.Fatalf("Apply() error=%T %v, want diagnostic provider", err, err)
	}
	got := provider.InstallationFailureDiagnostic()
	if got.Reason != "navigation_seed_conflict" || got.ResourceKind != "menu" || got.ResourceID != "menu-system-settings" {
		t.Fatalf("diagnostic=%#v", got)
	}
	if indexOf(calls, "identity.rollback") < 0 {
		t.Fatalf("identity failure was not compensated: calls=%#v", calls)
	}
}

func TestApplyServicePersistsCredentialFreeProgressAndRemovesJournalAfterMarker(t *testing.T) {
	calls := make([]string, 0, 20)
	journal := &applyJournalStub{calls: &calls}
	markers := &applyMarkerStub{calls: &calls}
	identity := &applyIdentityStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls}
	service := NewApplyService(
		markers,
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIEle, Mode: installstate.ModeDev, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		identity,
		environment,
		journal,
		func() time.Time { return time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC) },
	)
	request := validApplyRequest()
	request.Mode = "dev"
	request.Database.DSN = "user:journal-database-secret@tcp(db:3306)/app"
	request.Redis.Password = "journal-redis-secret"
	request.Admin.Password = "JournalAdmin123"

	if _, err := service.Apply(context.Background(), request); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(journal.saved) == 0 {
		t.Fatal("journal saved no progress records")
	}
	encoded := journal.encoded(t)
	for _, secret := range []string{request.Database.DSN, request.Redis.Password, request.Admin.Password} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("journal contains credential %q: %s", secret, encoded)
		}
	}
	markerIndex, finalizeIndex := indexOf(calls, "marker.create"), indexOf(calls, "environment.finalize")
	identityFinalizeIndex := indexOf(calls, "identity.finalize")
	cleanupIndex, removeIndex := indexOf(calls, "journal.cleanup"), indexOf(calls, "journal.remove")
	if markerIndex < 0 || finalizeIndex <= markerIndex || identityFinalizeIndex <= finalizeIndex || cleanupIndex <= identityFinalizeIndex || removeIndex <= cleanupIndex {
		t.Fatalf("completion cleanup/removal ran before the final marker: calls=%#v", calls)
	}
	if journal.removedID == "" || journal.removedID != journal.created.ID {
		t.Fatalf("journal removal = %q, created id = %q", journal.removedID, journal.created.ID)
	}
	if identity.preparedReference == "" {
		t.Fatal("identity was initialized without a pre-journaled recovery reference")
	}
	preparedPersisted := false
	for _, saved := range journal.saved {
		if saved.Identity != nil && saved.Identity.Reference == identity.preparedReference {
			preparedPersisted = true
			break
		}
	}
	if !preparedPersisted {
		t.Fatalf("identity reference %q was not persisted before initialization: %#v", identity.preparedReference, journal.saved)
	}
	if environment.preparedReference == "" {
		t.Fatal("environment was published without a pre-journaled recovery reference")
	}
	environmentIntentPersisted := false
	for _, saved := range journal.saved {
		if saved.EnvironmentIntent == environment.preparedReference && saved.Environment == nil {
			environmentIntentPersisted = true
			break
		}
	}
	if !environmentIntentPersisted {
		t.Fatalf("environment intent %q was not persisted before publication: %#v", environment.preparedReference, journal.saved)
	}
}

func TestApplyServiceReportsPersistentOwnershipReleaseFailure(t *testing.T) {
	calls := make([]string, 0, 24)
	journal := &applyJournalStub{calls: &calls, releaseErr: errors.New("persistent apply lease remove failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		journal,
		time.Now,
	)

	if result, err := service.Apply(context.Background(), validApplyRequest()); !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("Apply() result=%#v error=%v, want observable ErrPreflightFailed", result, err)
	}
	if indexOf(calls, "journal.release") < 0 {
		t.Fatalf("ownership release was not attempted: calls=%#v", calls)
	}
}

func TestApplyServiceRecoversInterruptedJournalWithFreshDatabaseCredentialsThenRetries(t *testing.T) {
	calls := make([]string, 0, 30)
	identity := &applyIdentityStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls}
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeDev, DatabaseTarget: mustDatabaseTargetDigest(t, validApplyRequest().Database), Phase: TransactionApplying, CurrentStep: "lock",
		CompletedSteps: []string{"plan", "database", "redis", "schema", "identity", "environment"},
		Identity:       &IdentityReceipt{Reference: "install-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Environment:    &EnvironmentReceipt{Reference: "install-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Digest: strings.Repeat("d", 64)},
		UpdatedAt:      time.Date(2026, time.August, 24, 8, 0, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: interrupted, exists: true}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeDev, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		identity,
		environment,
		journal,
		func() time.Time { return time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC) },
	)
	request := validApplyRequest()
	request.Mode = "dev"
	request.Database.DSN = "fresh-user:fresh-secret@tcp(db:3306)/app"

	if _, err := service.Apply(context.Background(), request); err != nil {
		t.Fatalf("Apply() recovery error = %v; calls=%#v", err, calls)
	}
	wantOrder := []string{"environment.rollback", "identity.recover", "journal.remove", "journal.create", "schema.up", "identity.initialize", "environment.publish", "marker.create"}
	if !orderedCalls(calls, wantOrder) {
		t.Fatalf("calls = %#v, want ordered recovery/retry %#v", calls, wantOrder)
	}
	if identity.recoveryDatabase.DSN != request.Database.DSN || identity.recoveryReceipt.Reference != interrupted.Identity.Reference {
		t.Fatalf("identity recovery inputs = %#v/%#v", identity.recoveryDatabase, identity.recoveryReceipt)
	}
}

func TestApplyServiceAbandonsConcurrentInstallationLoserReceiptAfterRestart(t *testing.T) {
	calls := make([]string, 0, 30)
	request := validApplyRequest()
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeEmbedded, DatabaseTarget: mustDatabaseTargetDigest(t, request.Database),
		Phase: TransactionCompensationPending, CurrentStep: "failed",
		CompletedSteps: []string{"plan", "database", "redis", "schema"},
		Identity:       &IdentityReceipt{Reference: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		UpdatedAt:      time.Date(2026, time.August, 24, 8, 15, 0, 0, time.UTC),
	}
	identity := &applyIdentityStub{calls: &calls, recoveryErr: ErrIdentityNotOwned}
	journal := &applyJournalStub{calls: &calls, loaded: interrupted, exists: true}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		identity,
		&applyEnvironmentStub{calls: &calls},
		journal,
		time.Now,
	)

	if _, err := service.Apply(context.Background(), request); err != nil {
		t.Fatalf("Apply() concurrent loser recovery error=%v calls=%#v", err, calls)
	}
	if !orderedCalls(calls, []string{"identity.recover", "journal.remove", "journal.create", "schema.up", "identity.initialize", "environment.publish", "marker.create"}) {
		t.Fatalf("concurrent loser recovery calls=%#v", calls)
	}
	if len(journal.removedIDs) < 1 || journal.removedIDs[0] != interrupted.ID {
		t.Fatalf("removed journals=%v want loser first=%q", journal.removedIDs, interrupted.ID)
	}
	if journal.created.ID == "" || journal.created.ID == interrupted.ID {
		t.Fatalf("new transaction=%q loser=%q", journal.created.ID, interrupted.ID)
	}
}

func TestApplyServiceDiscardsSideEffectFreeRetryableJournalBeforeDatabaseTargetValidation(t *testing.T) {
	calls := make([]string, 0, 30)
	request := validApplyRequest()
	oldRequest := request
	oldRequest.Database.Host = "old-db.internal"
	oldRequest.Database.Database = "old_app"
	oldRequest.Database.DSN = "old-user:old-password@tcp(old-db.internal:3306)/old_app"
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-cccccccccccccccccccccccccccccccc", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeEmbedded, DatabaseTarget: mustDatabaseTargetDigest(t, oldRequest.Database),
		Phase: TransactionRetryable, CurrentStep: "failed",
		CompletedSteps: []string{"plan", "database", "redis", "schema"},
		UpdatedAt:      time.Date(2026, time.August, 24, 8, 20, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: interrupted, exists: true}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		journal,
		time.Now,
	)

	if _, err := service.Apply(context.Background(), request); err != nil {
		t.Fatalf("Apply() changed-target retry error=%v calls=%#v", err, calls)
	}
	if !orderedCalls(calls, []string{"journal.load", "journal.remove", "plan", "database.check", "redis.check", "journal.create"}) {
		t.Fatalf("changed-target retry calls=%#v", calls)
	}
	if len(journal.removedIDs) < 1 || journal.removedIDs[0] != interrupted.ID {
		t.Fatalf("removed journals=%v want retryable first=%q", journal.removedIDs, interrupted.ID)
	}
	if journal.created.DatabaseTarget == interrupted.DatabaseTarget {
		t.Fatalf("new transaction retained old target=%q", interrupted.DatabaseTarget)
	}
}

func TestApplyServiceRecoversPreparedEnvironmentIntentAfterRestart(t *testing.T) {
	calls := make([]string, 0, 30)
	request := validApplyRequest()
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-dddddddddddddddddddddddddddddddd", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeEmbedded, DatabaseTarget: mustDatabaseTargetDigest(t, request.Database),
		Phase: TransactionApplying, CurrentStep: "environment",
		CompletedSteps:    []string{"plan", "database", "redis", "schema", "identity"},
		Identity:          &IdentityReceipt{Reference: "install-dddddddddddddddddddddddddddddddd"},
		EnvironmentIntent: "install-dddddddddddddddddddddddddddddddd",
		UpdatedAt:         time.Date(2026, time.August, 24, 8, 30, 0, 0, time.UTC),
	}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		&applyJournalStub{calls: &calls, loaded: interrupted, exists: true},
		time.Now,
	)

	if _, err := service.Apply(context.Background(), request); err != nil {
		t.Fatalf("Apply() prepared recovery error = %v; calls=%#v", err, calls)
	}
	if !orderedCalls(calls, []string{"environment.recover", "identity.recover", "journal.remove", "journal.create", "environment.publish", "marker.create"}) {
		t.Fatalf("prepared recovery call order = %#v", calls)
	}
}

func TestDatabaseTargetDigestIgnoresCredentialsButBindsStructuredTarget(t *testing.T) {
	t.Parallel()

	base := DatabaseConnection{
		Driver: "mysql", Mode: "single", Host: "DB.EXAMPLE", Port: 3306, Database: "app",
		Username: "first-user", Password: "first-password",
	}
	first, err := databaseTargetDigest(base)
	if err != nil {
		t.Fatalf("databaseTargetDigest(base) error = %v", err)
	}
	credentialsChanged := base
	credentialsChanged.Username = "second-user"
	credentialsChanged.Password = "second-password"
	second, err := databaseTargetDigest(credentialsChanged)
	if err != nil {
		t.Fatalf("databaseTargetDigest(credentials changed) error = %v", err)
	}
	if first != second {
		t.Fatalf("credential change changed target digest: %q != %q", first, second)
	}
	for name, edit := range map[string]func(*DatabaseConnection){
		"host":     func(connection *DatabaseConnection) { connection.Host = "other.example" },
		"port":     func(connection *DatabaseConnection) { connection.Port = 3307 },
		"database": func(connection *DatabaseConnection) { connection.Database = "other_app" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			edit(&changed)
			digest, err := databaseTargetDigest(changed)
			if err != nil {
				t.Fatal(err)
			}
			if digest == first {
				t.Fatalf("%s change did not change target digest", name)
			}
		})
	}

	dsn := base
	dsn.DSN = "first-user:first-password@tcp(dsn-db.example:3306)/dsn_app?parseTime=true"
	firstDSN, err := databaseTargetDigest(dsn)
	if err != nil {
		t.Fatal(err)
	}
	dsn.DSN = "second-user:second-password@tcp(dsn-db.example:3306)/dsn_app?parseTime=true"
	secondDSN, err := databaseTargetDigest(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if firstDSN != secondDSN {
		t.Fatalf("DSN credential change changed target digest: %q != %q", firstDSN, secondDSN)
	}
	dsn.DSN = "second-user:second-password@tcp(other-db.example:3306)/dsn_app?parseTime=true"
	otherDSN, err := databaseTargetDigest(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if otherDSN == firstDSN {
		t.Fatal("effective DSN endpoint change did not change target digest")
	}
}

func TestApplyServiceRejectsDifferentRecoveryDatabaseBeforeCompensation(t *testing.T) {
	calls := make([]string, 0, 10)
	original := validApplyRequest().Database
	originalTarget, err := databaseTargetDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-cccccccccccccccccccccccccccccccc", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeEmbedded, Phase: TransactionCompensationPending, CurrentStep: "failed",
		CompletedSteps: []string{"plan", "database", "redis", "schema", "identity", "environment"},
		DatabaseTarget: originalTarget,
		Identity:       &IdentityReceipt{Reference: "install-cccccccccccccccccccccccccccccccc"},
		Environment:    &EnvironmentReceipt{Reference: "install-cccccccccccccccccccccccccccccccc", Digest: strings.Repeat("d", 64)},
		UpdatedAt:      time.Date(2026, time.August, 24, 8, 0, 0, 0, time.UTC),
	}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		&applyJournalStub{calls: &calls, loaded: interrupted, exists: true},
		time.Now,
	)
	request := validApplyRequest()
	request.Database.Host = "different-db.example"
	request.Database.Port = 3306
	request.Database.Database = "app"
	request.Database.DSN = ""

	if _, err := service.Apply(context.Background(), request); !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("Apply() error = %v, want ErrPreflightFailed", err)
	}
	for _, forbidden := range []string{"environment.rollback", "environment.recover", "identity.recover", "identity.rollback"} {
		if indexOf(calls, forbidden) >= 0 {
			t.Fatalf("different target triggered compensation %q: calls=%#v", forbidden, calls)
		}
	}
}

func TestApplyServiceMarksFailedSideEffectFreeTransactionRetryable(t *testing.T) {
	calls := make([]string, 0, 16)
	journal := &applyJournalStub{calls: &calls}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls, err: errors.New("schema fixture failure")},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		journal,
		time.Now,
	)

	if _, err := service.Apply(context.Background(), validApplyRequest()); err == nil {
		t.Fatal("Apply() error = nil")
	}
	if len(journal.saved) == 0 {
		t.Fatal("failed apply did not update journal")
	}
	last := journal.saved[len(journal.saved)-1]
	if last.Phase != TransactionRetryable || last.CurrentStep != "failed" || last.Identity != nil || last.Environment != nil || last.Marker != nil {
		t.Fatalf("failed transaction = %#v, want credential-free retryable state", last)
	}
}

func TestApplyServiceReconcileCompletedRemovesOnlyMatchingServerJournal(t *testing.T) {
	calls := make([]string, 0, 8)
	marker := validMarker(installstate.UIAntd)
	marker.Mode = installstate.ModeDev
	transaction := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner,
		ID: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SelectedUI: marker.SelectedUI, Mode: marker.Mode,
		DatabaseTarget: strings.Repeat("a", 64), Phase: TransactionApplying, CurrentStep: "lock",
		CompletedSteps:    []string{"plan", "database", "redis", "schema", "identity", "environment"},
		Identity:          &IdentityReceipt{Reference: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		EnvironmentIntent: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Environment:       &EnvironmentReceipt{Reference: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Digest: strings.Repeat("c", 64)},
		Marker:            &marker, UpdatedAt: time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: transaction, exists: true}
	environment := &applyEnvironmentStub{calls: &calls}
	identity := &applyIdentityStub{calls: &calls}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, loaded: marker, installed: true}, nil, nil, nil, identity, environment, journal, time.Now,
	)

	if err := service.ReconcileCompleted(context.Background()); err != nil {
		t.Fatalf("ReconcileCompleted() error = %v", err)
	}
	if journal.removedID != transaction.ID {
		t.Fatalf("removed journal id = %q, want %q", journal.removedID, transaction.ID)
	}
	if !orderedCalls(calls, []string{"journal.load", "environment.finalize", "identity.finalize", "journal.remove"}) {
		t.Fatalf("completed recovery order = %#v", calls)
	}

	journal.removedID = ""
	journal.loaded.SelectedUI = installstate.UIEle
	journal.loaded.Marker = nil
	if err := service.ReconcileCompleted(context.Background()); err != nil {
		t.Fatalf("ReconcileCompleted(mismatch) error = %v", err)
	}
	if journal.removedID != "" {
		t.Fatalf("mismatched journal removed: %q", journal.removedID)
	}
}

func TestApplyServiceKeepsCompletedJournalWhenEnvironmentFinalizeNeedsRetry(t *testing.T) {
	calls := make([]string, 0, 24)
	journal := &applyJournalStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls, finalizeErr: errors.New("temporary finalize failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeDev, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		environment,
		journal,
		time.Now,
	)
	request := validApplyRequest()
	request.Mode = "dev"

	result, err := service.Apply(context.Background(), request)
	if err != nil || result.State != StateInstalled {
		t.Fatalf("committed Apply() result=%#v error=%v", result, err)
	}
	if journal.removedID != "" {
		t.Fatalf("journal removed before environment finalize retry: %q", journal.removedID)
	}
	if !orderedCalls(calls, []string{"marker.create", "environment.finalize", "journal.cleanup"}) {
		t.Fatalf("completion calls = %#v", calls)
	}
}

func TestApplyServiceKeepsCompletedJournalWhenBackupCleanupNeedsRetry(t *testing.T) {
	calls := make([]string, 0, 24)
	journal := &applyJournalStub{calls: &calls, cleanupErr: errors.New("temporary backup cleanup failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeDev, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		journal,
		time.Now,
	)
	request := validApplyRequest()
	request.Mode = "dev"

	result, err := service.Apply(context.Background(), request)
	if err != nil || result.State != StateInstalled {
		t.Fatalf("committed Apply() result=%#v error=%v", result, err)
	}
	if journal.removedID != "" {
		t.Fatalf("journal removed before backup cleanup retry: %q", journal.removedID)
	}
	if !orderedCalls(calls, []string{"marker.create", "environment.finalize", "identity.finalize", "journal.cleanup"}) {
		t.Fatalf("completion calls = %#v", calls)
	}
}

func TestApplyServiceReconcileCompletedPreservesJournalWhenFinalizeFails(t *testing.T) {
	calls := make([]string, 0, 12)
	marker := validMarker(installstate.UIEle)
	marker.Mode = installstate.ModeDev
	id := "install-cccccccccccccccccccccccccccccccc"
	transaction := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner, ID: id,
		SelectedUI: marker.SelectedUI, Mode: marker.Mode, DatabaseTarget: strings.Repeat("a", 64),
		Phase: TransactionApplying, CurrentStep: "lock",
		CompletedSteps:    []string{"plan", "database", "redis", "schema", "identity", "environment"},
		Identity:          &IdentityReceipt{Reference: id},
		EnvironmentIntent: id,
		Environment:       &EnvironmentReceipt{Reference: id, Digest: strings.Repeat("d", 64)},
		Marker:            &marker,
		UpdatedAt:         time.Date(2026, time.August, 24, 10, 0, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: transaction, exists: true}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, loaded: marker, installed: true}, nil, nil, nil,
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls, finalizeErr: errors.New("temporary finalize failure")},
		journal, time.Now,
	)

	if err := service.ReconcileCompleted(context.Background()); err == nil {
		t.Fatal("ReconcileCompleted() error = nil")
	}
	if journal.removedID != "" {
		t.Fatalf("journal removed after failed finalize: %q", journal.removedID)
	}
	if indexOf(calls, "journal.cleanup") >= 0 || indexOf(calls, "identity.finalize") >= 0 {
		t.Fatalf("later completion steps ran after finalize failure: %#v", calls)
	}
}

func TestApplyServiceReconcileCompletedPreservesJournalWhenBackupCleanupFails(t *testing.T) {
	calls := make([]string, 0, 12)
	marker := validMarker(installstate.UIEle)
	marker.Mode = installstate.ModeDev
	id := "install-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	transaction := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner, ID: id,
		SelectedUI: marker.SelectedUI, Mode: marker.Mode, DatabaseTarget: strings.Repeat("a", 64),
		Phase: TransactionApplying, CurrentStep: "lock",
		CompletedSteps:    []string{"plan", "database", "redis", "schema", "identity", "environment"},
		Identity:          &IdentityReceipt{Reference: id},
		EnvironmentIntent: id,
		Environment:       &EnvironmentReceipt{Reference: id, Digest: strings.Repeat("d", 64)},
		Marker:            &marker,
		UpdatedAt:         time.Date(2026, time.August, 24, 10, 15, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{
		calls: &calls, loaded: transaction, exists: true,
		cleanupErr: errors.New("temporary backup cleanup failure"),
	}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, loaded: marker, installed: true}, nil, nil, nil,
		&applyIdentityStub{calls: &calls}, &applyEnvironmentStub{calls: &calls}, journal, time.Now,
	)

	if err := service.ReconcileCompleted(context.Background()); err == nil {
		t.Fatal("ReconcileCompleted() error = nil")
	}
	if journal.removedID != "" {
		t.Fatalf("journal removed after failed backup cleanup: %q", journal.removedID)
	}
	if !orderedCalls(calls, []string{"environment.finalize", "identity.finalize", "journal.cleanup"}) {
		t.Fatalf("completion calls = %#v", calls)
	}
}

func TestApplyServiceReconcileCompletedPreservesPreMarkerTransaction(t *testing.T) {
	calls := make([]string, 0, 8)
	installed := validMarker(installstate.UIAntd)
	installed.Mode = installstate.ModeDev
	preMarker := installed
	preMarker.InstalledAt = installed.InstalledAt.Add(-time.Minute)
	id := "install-dddddddddddddddddddddddddddddddd"
	transaction := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner, ID: id,
		SelectedUI: installed.SelectedUI, Mode: installed.Mode, DatabaseTarget: strings.Repeat("a", 64),
		Phase: TransactionApplying, CurrentStep: "lock",
		CompletedSteps:    []string{"plan", "database", "redis", "schema", "identity", "environment"},
		Identity:          &IdentityReceipt{Reference: id},
		EnvironmentIntent: id,
		Environment:       &EnvironmentReceipt{Reference: id, Digest: strings.Repeat("d", 64)},
		Marker:            &preMarker,
		UpdatedAt:         time.Date(2026, time.August, 24, 10, 30, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: transaction, exists: true}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, loaded: installed, installed: true}, nil, nil, nil,
		&applyIdentityStub{calls: &calls}, &applyEnvironmentStub{calls: &calls}, journal, time.Now,
	)

	if err := service.ReconcileCompleted(context.Background()); err != nil {
		t.Fatalf("ReconcileCompleted() error = %v", err)
	}
	if journal.removedID != "" || indexOf(calls, "environment.finalize") >= 0 || indexOf(calls, "identity.finalize") >= 0 {
		t.Fatalf("pre-marker transaction was treated as committed: calls=%#v removed=%q", calls, journal.removedID)
	}
}

func TestApplyServiceReconcileCompletedPristineDoesNotCreateOwnershipState(t *testing.T) {
	calls := make([]string, 0, 2)
	journal := &applyJournalStub{calls: &calls, ownershipErr: ErrApplyBusy}
	service := NewApplyService(&applyMarkerStub{calls: &calls, installed: false}, nil, nil, nil, nil, nil, journal, time.Now)

	if err := service.ReconcileCompleted(context.Background()); err != nil {
		t.Fatalf("ReconcileCompleted() pristine error = %v", err)
	}
	if indexOf(calls, "journal.acquire") >= 0 {
		t.Fatalf("pristine reconciliation created process ownership state: %#v", calls)
	}
}

func TestApplyServiceReconcileCompletedReportsPersistentOwnershipReleaseFailure(t *testing.T) {
	calls := make([]string, 0, 8)
	marker := validMarker(installstate.UIAntd)
	journal := &applyJournalStub{calls: &calls, releaseErr: errors.New("persistent apply lease remove failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, loaded: marker, installed: true}, nil, nil, nil, nil, nil, journal, time.Now,
	)

	if err := service.ReconcileCompleted(context.Background()); !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("ReconcileCompleted() error = %v, want ErrPreflightFailed", err)
	}
	if indexOf(calls, "journal.release") < 0 {
		t.Fatalf("ownership release was not attempted: calls=%#v", calls)
	}
}

func TestValidateApplyRequestAcceptsOnlyBundledLocalePolicy(t *testing.T) {
	valid := validApplyRequest()
	valid.Locale = platformi18n.LocaleEnUS
	valid.LocaleMode = string(platformi18n.ModeMulti)
	if _, err := validateApplyRequest(valid); err != nil {
		t.Fatalf("validate bundled locale policy error = %v", err)
	}
	for name, edit := range map[string]func(*ApplyRequest){
		"unknown locale": func(request *ApplyRequest) { request.Locale = "fr-FR" },
		"unknown mode":   func(request *ApplyRequest) { request.LocaleMode = "fallback" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			edit(&candidate)
			if _, err := validateApplyRequest(candidate); !errors.Is(err, ErrInvalidApply) {
				t.Fatalf("validate error = %v, want ErrInvalidApply", err)
			}
		})
	}
}

func TestValidateApplyRequestEnforcesInitialAdminPasswordPolicy(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{name: "five characters", password: "Ab123", valid: false},
		{name: "six mixed characters", password: "Abc123", valid: true},
		{name: "letters only", password: "Abcdefghijkl", valid: false},
		{name: "digits only", password: "123456789012", valid: false},
		{name: "symbol", password: "Abcdefghij1!", valid: false},
		{name: "non ASCII", password: "密码Abc123", valid: false},
		{name: "whitespace", password: "Abcdef 12345", valid: false},
		{name: "72 characters", password: strings.Repeat("a", 71) + "1", valid: true},
		{name: "73 characters", password: strings.Repeat("a", 72) + "1", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validApplyRequest()
			request.Admin.Password = test.password
			_, err := validateApplyRequest(request)
			if test.valid && err != nil {
				t.Fatalf("validateApplyRequest() error = %v, want nil", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidApply) {
				t.Fatalf("validateApplyRequest() error = %v, want ErrInvalidApply", err)
			}
		})
	}
}

func TestApplyServiceRollsBackCompletedSideEffectsInReverseOrder(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 12)
	markers := &applyMarkerStub{calls: &calls, createErr: errors.New("marker fixture failure")}
	planner := &applyPlanStub{calls: &calls, plan: Plan{
		SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
		CanCleanup: true, CanBuild: true, CanWriteEnv: true,
	}}
	service := NewApplyService(
		markers,
		planner,
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		nil,
		func() time.Time { return time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC) },
	)

	_, err := service.Apply(context.Background(), validApplyRequest())
	if !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("Apply() error = %v, want ErrApplyFailed", err)
	}
	wantSuffix := []string{"marker.create", "marker.rollback", "environment.rollback", "identity.rollback"}
	if len(calls) < len(wantSuffix) || !reflect.DeepEqual(calls[len(calls)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("call suffix = %#v, want %#v; all=%#v", calls, wantSuffix, calls)
	}
}

func TestApplyServiceKeepsFailedCompensationForExplicitRollback(t *testing.T) {
	t.Parallel()

	environment := &applyEnvironmentStub{calls: new([]string), rollbackErr: errors.New("temporary environment rollback failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: new([]string), createErr: errors.New("marker fixture failure")},
		&applyPlanStub{calls: new([]string), plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: new([]string)},
		&applySchemaStub{calls: new([]string)},
		&applyIdentityStub{calls: new([]string)},
		environment,
		nil,
		time.Now,
	)
	if _, err := service.Apply(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyRollback) {
		t.Fatalf("Apply() error = %v, want rollback sentinel", err)
	}
	if !service.CanRollback() {
		t.Fatal("CanRollback() = false, want pending compensation")
	}
	environment.rollbackErr = nil
	if err := service.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if service.CanRollback() {
		t.Fatal("CanRollback() = true after explicit rollback")
	}
}

func TestExplicitRollbackClearsDurableCompensationReceipts(t *testing.T) {
	calls := make([]string, 0, 30)
	journal := &applyJournalStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls, rollbackErr: errors.New("temporary environment rollback failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls, createErr: errors.New("marker fixture failure")},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		environment,
		journal,
		time.Now,
	)
	if _, err := service.Apply(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyRollback) {
		t.Fatalf("Apply() error = %v, want ErrApplyRollback", err)
	}
	failed := journal.saved[len(journal.saved)-1]
	if failed.Phase != TransactionCompensationPending || failed.Environment == nil {
		t.Fatalf("failed durable transaction = %#v", failed)
	}

	environment.rollbackErr = nil
	if err := service.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	rolledBack := journal.saved[len(journal.saved)-1]
	if rolledBack.Phase != TransactionRetryable || rolledBack.Identity != nil || rolledBack.Environment != nil || rolledBack.Marker != nil {
		t.Fatalf("durable transaction after explicit rollback = %#v", rolledBack)
	}
}

func TestExplicitRollbackDoesNotRaceAnActiveInstallerProcess(t *testing.T) {
	calls := make([]string, 0, 4)
	journal := &applyJournalStub{calls: &calls, ownershipErr: ErrApplyBusy}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls}, nil, nil, nil,
		&applyIdentityStub{calls: &calls}, &applyEnvironmentStub{calls: &calls}, journal, time.Now,
	)
	service.pending = &pendingRollback{
		environment: &EnvironmentReceipt{Reference: "environment-1", Digest: strings.Repeat("c", 64)},
	}

	if err := service.Rollback(context.Background()); !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("Rollback() error = %v, want ErrApplyBusy", err)
	}
	if indexOf(calls, "environment.rollback") >= 0 || service.pending == nil {
		t.Fatalf("rollback touched pending side effects while ownership was busy: calls=%#v pending=%#v", calls, service.pending)
	}
}

func TestApplyServicePersistsRetryableStateAfterCallerCancellation(t *testing.T) {
	calls := make([]string, 0, 30)
	ctx, cancel := context.WithCancel(context.Background())
	journal := &applyJournalStub{calls: &calls, respectContext: true}
	environment := &applyEnvironmentStub{calls: &calls, publishHook: cancel}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls},
		environment,
		journal,
		time.Now,
	)

	if _, err := service.Apply(ctx, validApplyRequest()); err == nil {
		t.Fatal("Apply() error = nil after caller cancellation")
	}
	if len(journal.saved) == 0 {
		t.Fatal("cancelled apply persisted no recovery state")
	}
	last := journal.saved[len(journal.saved)-1]
	if last.Phase != TransactionRetryable || last.CurrentStep != "failed" || last.Identity != nil || last.EnvironmentIntent != "" || last.Environment != nil || last.Marker != nil {
		t.Fatalf("cancelled apply durable state = %#v, want side-effect-free retryable", last)
	}
}

func TestApplyServiceDoesNotRecoverJournalOwnedByActiveInstallerProcess(t *testing.T) {
	calls := make([]string, 0, 8)
	id := "install-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	interrupted := ApplyTransaction{
		Schema: ApplyTransactionSchema, Owner: ApplyTransactionOwner, ID: id,
		SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
		DatabaseTarget: mustDatabaseTargetDigest(t, validApplyRequest().Database),
		Phase:          TransactionApplying, CurrentStep: "environment",
		CompletedSteps: []string{"plan", "database", "redis", "schema", "identity"},
		Identity:       &IdentityReceipt{Reference: id},
		UpdatedAt:      time.Date(2026, time.August, 24, 11, 0, 0, 0, time.UTC),
	}
	journal := &applyJournalStub{calls: &calls, loaded: interrupted, exists: true, ownershipErr: ErrApplyBusy}
	service := NewApplyService(
		&applyMarkerStub{calls: &calls},
		&applyPlanStub{calls: &calls, plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: &calls}, &applySchemaStub{calls: &calls},
		&applyIdentityStub{calls: &calls}, &applyEnvironmentStub{calls: &calls}, journal, time.Now,
	)

	if _, err := service.Apply(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("Apply() error = %v, want ErrApplyBusy", err)
	}
	for _, forbidden := range []string{"marker.load", "journal.load", "environment.rollback", "identity.recover", "schema.up"} {
		if indexOf(calls, forbidden) >= 0 {
			t.Fatalf("active installer transaction was touched: calls=%#v", calls)
		}
	}
}

func validApplyRequest() ApplyRequest {
	return ApplyRequest{
		Mode:     "embedded",
		Database: DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"},
		Redis:    RedisConnection{Mode: "single", Addr: "redis:6379"},
		Admin:    AdminAccount{Username: "admin", Password: "InitialAdmin123"},
	}
}

func mustDatabaseTargetDigest(t *testing.T, connection DatabaseConnection) string {
	t.Helper()
	digest, err := databaseTargetDigest(connection)
	if err != nil {
		t.Fatalf("databaseTargetDigest() error = %v", err)
	}
	return digest
}

type applyMarkerStub struct {
	calls     *[]string
	loaded    installstate.Marker
	installed bool
	created   installstate.Marker
	createErr error
}

func (s *applyMarkerStub) Load(context.Context) (installstate.Marker, bool, error) {
	*s.calls = append(*s.calls, "marker.load")
	return s.loaded, s.installed, nil
}

func (s *applyMarkerStub) Create(_ context.Context, marker installstate.Marker) error {
	*s.calls = append(*s.calls, "marker.create")
	s.created = marker
	return s.createErr
}

func (s *applyMarkerStub) Remove(context.Context, installstate.Marker) error {
	*s.calls = append(*s.calls, "marker.rollback")
	return nil
}

type applyPlanStub struct {
	calls   *[]string
	plan    Plan
	request PlanRequest
}

func (s *applyPlanStub) Plan(_ context.Context, request PlanRequest) (Plan, error) {
	*s.calls = append(*s.calls, "plan")
	s.request = request
	return s.plan, nil
}

type applyDependencyStub struct{ calls *[]string }

func (s *applyDependencyStub) CheckDatabase(context.Context, DatabaseConnection) (DependencyCheck, error) {
	*s.calls = append(*s.calls, "database.check")
	return DependencyCheck{Kind: "database", Driver: "mysql", Mode: "single", OK: true, Reason: "reachable"}, nil
}

func (s *applyDependencyStub) CheckRedis(context.Context, RedisConnection) (DependencyCheck, error) {
	*s.calls = append(*s.calls, "redis.check")
	return DependencyCheck{Kind: "redis", Mode: "single", OK: true, Reason: "reachable"}, nil
}

type applySchemaStub struct {
	calls *[]string
	err   error
}

func (s *applySchemaStub) Up(context.Context, DatabaseConnection) (SchemaReceipt, error) {
	*s.calls = append(*s.calls, "schema.up")
	return SchemaReceipt{Version: 4}, s.err
}

type applyIdentityStub struct {
	calls             *[]string
	recoveryDatabase  DatabaseConnection
	recoveryReceipt   IdentityReceipt
	preparedReference string
	recoveryErr       error
	initializeErr     error
}

func (s *applyIdentityStub) Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error) {
	*s.calls = append(*s.calls, "identity.initialize")
	return IdentityReceipt{Reference: "installation-1"}, s.initializeErr
}

func (s *applyIdentityStub) InitializeWithReference(_ context.Context, _ DatabaseConnection, _ AdminAccount, reference string) (IdentityReceipt, error) {
	*s.calls = append(*s.calls, "identity.initialize")
	s.preparedReference = reference
	return IdentityReceipt{Reference: reference}, s.initializeErr
}

func (s *applyIdentityStub) Rollback(context.Context, IdentityReceipt) error {
	*s.calls = append(*s.calls, "identity.rollback")
	return nil
}

func (s *applyIdentityStub) RecoverRollback(_ context.Context, database DatabaseConnection, receipt IdentityReceipt) error {
	*s.calls = append(*s.calls, "identity.recover")
	s.recoveryDatabase = database
	s.recoveryReceipt = receipt
	return s.recoveryErr
}

func (s *applyIdentityStub) Finalize(context.Context, IdentityReceipt) error {
	*s.calls = append(*s.calls, "identity.finalize")
	return nil
}

type applyEnvironmentStub struct {
	calls             *[]string
	rollbackErr       error
	finalizeErr       error
	preparedReference string
	publishHook       func()
}

type applyJournalStub struct {
	calls          *[]string
	loaded         ApplyTransaction
	exists         bool
	created        ApplyTransaction
	saved          []ApplyTransaction
	removedID      string
	removedIDs     []string
	respectContext bool
	ownershipErr   error
	releaseErr     error
	cleanupErr     error
}

func (s *applyJournalStub) AcquireApply(context.Context) (func() error, error) {
	*s.calls = append(*s.calls, "journal.acquire")
	if s.ownershipErr != nil {
		return nil, s.ownershipErr
	}
	return func() error {
		*s.calls = append(*s.calls, "journal.release")
		return s.releaseErr
	}, nil
}

func (s *applyJournalStub) Load(context.Context) (ApplyTransaction, bool, error) {
	*s.calls = append(*s.calls, "journal.load")
	return s.loaded, s.exists, nil
}

func (s *applyJournalStub) Create(_ context.Context, transaction ApplyTransaction) error {
	*s.calls = append(*s.calls, "journal.create")
	s.created = transaction
	return nil
}

func (s *applyJournalStub) Update(ctx context.Context, transaction ApplyTransaction) error {
	*s.calls = append(*s.calls, "journal.update")
	if s.respectContext && ctx.Err() != nil {
		return ctx.Err()
	}
	s.saved = append(s.saved, transaction)
	return nil
}

func (s *applyJournalStub) Remove(_ context.Context, id string) error {
	*s.calls = append(*s.calls, "journal.remove")
	s.removedID = id
	s.removedIDs = append(s.removedIDs, id)
	return nil
}

func (s *applyJournalStub) CleanupCompleted(_ context.Context, _ installstate.Marker) error {
	*s.calls = append(*s.calls, "journal.cleanup")
	return s.cleanupErr
}

func (s *applyJournalStub) encoded(t *testing.T) string {
	t.Helper()
	all := append([]ApplyTransaction{s.created}, s.saved...)
	encoded, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func orderedCalls(values, targets []string) bool {
	next := 0
	for _, value := range values {
		if next < len(targets) && value == targets[next] {
			next++
		}
	}
	return next == len(targets)
}

func (s *applyEnvironmentStub) Publish(context.Context, ApplyRequest, Plan) (EnvironmentReceipt, error) {
	*s.calls = append(*s.calls, "environment.publish")
	return EnvironmentReceipt{Digest: strings.Repeat("c", 64), Reference: "environment-1"}, nil
}

func (s *applyEnvironmentStub) PublishWithReference(_ context.Context, _ ApplyRequest, _ Plan, reference string) (EnvironmentReceipt, error) {
	*s.calls = append(*s.calls, "environment.publish")
	s.preparedReference = reference
	if s.publishHook != nil {
		s.publishHook()
	}
	return EnvironmentReceipt{Digest: strings.Repeat("c", 64), Reference: reference}, nil
}

func (s *applyEnvironmentStub) RecoverPrepared(_ context.Context, _ string) error {
	*s.calls = append(*s.calls, "environment.recover")
	return s.rollbackErr
}

func (s *applyEnvironmentStub) Rollback(context.Context, EnvironmentReceipt) error {
	*s.calls = append(*s.calls, "environment.rollback")
	return s.rollbackErr
}

func (s *applyEnvironmentStub) Finalize(context.Context, EnvironmentReceipt) error {
	*s.calls = append(*s.calls, "environment.finalize")
	return s.finalizeErr
}
