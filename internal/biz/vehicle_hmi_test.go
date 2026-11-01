package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeVehicleHMIRepo struct {
	command                   *VehicleCommand
	commands                  []*VehicleCommand
	activeCommands            []*VehicleCommand
	publishErr                error
	updateErr                 error
	commandAfterUpdateError   *VehicleCommand
	updateCalls               int
	publishCalls              int
	pendingCommandCalls       int
	activeCommandCalls        int
	boardingVerificationCalls int
	lastBoardingVerification  *BoardingVerification
}

func (r *fakeVehicleHMIRepo) ListPendingCommands(context.Context, int64, string) ([]*VehicleCommand, error) {
	r.pendingCommandCalls++
	if r.commands != nil {
		return r.commands, nil
	}
	return []*VehicleCommand{r.command}, nil
}
func (r *fakeVehicleHMIRepo) ListActiveCommands(context.Context, int64, string) ([]*VehicleCommand, error) {
	r.activeCommandCalls++
	return r.activeCommands, nil
}
func (r *fakeVehicleHMIRepo) GetCommand(context.Context, string) (*VehicleCommand, error) {
	return r.command, nil
}
func (r *fakeVehicleHMIRepo) UpdateCommandStatus(_ context.Context, _ string, _, target CommandStatus, result string, changedAt time.Time) (*VehicleCommand, error) {
	r.updateCalls++
	if r.updateErr != nil {
		if r.commandAfterUpdateError != nil {
			r.command = r.commandAfterUpdateError
		}
		return nil, r.updateErr
	}
	r.command.Status = target
	r.command.ResultMessage = result
	if target == CommandStatusAcknowledged {
		r.command.AcknowledgedAt = &changedAt
	} else {
		r.command.FinishedAt = &changedAt
	}
	return r.command, nil
}
func (r *fakeVehicleHMIRepo) CreateBoardingVerification(_ context.Context, item *BoardingVerification) error {
	r.boardingVerificationCalls++
	r.lastBoardingVerification = item
	return nil
}
func (*fakeVehicleHMIRepo) CreateTelemetry(context.Context, *TelemetryReport) error { return nil }
func (*fakeVehicleHMIRepo) CreateFault(context.Context, *FaultReport) error         { return nil }
func (*fakeVehicleHMIRepo) GetHMIView(context.Context, int64) (*HMIView, error) {
	return &HMIView{VehicleID: 301}, nil
}
func (*fakeVehicleHMIRepo) CreateAssistanceRequest(context.Context, *AssistanceRequest) error {
	return nil
}
func (*fakeVehicleHMIRepo) CreateMediaRecord(context.Context, *MediaRecord) error { return nil }
func (r *fakeVehicleHMIRepo) Publish(context.Context, string, string, []byte) error {
	r.publishCalls++
	return r.publishErr
}

func TestAcknowledgeCommand(t *testing.T) {
	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusSent}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	command, err := uc.AcknowledgeCommand(t.Context(), "cmd-1")
	if err != nil {
		t.Fatalf("AcknowledgeCommand() error = %v", err)
	}
	if command.Status != CommandStatusAcknowledged || repo.updateCalls != 1 {
		t.Fatalf("unexpected command status %d, update calls %d", command.Status, repo.updateCalls)
	}
}

func TestDuplicateAcknowledgementIsIdempotent(t *testing.T) {
	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusAcknowledged}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	command, err := uc.AcknowledgeCommand(t.Context(), "cmd-1")
	if err != nil {
		t.Fatalf("AcknowledgeCommand() error = %v", err)
	}
	if command.Status != CommandStatusAcknowledged || repo.updateCalls != 0 {
		t.Fatalf("duplicate acknowledgement changed state")
	}
}

func TestAcknowledgeCommandRecoversConcurrentDuplicateUpdate(t *testing.T) {
	racingErr := errors.New("conditional update matched no rows")
	repo := &fakeVehicleHMIRepo{
		command:                 &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusSent},
		updateErr:               racingErr,
		commandAfterUpdateError: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusAcknowledged},
	}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	command, err := uc.AcknowledgeCommand(t.Context(), "cmd-1")
	if err != nil {
		t.Fatalf("AcknowledgeCommand() error = %v", err)
	}
	if command.Status != CommandStatusAcknowledged || repo.updateCalls != 1 {
		t.Fatalf("concurrent acknowledgement was not recovered")
	}
}

func TestCompleteCommandRejectsOutOfOrderResult(t *testing.T) {
	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusSent}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	_, err := uc.CompleteCommand(t.Context(), "cmd-1", true, "ok")
	if !errors.Is(err, ErrInvalidCommandTransition) {
		t.Fatalf("CompleteCommand() error = %v, want ErrInvalidCommandTransition", err)
	}
}

func TestCompleteCommandReturnsPublishError(t *testing.T) {
	publishErr := errors.New("publish failed")
	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusAcknowledged}, publishErr: publishErr}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	_, err := uc.CompleteCommand(t.Context(), "cmd-1", true, "ok")
	if !errors.Is(err, publishErr) {
		t.Fatalf("CompleteCommand() error = %v, want publish error", err)
	}
}

func TestDuplicateCommandResultRetriesEventPublication(t *testing.T) {
	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1", Status: CommandStatusSucceeded}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	command, err := uc.CompleteCommand(t.Context(), "cmd-1", true, "ok")
	if err != nil {
		t.Fatalf("CompleteCommand() error = %v", err)
	}
	if command.Status != CommandStatusSucceeded || repo.updateCalls != 0 || repo.publishCalls != 1 {
		t.Fatalf("duplicate result did not retry event publication")
	}
}

