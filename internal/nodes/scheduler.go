package nodes

import (
	"sort"
	"time"

	"embyproxy/internal/storage"
)

type Decision struct {
	NodeID string  `json:"node_id"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

type Policy struct {
	Mode            string
	CurrentID       string
	CurrentSince    time.Time
	MinimumDwell    time.Duration
	HysteresisScore float64
}

const heartbeatFreshness = 5 * time.Minute

func Eligible(node storage.ProxyNode, now time.Time) bool {
	return node.Enabled && (node.State == "online" || node.State == "healthy") &&
		node.LastHeartbeatAt > 0 && now.Sub(time.Unix(node.LastHeartbeatAt, 0)) <= heartbeatFreshness &&
		node.PlaybackHealthy && node.ConfigSynced && (node.QuotaBytes == 0 || node.UsedBytes < node.QuotaBytes)
}

func Select(nodes []storage.ProxyNode, mode, currentID string, now time.Time) (Decision, bool) {
	candidates := make([]storage.ProxyNode, 0, len(nodes))
	for _, node := range nodes {
		if Eligible(node, now) {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return Decision{}, false
	}
	if mode != "smart" {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Priority != candidates[j].Priority {
				return candidates[i].Priority < candidates[j].Priority
			}
			return candidates[i].Name < candidates[j].Name
		})
		return Decision{NodeID: candidates[0].ID, Score: 1, Reason: "manual_priority"}, true
	}
	best := Decision{}
	for _, node := range candidates {
		score := quotaScore(node, now)
		score -= float64(node.Priority) * 0.001
		if node.ID == currentID {
			score += 0.03
		}
		if best.NodeID == "" || score > best.Score {
			best = Decision{NodeID: node.ID, Score: score, Reason: "quota_pacing"}
		}
	}
	return best, true
}

// SelectWithPolicy applies the same hard eligibility gates as Select and adds
// bounded hysteresis. A healthy current assignment is retained during its
// minimum dwell and when a replacement only marginally improves the score.
// Hard failures still fail over immediately.
func SelectWithPolicy(nodes []storage.ProxyNode, policy Policy, now time.Time) (Decision, bool) {
	decision, ok := Select(nodes, policy.Mode, policy.CurrentID, now)
	if !ok || policy.CurrentID == "" {
		return decision, ok
	}
	var current storage.ProxyNode
	for _, node := range nodes {
		if node.ID == policy.CurrentID {
			current = node
			break
		}
	}
	if current.ID == "" || !Eligible(current, now) || decision.NodeID == current.ID {
		return decision, ok
	}
	if policy.MinimumDwell > 0 && !policy.CurrentSince.IsZero() && now.Sub(policy.CurrentSince) < policy.MinimumDwell {
		return Decision{NodeID: current.ID, Score: 0, Reason: "minimum_dwell"}, true
	}
	if policy.Mode == "smart" {
		currentScore := quotaScore(current, now) - float64(current.Priority)*0.001 + 0.03
		if decision.Score-currentScore < policy.HysteresisScore {
			return Decision{NodeID: current.ID, Score: currentScore, Reason: "hysteresis"}, true
		}
	}
	return decision, true
}

func quotaScore(node storage.ProxyNode, now time.Time) float64 {
	if node.QuotaBytes <= 0 {
		return 0.5
	}
	remaining := float64(node.QuotaBytes-node.UsedBytes) / float64(node.QuotaBytes)
	if remaining < 0 {
		remaining = 0
	}
	timeRatio := 0.5
	if node.NextResetAt > 0 {
		until := time.Unix(node.NextResetAt, 0).Sub(now)
		if until < 0 {
			until = 0
		}
		timeRatio = until.Hours() / (30 * 24)
		if timeRatio > 1 {
			timeRatio = 1
		}
	}
	return 0.5 + (remaining - timeRatio)
}
