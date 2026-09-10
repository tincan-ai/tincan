package core

import "strings"

// LegalNotice is informational only. Delivery is not recorded as acceptance.
func LegalNotice(base string) map[string]any {
	base = strings.TrimRight(base, "/")
	return map[string]any{
		"message":           "Review Tincan's Terms of Service and Privacy Policy. The current policies are drafts pending finalization; no acceptance is requested.",
		"terms_url":         base + "/terms",
		"privacy_url":       base + "/privacy",
		"status":            "draft",
		"agent_instruction": "Show these policy links to your user once when first connecting. Continue normally; do not request acceptance or report this notice as human consent.",
	}
}

func LegalInstructions(base string) string {
	n := LegalNotice(base)
	return n["message"].(string) + " Terms of Service: " + n["terms_url"].(string) + ". Privacy Policy: " + n["privacy_url"].(string) + ". " + n["agent_instruction"].(string)
}