func TestHMIViewReadsCommandsWithoutMarkingThemSent(t *testing.T) {
	repo := &fakeVehicleHMIRepo{
		activeCommands: []*VehicleCommand{{CommandNo: "cmd-pending", Status: CommandStatusPending}},
	}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	view, err := uc.HMIView(t.Context(), 1001)
	if err != nil {
		t.Fatalf("HMIView() error = %v", err)
	}
	if repo.pendingCommandCalls != 0 || repo.activeCommandCalls != 1 {
		t.Fatalf("HMIView() pending calls = %d, active calls = %d", repo.pendingCommandCalls, repo.activeCommandCalls)
	}
	if len(view.PendingCommands) != 1 || view.PendingCommands[0].CommandNo != "cmd-pending" {
		t.Fatalf("HMIView() commands = %#v", view.PendingCommands)
	}
}

func TestVerifyBoardingRejectsMissingCredentialHash(t *testing.T) {
	uc := NewVehicleHMIUsecase(&fakeVehicleHMIRepo{}, log.DefaultLogger)
	err := uc.VerifyBoarding(t.Context(), &BoardingVerification{OrderID: 1, VerificationType: 1})
	if !errors.Is(err, ErrInvalidVehicleInput) {
		t.Fatalf("VerifyBoarding() error = %v, want ErrInvalidVehicleInput", err)
	}
}

func TestVerifyBoardingPersistsFailedAttempt(t *testing.T) {
	repo := &fakeVehicleHMIRepo{}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	item := &BoardingVerification{
		OrderID:          1001,
		VerificationType: 3,
		CredentialHash:   "sha256:test-credential",
		Succeeded:        false,
		FailureReason:    "verification failed",
	}
	if err := uc.VerifyBoarding(t.Context(), item); err != nil {
		t.Fatalf("VerifyBoarding() error = %v", err)
	}
	if repo.boardingVerificationCalls != 1 || repo.lastBoardingVerification.Succeeded {
		t.Fatalf("failed verification was not persisted correctly")
	}
}

func TestPendingCommandsKeepsSentCommandForOfflineReconnect(t *testing.T) {
	repo := &fakeVehicleHMIRepo{commands: []*VehicleCommand{
		{CommandNo: "cmd-sent", Status: CommandStatusSent, Priority: 5},
	}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	commands, err := uc.PendingCommands(t.Context(), 301, "")
	if err != nil {
		t.Fatalf("PendingCommands() error = %v", err)
	}
	if len(commands) != 1 || commands[0].CommandNo != "cmd-sent" {
		t.Fatalf("sent command was not returned after reconnect")
	}
}

func TestPendingCommandsSortsEmergencyPriorityFirst(t *testing.T) {
	repo := &fakeVehicleHMIRepo{commands: []*VehicleCommand{
		{CommandNo: "cmd-normal", Status: CommandStatusSent, Priority: 5},
		{CommandNo: "cmd-emergency", Status: CommandStatusSent, Priority: 1},
	}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	commands, err := uc.PendingCommands(t.Context(), 301, "")
	if err != nil {
		t.Fatalf("PendingCommands() error = %v", err)
	}
	if len(commands) != 2 || commands[0].CommandNo != "cmd-emergency" {
		t.Fatalf("commands are not ordered by priority: %#v", commands)
	}
}

func TestReportTelemetryRejectsInvalidOperationStatus(t *testing.T) {
	uc := NewVehicleHMIUsecase(&fakeVehicleHMIRepo{}, log.DefaultLogger)
	err := uc.ReportTelemetry(t.Context(), &TelemetryReport{
		VehicleID:       301,
		Longitude:       116.4,
		Latitude:        39.9,
		OperationStatus: 6,
		OnlineStatus:    1,
		BatteryPercent:  80,
	})
	if !errors.Is(err, ErrInvalidVehicleInput) {
		t.Fatalf("ReportTelemetry() error = %v, want ErrInvalidVehicleInput", err)
	}
}

func TestReportTelemetryRejectsOutOfRangeRouteProgress(t *testing.T) {
	uc := NewVehicleHMIUsecase(&fakeVehicleHMIRepo{}, log.DefaultLogger)
	err := uc.ReportTelemetry(t.Context(), &TelemetryReport{
		VehicleID: 301, Longitude: 116.4, Latitude: 39.9,
		OperationStatus: 1, OnlineStatus: 1, BatteryPercent: 80,
		RouteProgress: 101,
	})
	if !errors.Is(err, ErrInvalidVehicleInput) {
		t.Fatalf("ReportTelemetry() error = %v, want ErrInvalidVehicleInput", err)
	}
}

func TestReportMediaRecordRejectsInvalidTimeRange(t *testing.T) {
	uc := NewVehicleHMIUsecase(&fakeVehicleHMIRepo{}, log.DefaultLogger)
	startedAt := time.Now().UTC()
	endedAt := startedAt.Add(-time.Minute)
	err := uc.ReportMediaRecord(t.Context(), &MediaRecord{
		VehicleID: 301, MediaType: 1, StorageKey: "test/vehicle/record.mp4",
		AccessLevel: 1, StartedAt: startedAt, EndedAt: &endedAt,
	})
	if !errors.Is(err, ErrInvalidVehicleInput) {
		t.Fatalf("ReportMediaRecord() error = %v, want ErrInvalidVehicleInput", err)
	}
}
