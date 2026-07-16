package main

import (
	"fmt"
	"strings"
)

// This file defines lifecycle status, ad rotation mode, and the next-action hint
// surfaced to agents. The Go string types match the Google Ads REST API enum
// strings exactly.

// AdStatus is the lifecycle status of an ad, ad group, campaign, or asset group.
type AdStatus string

const (
	AdStatusEnabled AdStatus = "ENABLED"
	AdStatusPaused  AdStatus = "PAUSED"
	AdStatusRemoved AdStatus = "REMOVED"
)

// defaultAdStatus is PAUSED for safety: newly drafted entities ship paused so
// an agent can review before traffic flows. Callers opt out by passing ENABLED.
const defaultAdStatus = AdStatusPaused

// parseAdStatus validates a user-supplied status string (case-insensitive),
// returning the default (PAUSED) for an empty input.
func parseAdStatus(s string) (AdStatus, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "":
		return defaultAdStatus, nil
	case "ENABLED":
		return AdStatusEnabled, nil
	case "PAUSED":
		return AdStatusPaused, nil
	case "REMOVED":
		return AdStatusRemoved, nil
	default:
		return "", fmt.Errorf("invalid status %q: must be ENABLED, PAUSED, or REMOVED", s)
	}
}

// parseCreateStatus is parseAdStatus for create tools: an entity can never be
// created in REMOVED status (issue #14).
func parseCreateStatus(s string) (AdStatus, error) {
	status, err := parseAdStatus(s)
	if err != nil {
		return "", err
	}
	if status == AdStatusRemoved {
		return "", fmt.Errorf("cannot create an entity in REMOVED status — use ENABLED or PAUSED")
	}
	return status, nil
}

// AdRotationMode is the ad rotation mode for an ad group.
type AdRotationMode string

const (
	AdRotationOptimize      AdRotationMode = "OPTIMIZE"
	AdRotationRotateForever AdRotationMode = "ROTATE_FOREVER"
)

// parseAdRotationMode validates a user-supplied rotation mode (case-insensitive).
// There is no default: rotation mode is only written when explicitly requested.
func parseAdRotationMode(s string) (AdRotationMode, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "OPTIMIZE":
		return AdRotationOptimize, nil
	case "ROTATE_FOREVER":
		return AdRotationRotateForever, nil
	default:
		return "", fmt.Errorf("invalid ad rotation mode %q: must be OPTIMIZE or ROTATE_FOREVER", s)
	}
}

// NextActionHint points an agent at the next MCP tool to call to continue a
// workflow — e.g. enabling an entity that was created in PAUSED status.
type NextActionHint struct {
	Tool        string         `json:"tool"`
	Params      map[string]any `json:"params"`
	Description string         `json:"description"`
}

// enableAdHint tells the agent how to enable a freshly drafted (PAUSED) ad via
// the enable_entity MCP tool, using the composite "<adGroupID>~<adID>" id.
func enableAdHint(adGroupID, adID string) NextActionHint {
	composite := adGroupID + "~" + adID
	return NextActionHint{
		Tool:   "enable_entity",
		Params: map[string]any{"entity_type": "ad", "entity_id": composite},
		Description: fmt.Sprintf(
			"Ad was created in PAUSED status. Call enable_entity with entity_type='ad' and entity_id='%s' to transition it to ENABLED via MCP — no UI action required.",
			composite),
	}
}

// enableCampaignHint tells the agent how to enable a freshly drafted campaign.
func enableCampaignHint(campaignID string) NextActionHint {
	return NextActionHint{
		Tool:   "enable_entity",
		Params: map[string]any{"entity_type": "campaign", "entity_id": campaignID},
		Description: fmt.Sprintf(
			"Campaign was created in PAUSED status. Call enable_entity with entity_type='campaign' and entity_id='%s' to start serving — no UI action required.",
			campaignID),
	}
}

// enableAdGroupHint tells the agent how to enable a freshly drafted ad group.
func enableAdGroupHint(adGroupID string) NextActionHint {
	return NextActionHint{
		Tool:   "enable_entity",
		Params: map[string]any{"entity_type": "ad_group", "entity_id": adGroupID},
		Description: fmt.Sprintf(
			"Ad group was created in PAUSED status. Call enable_entity with entity_type='ad_group' and entity_id='%s' to activate it — no UI action required.",
			adGroupID),
	}
}
