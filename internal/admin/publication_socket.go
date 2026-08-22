package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"embyproxy/internal/publicationprotocol"
)

type SocketPublicationSyncer struct {
	socketPath string
	timeout    time.Duration
}

func NewSocketPublicationSyncer(socketPath string, timeout time.Duration) (*SocketPublicationSyncer, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" || !strings.HasPrefix(socketPath, "/run/") || strings.ContainsAny(socketPath, "\x00\r\n") {
		return nil, errors.New("invalid publication agent socket")
	}
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 45 * time.Second
	}
	return &SocketPublicationSyncer{socketPath: socketPath, timeout: timeout}, nil
}

func (s *SocketPublicationSyncer) Publish(ctx context.Context, plan PublicationPlan) (PublicationSyncResult, error) {
	return s.call(ctx, publicationprotocol.ActionPublish, plan)
}

func (s *SocketPublicationSyncer) Unpublish(ctx context.Context, plan PublicationPlan) (PublicationSyncResult, error) {
	return s.call(ctx, publicationprotocol.ActionUnpublish, plan)
}

func (s *SocketPublicationSyncer) Readiness(ctx context.Context, plan PublicationPlan) (PublicationSyncResult, error) {
	return s.call(ctx, publicationprotocol.ActionCheck, plan)
}

func (s *SocketPublicationSyncer) PlaybackCanary(ctx context.Context, plan PublicationPlan, input PlaybackCanaryInput) (PlaybackCanaryResult, error) {
	timeout := s.timeout
	if timeout < 90*time.Second {
		timeout = 90 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(callCtx, "unix", s.socketPath)
	if err != nil {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "edge_adapter_unreachable"}, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	request := publicationprotocol.Request{
		Version: publicationprotocol.Version, Action: publicationprotocol.ActionPlaybackCanary,
		OperationID: plan.OperationID, NodeName: plan.NodeName, RouteSlug: plan.RouteSlug,
		PlaybackCanary: &publicationprotocol.PlaybackCanaryRequest{ItemID: input.ItemID, ItemIDs: input.ItemIDs, AccessToken: input.AccessToken},
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "edge_adapter_request_failed"}, err
	}
	request.PlaybackCanary.AccessToken = ""
	var response publicationprotocol.Response
	decoder := json.NewDecoder(bufio.NewReaderSize(io.LimitReader(connection, 64<<10), 16<<10))
	if err := decoder.Decode(&response); err != nil || response.Playback == nil {
		return PlaybackCanaryResult{Status: "failed", FailureClass: "edge_adapter_response_invalid"}, errors.New("edge_adapter_response_invalid")
	}
	p := response.Playback
	result := PlaybackCanaryResult{Status: p.Status, FailureClass: p.FailureClass, ConnectivityStatus: p.ConnectivityStatus,
		PlaybackInfoStatus: p.PlaybackInfoStatus, VideoStreamStatus: p.VideoStreamStatus, MediaStatus: p.MediaStatus,
		RedirectsFollowed: p.RedirectsFollowed, EndpointsDiscovered: p.EndpointsDiscovered, BytesRead: p.BytesRead,
		ByteGrowth: p.ByteGrowth, ContentRange: p.ContentRange, AcceptRanges: p.AcceptRanges,
		Samples: p.Samples, SamplesPassed: p.SamplesPassed}
	if !response.OK {
		if result.FailureClass == "" {
			result.FailureClass = response.FailedStep
		}
		return result, errors.New("playback_canary_failed")
	}
	return result, nil
}

func (s *SocketPublicationSyncer) call(ctx context.Context, action string, plan PublicationPlan) (PublicationSyncResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(callCtx, "unix", s.socketPath)
	if err != nil {
		return unavailableSyncResult("edge_adapter_unreachable", "edge_adapter_connect"), err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(s.timeout))
	request := publicationprotocol.Request{
		Version: publicationprotocol.Version, Action: action,
		OperationID: plan.OperationID, NodeName: plan.NodeName, RouteSlug: plan.RouteSlug,
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return unavailableSyncResult("edge_adapter_request_failed", "edge_adapter_request"), err
	}
	var response publicationprotocol.Response
	decoder := json.NewDecoder(bufio.NewReaderSize(io.LimitReader(connection, 64<<10), 16<<10))
	if err := decoder.Decode(&response); err != nil {
		return unavailableSyncResult("edge_adapter_response_invalid", "edge_adapter_response"), err
	}
	result := PublicationSyncResult{
		NOSLA: PublicationEdgeResult{Status: response.NOSLA.Status, Reason: response.NOSLA.ErrorCode,
			FailedStep: response.NOSLA.FailedStep, BackupPath: response.NOSLA.BackupPath},
		BWG: PublicationEdgeResult{Status: response.BWG.Status, Reason: response.BWG.ErrorCode,
			FailedStep: response.BWG.FailedStep, BackupPath: response.BWG.BackupPath},
		FailedStep: response.FailedStep,
		Reason:     response.ErrorCode,
	}
	if response.OK && validPublicationEdgeStatus(action, result.NOSLA.Status) && validPublicationEdgeStatus(action, result.BWG.Status) {
		return result, nil
	}
	if response.OK {
		result.Reason = "edge_adapter_response_invalid"
		result.FailedStep = "edge_adapter_response"
	}
	if result.Reason == "" {
		result.Reason = "edge_sync_failed"
	}
	if result.FailedStep == "" {
		result.FailedStep = "edge_sync"
	}
	return result, errors.New(result.Reason)
}

func validPublicationEdgeStatus(action, status string) bool {
	switch action {
	case publicationprotocol.ActionCheck:
		return status == "ready"
	case publicationprotocol.ActionPublish:
		return status == "synced"
	case publicationprotocol.ActionUnpublish:
		return status == "removed" || status == "not_configured"
	default:
		return false
	}
}

func unavailableSyncResult(reason, step string) PublicationSyncResult {
	return PublicationSyncResult{
		NOSLA:  PublicationEdgeResult{Status: "unavailable", Reason: reason},
		BWG:    PublicationEdgeResult{Status: "unavailable", Reason: reason},
		Reason: reason, FailedStep: step,
	}
}
